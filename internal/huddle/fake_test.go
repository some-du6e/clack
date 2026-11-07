package huddle

// A fake Slack + Amazon Chime: rooms.join answers with a meeting whose
// signaling URL is a local websocket server speaking Chime's protocol, and
// the "SFU" behind it is a pion peer connection on loopback. Nothing here
// talks to Slack or AWS.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
)

const (
	fakeSelfAtt   = "att-self"
	fakeRemoteAtt = "att-remote"
	fakeRemote    = "U0REMOTE01"
	fakeSelf      = "U0SELFUSER"
	fakeToken     = "join-token-123"
	shareStream   = 7
)

type fakeCaller struct{ signal string }

func (f fakeCaller) Call(ctx context.Context, method string, p url.Values, out any) error {
	if method != "rooms.join" {
		return errors.New("fake slack: " + method)
	}
	res := map[string]any{"ok": true,
		"call": map[string]any{"call_id": "R0FAKEROOM", "free_willy": map[string]any{
			"meeting": map[string]any{"MeetingId": "m-1", "MediaRegion": "us-east-2",
				"MediaPlacement": map[string]any{"SignalingUrl": f.signal, "AudioHostUrl": "audio.example:3478"}},
			"attendee": map[string]any{"AttendeeId": fakeSelfAtt, "ExternalUserId": "E1-R0FAKEROOM-" + fakeSelf, "JoinToken": fakeToken},
		}},
		"huddle": map[string]any{"id": "R0FAKEROOM", "participants": []string{fakeSelf, fakeRemote}, "thread_root_ts": "1700000000.000100", "date_start": time.Now().Unix()},
	}
	b, _ := json.Marshal(res)
	return json.Unmarshal(b, out)
}

// fakeChime is the signaling server + SFU.
type fakeChime struct {
	t        *testing.T
	clip     [][]byte // H.264 access units
	mu       sync.Mutex
	pc       *webrtc.PeerConnection
	video    *webrtc.TrackLocalStaticSample
	gotToken atomic.Bool
	joined   atomic.Bool
	subs     atomic.Int32
	muted    atomic.Bool
	left     atomic.Bool
	audioIn  atomic.Int64
	peak     atomic.Int64 // level * 1000
	reacji   atomic.Value // string
	sending  atomic.Bool
}

func (f *fakeChime) send(ctx context.Context, c *websocket.Conn, typ int, body func(w *pbw)) {
	_ = c.Write(ctx, websocket.MessageBinary, buildFrame(typ, body))
}

func (f *fakeChime) serve(w http.ResponseWriter, r *http.Request) {
	if strings.Contains(r.Header.Get("Sec-WebSocket-Protocol"), fakeToken) {
		f.gotToken.Store(true)
	}
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{"_aws_wt_session"}})
	if err != nil {
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	defer c.CloseNow()
	var wmu sync.Mutex
	send := func(typ int, body func(w *pbw)) {
		wmu.Lock()
		defer wmu.Unlock()
		f.send(ctx, c, typ, body)
	}
	for {
		_, b, err := c.Read(ctx)
		if err != nil {
			return
		}
		fr, err := decodeFrame(b)
		if err != nil {
			f.t.Errorf("fake chime: bad frame: %v", err)
			return
		}
		switch fr.Type {
		case ftJoin:
			f.joined.Store(true)
			send(ftJoinAck, func(w *pbw) { w.msg(5, func(w *pbw) {}) }) // no TURN: direct on loopback
			send(ftIndex, func(w *pbw) {
				w.msg(8, func(w *pbw) {
					w.msg(2, func(w *pbw) {
						w.uint(1, shareStream)
						w.uint(3, 1500)
						w.uint(6, 3)
						w.str(8, fakeRemoteAtt+"#content")
						w.uint(9, mediaVideo)
						w.str(10, "E1-R0FAKEROOM-"+fakeRemote)
						w.uint(11, 320)
						w.uint(12, 180)
					})
					w.uint(4, 2)
				})
			})
		case ftSubscribe:
			sdp, recv := parseSubscribe(b)
			answer, ssrc, err := f.negotiate(sdp, recv)
			if err != nil {
				f.t.Errorf("fake chime: negotiate: %v", err)
				return
			}
			f.subs.Add(1)
			send(ftSubscribeAck, func(w *pbw) {
				w.msg(7, func(w *pbw) {
					w.str(3, answer)
					if ssrc != 0 {
						w.msg(4, func(w *pbw) {
							w.uint(1, shareStream)
							w.uint(2, uint64(ssrc))
						})
					}
				})
			})
			if f.subs.Load() == 1 {
				go f.chatter(ctx, send)
			}
		case ftAudioControl:
			f.muted.Store(parseMute(b))
		case ftPingPong:
			if fr.PingType == pingTypePing {
				id := fr.PingID
				send(ftPingPong, func(w *pbw) {
					w.msg(20, func(w *pbw) {
						w.uint(1, pingTypePong)
						w.uint(2, uint64(id))
					})
				})
			}
		case ftDataMessage:
			for _, m := range fr.Data {
				if pm, err := DecodePeerMessage(m.Data); err == nil && pm.Reacji != nil && m.Topic == TopicData {
					f.reacji.Store(pm.Reacji.Emoji + "/" + pm.FromPeerIDString)
				}
			}
		case ftLeave:
			f.left.Store(true)
			send(ftLeaveAck, func(w *pbw) { w.msg(12, func(w *pbw) {}) })
			return
		}
	}
}

// chatter is what Chime tells a joined attendee: who's on which audio
// stream, volumes, and a reaction from the other person.
func (f *fakeChime) chatter(ctx context.Context, send func(int, func(*pbw))) {
	send(ftAudioStreamIDInfo, func(w *pbw) {
		w.msg(19, func(w *pbw) {
			w.msg(1, func(w *pbw) {
				w.uint(1, 1)
				w.str(2, fakeSelfAtt)
				w.str(4, "E1-R0FAKEROOM-"+fakeSelf)
			})
			w.msg(1, func(w *pbw) {
				w.uint(1, 2)
				w.str(2, fakeRemoteAtt)
				w.str(4, "E1-R0FAKEROOM-"+fakeRemote)
				w.boolean(3, false)
			})
		})
	})
	pm := (&PeerMessage{Reacji: &Reacji{Emoji: "tada", Type: ReacjiStandard}, FromPeerIDString: fakeRemoteAtt}).Encode()
	t := time.NewTicker(150 * time.Millisecond)
	defer t.Stop()
	for i := 0; ; i++ {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		send(ftAudioMetadata, func(w *pbw) {
			w.msg(18, func(w *pbw) {
				w.msg(1, func(w *pbw) {
					w.uint(1, 2)
					w.uint(2, 10) // -10 dBFS: talking
				})
			})
		})
		if i == 3 {
			send(ftDataMessage, func(w *pbw) {
				w.msg(23, func(w *pbw) {
					w.msg(1, func(w *pbw) {
						w.str(1, TopicData)
						w.bytes(2, pm)
						w.str(4, fakeRemoteAtt)
					})
				})
			})
		}
	}
}

func parseSubscribe(b []byte) (sdp string, recv []uint32) {
	_ = pbEach(b[1:], func(f pbField) error {
		if f.num == 6 {
			return pbEach(f.b, func(g pbField) error {
				switch g.num {
				case 3:
					recv = pbUints(recv, g)
				case 4:
					sdp = g.str()
				}
				return nil
			})
		}
		return nil
	})
	return
}

func parseMute(b []byte) (muted bool) {
	_ = pbEach(b[1:], func(f pbField) error {
		if f.num == 17 {
			return pbEach(f.b, func(g pbField) error {
				if g.num == 1 {
					muted = g.boolean()
				}
				return nil
			})
		}
		return nil
	})
	return
}

func (f *fakeChime) negotiate(offer string, recv []uint32) (string, uint32, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !strings.Contains(offer, "o=mozilla-chrome") {
		f.t.Errorf("offer origin not rewritten")
	}
	first := f.pc == nil
	if first {
		m := &webrtc.MediaEngine{}
		if err := m.RegisterDefaultCodecs(); err != nil {
			return "", 0, err
		}
		se := webrtc.SettingEngine{}
		se.SetIncludeLoopbackCandidate(true)
		se.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4})
		pc, err := webrtc.NewAPI(webrtc.WithMediaEngine(m), webrtc.WithSettingEngine(se)).NewPeerConnection(webrtc.Configuration{})
		if err != nil {
			return "", 0, err
		}
		pc.OnTrack(f.onTrack)
		f.pc = pc
	}
	pc := f.pc
	if err := pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: offer}); err != nil {
		return "", 0, err
	}
	if first { // the huddle's mixed audio: a 660 Hz tone
		at, err := webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2}, "mix", "chime")
		if err != nil {
			return "", 0, err
		}
		if _, err := pc.AddTrack(at); err != nil {
			return "", 0, err
		}
		go f.sendTone(at)
	}
	var ssrc uint32
	wantShare := false
	for _, id := range recv {
		wantShare = wantShare || id == shareStream
	}
	if wantShare && f.video == nil {
		vt, err := webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264, ClockRate: 90000,
			SDPFmtpLine: "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42e01f"}, "share", "chime")
		if err != nil {
			return "", 0, err
		}
		if _, err := pc.AddTrack(vt); err != nil {
			return "", 0, err
		}
		f.video = vt
	}
	ans, err := pc.CreateAnswer(nil)
	if err != nil {
		return "", 0, err
	}
	g := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(ans); err != nil {
		return "", 0, err
	}
	<-g
	if f.video != nil {
		for _, s := range pc.GetSenders() {
			if s.Track() == f.video {
				if ps := s.GetParameters(); len(ps.Encodings) > 0 {
					ssrc = uint32(ps.Encodings[0].SSRC)
				}
			}
		}
		if f.sending.CompareAndSwap(false, true) {
			go f.sendClip(f.video)
		}
	}
	return pc.LocalDescription().SDP, ssrc, nil
}

func (f *fakeChime) sendTone(t *webrtc.TrackLocalStaticSample) {
	enc, err := newOpusEnc()
	if err != nil {
		f.t.Error(err)
		return
	}
	tone := NewToneDevice(660).(*toneDevice)
	frames := make(chan []int16, 4)
	_ = tone.Start(func(p []int16) {
		select {
		case frames <- append([]int16(nil), p...):
		default:
		}
	}, nil)
	defer tone.Close()
	buf := make([]byte, 0, 1500)
	for p := range frames {
		pkt, err := enc.encode(p, buf)
		if err != nil {
			continue
		}
		if t.WriteSample(media.Sample{Data: pkt, Duration: 20 * time.Millisecond}) != nil {
			return
		}
		if f.left.Load() {
			return
		}
	}
}

func (f *fakeChime) sendClip(t *webrtc.TrackLocalStaticSample) {
	tick := time.NewTicker(time.Second / 15)
	defer tick.Stop()
	for i := 0; !f.left.Load(); i++ {
		<-tick.C
		if err := t.WriteSample(media.Sample{Data: f.clip[i%len(f.clip)], Duration: time.Second / 15}); err != nil {
			return
		}
	}
}

func (f *fakeChime) onTrack(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
	if track.Kind() != webrtc.RTPCodecTypeAudio {
		return
	}
	dec, err := newOpusDec()
	if err != nil {
		return
	}
	for {
		pkt, _, err := track.ReadRTP()
		if err != nil {
			return
		}
		f.audioIn.Add(1)
		if pcm, err := dec.decode(pkt.Payload); err == nil {
			if lv := int64(level(pcm) * 1000); lv > f.peak.Load() {
				f.peak.Store(lv)
			}
		}
	}
}

func (s *Session) audioInCount() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stats.audioIn
}

// splitAU cuts an Annex B stream into access units (one slice per frame).
func splitAU(b []byte) [][]byte {
	var aus [][]byte
	var cur []byte
	sc := []byte{0, 0, 1}
	for len(b) > 0 {
		i := bytes.Index(b, sc)
		if i < 0 {
			break
		}
		start := i
		if start > 0 && b[start-1] == 0 {
			start--
		}
		b = b[i+3:]
		j := bytes.Index(b, sc)
		end := len(b)
		if j >= 0 {
			end = j
			if end > 0 && b[end-1] == 0 {
				end--
			}
		}
		nal := b[:end]
		cur = append(cur, 0, 0, 0, 1)
		cur = append(cur, nal...)
		if t := nal[0] & 0x1f; t == 1 || t == 5 {
			aus = append(aus, cur)
			cur = nil
		}
		b = b[end:]
	}
	return aus
}

func TestFakeChime(t *testing.T) {
	clip, err := os.ReadFile("testdata/testsrc-320x180.h264")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeChime{t: t, clip: splitAU(clip)}
	if len(f.clip) < 10 {
		t.Fatalf("clip: %d access units", len(f.clip))
	}
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	defer srv.Close()
	defer func() {
		f.mu.Lock()
		if f.pc != nil {
			_ = f.pc.Close()
		}
		f.mu.Unlock()
	}()
	testSettings = func(se *webrtc.SettingEngine) {
		se.SetIncludeLoopbackCandidate(true)
		se.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4})
	}
	defer func() { testSettings = nil }()

	var peers sync.Map
	ended := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, err := Join(ctx, Options{
		Client: fakeCaller{signal: "ws" + strings.TrimPrefix(srv.URL, "http") + "/control/m-1"},
		SelfID: fakeSelf, Device: NewToneDevice(440), Logf: t.Logf,
		OnPeer: func(ev PeerEvent) { peers.Store(ev.Kind, ev) },
		OnEnd:  func(err error) { ended <- err },
	}, "C0TEST")
	if err != nil {
		t.Fatal(err)
	}
	if st := s.State(); st.RoomID != "R0FAKEROOM" || st.ThreadTS != "1700000000.000100" || len(st.Participants) != 2 {
		t.Fatalf("after join: %+v", st)
	}

	// wait for everything the fake sends to arrive
	deadline := time.Now().Add(25 * time.Second)
	var st State
	var frame VideoFrame
	ok := func() bool {
		st = s.State()
		if st.Phase != Connected || len(st.Videos) == 0 {
			return false
		}
		frame = s.Frame(st.Videos[0].Key)
		var remote *Participant
		for i := range st.Participants {
			if st.Participants[i].UserID == fakeRemote {
				remote = &st.Participants[i]
			}
		}
		_, gotPeer := peers.Load("reacji")
		return remote != nil && remote.InMedia && remote.Speaking(time.Now()) && remote.Reaction == "tada" &&
			gotPeer && frame.Img != nil && frame.Seq >= 3 && f.peak.Load() > 50 && s.audioInCount() > 20
	}
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out: phase %s, videos %+v, frame seq %d, peak sent %d, packets in %d, stats %s, state %+v",
				st.Phase, st.Videos, frame.Seq, f.peak.Load(), f.audioIn.Load(), s.Stats(), st.Participants)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !f.gotToken.Load() {
		t.Error("the join token wasn't offered as a websocket subprotocol")
	}
	if v := st.Videos[0]; !v.Content || v.UserID != fakeRemote || v.W != 320 || v.H != 180 {
		t.Errorf("video: %+v", v)
	}
	if b := frame.Img.Bounds(); b.Dx() != 320 || b.Dy() != 180 {
		t.Errorf("frame %v", b)
	}
	if s.UserOf(fakeRemoteAtt) != fakeRemote {
		t.Errorf("UserOf: %q", s.UserOf(fakeRemoteAtt))
	}
	s.mu.Lock()
	inLevel := s.stats.inLevel
	s.mu.Unlock()
	t.Logf("received mixed audio level %.3f; the fake heard us at %.3f; %s", inLevel, float64(f.peak.Load())/1000, s.Stats())

	// controls
	s.SetMuted(true)
	if err := s.React("wave", ""); err != nil {
		t.Fatal(err)
	}
	for time.Now().Before(deadline) && (!f.muted.Load() || f.reacji.Load() == nil) {
		time.Sleep(50 * time.Millisecond)
	}
	if !f.muted.Load() {
		t.Error("AUDIO_CONTROL muted never arrived")
	}
	if r, _ := f.reacji.Load().(string); r != "wave/"+fakeSelfAtt {
		t.Errorf("reaction at the fake: %q", r)
	}
	s.SetDeafened(true)
	if st := s.State(); !st.Deafened || !st.Muted {
		t.Errorf("deafen: %+v", st)
	}

	// a room update for another room changes nothing; ours ending ends us
	s.HandleRoomEvent("sh_room_update", json.RawMessage(`{"room":{"id":"R0OTHER","has_ended":true}}`))
	if s.State().Phase == Ended {
		t.Fatal("another room's end ended us")
	}
	s.Leave()
	if !f.left.Load() {
		t.Error("LEAVE never reached the fake")
	}
	select {
	case err := <-ended:
		if err != nil {
			t.Errorf("end reason: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Error("OnEnd not called")
	}
	if s.State().Phase != Ended {
		t.Error("not ended")
	}
}
