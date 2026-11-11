package huddle

// Fake is a stand-in Slack + Amazon Chime for tests and the UI demo
// (cmd/huddle-demo): rooms.join answers with a meeting whose signaling URL
// is a local websocket server speaking Chime's protocol, and the "SFU"
// behind it is a pion peer connection on loopback. Nothing here talks to
// Slack or AWS, and the app never references it (the linker drops it).

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/pion/rtcp"
	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
	"github.com/pion/webrtc/v4/pkg/media/samplebuilder"
	"github.com/thesyncim/goh264"
)

const (
	fakeSelfAtt   = "att-self"
	fakeRemoteAtt = "att-remote"
	fakeRemote    = "U0REMOTE01"
	fakeSelf      = "U0SELFUSER"
	fakeToken     = "join-token-123"
	shareStream   = 7
)

type fakeCaller struct {
	signal string
	log    *callLog
}

// FakeCall is one Slack Web API call the fake answered (or refused).
type FakeCall struct {
	Method string
	Params url.Values
}

type callLog struct {
	mu    sync.Mutex
	calls []FakeCall
}

func (l *callLog) add(method string, p url.Values) {
	if l == nil {
		return
	}
	cp := url.Values{}
	for k, v := range p {
		cp[k] = append([]string(nil), v...)
	}
	l.mu.Lock()
	l.calls = append(l.calls, FakeCall{Method: method, Params: cp})
	l.mu.Unlock()
}

// FakeLinkBase is where the fake's rooms.getLink links point:
// FakeLinkBase + the channel id.
const FakeLinkBase = "https://app.slack.com/huddle/T0FAKETEAM/"

func (f fakeCaller) Call(ctx context.Context, method string, p url.Values, out any) error {
	f.log.add(method, p)
	if method == "rooms.getLink" { // the copy huddle link answer
		ch := p.Get("channel_id")
		if ch == "" {
			return errors.New("fake slack: rooms.getLink without channel_id")
		}
		b, _ := json.Marshal(map[string]any{"ok": true, "link": FakeLinkBase + ch})
		return json.Unmarshal(b, out)
	}
	if method == "rooms.startTranscription" || method == "rooms.stopTranscription" { // captions on / off
		if p.Get("call_id") == "" {
			return errors.New("fake slack: " + method + " without call_id")
		}
		return json.Unmarshal([]byte(`{"ok":true}`), out)
	}
	if method == "screenhero.rooms.rename" { // a huddle topic
		if p.Get("room") == "" {
			return errors.New("fake slack: screenhero.rooms.rename without room")
		}
		return json.Unmarshal([]byte(`{"ok":true}`), out)
	}
	if method == "rooms.notifyMember" { // Invite people
		if p.Get("channel_id") == "" || p.Get("user_id") == "" {
			return errors.New("fake slack: rooms.notifyMember without channel_id or user_id")
		}
		return json.Unmarshal([]byte(`{"ok":true}`), out)
	}
	if method == "rooms.inviteResponse" { // Decline / Be there soon on an incoming invite
		if p.Get("channel_id") == "" || p.Get("room_id") == "" || p.Get("response") == "" {
			return errors.New("fake slack: rooms.inviteResponse without channel_id, room_id or response")
		}
		return json.Unmarshal([]byte(`{"ok":true}`), out)
	}
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
	if strings.HasPrefix(p.Get("channel_id"), "D") {
		// a new DM huddle: only you so far (the server rings the other
		// member; FakeRingUpdate is the sh_room_update that says so)
		res["huddle"] = map[string]any{"id": "R0FAKEROOM", "participants": []string{fakeSelf}, "channels": []string{p.Get("channel_id")}, "date_start": time.Now().Unix()}
	}
	b, _ := json.Marshal(res)
	return json.Unmarshal(b, out)
}

// fakeChime is the signaling server + SFU.
type fakeChime struct {
	errf     func(string, ...any)
	clip     [][]byte // H.264 access units
	mu       sync.Mutex
	pc       *webrtc.PeerConnection
	api      *webrtc.API
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
	cam      fakeCamIn
	aware    fakeAware
	solo     atomic.Bool // SoloRoom: nobody else in the meeting
}

// fakeAware is the awareness traffic: what you sent, and how the fake's
// other person talks on the topic.
type fakeAware struct {
	mu      sync.Mutex
	send    func(int, func(*pbw)) // the live connection's (nil before JOIN)
	seen    FakeAwareness
	sticker string // the other person's
}

// FakeAwareness is what the fake saw of your awareness messages.
type FakeAwareness struct {
	Queries, Broadcasts, Closes int
	Last                        *Awareness // your last broadcast
}

// fakeCamIn is what the fake saw of your camera: the SUBSCRIBE send side
// and the H.264 RTP on the camera m-section, decoded.
type fakeCamIn struct {
	mu      sync.Mutex
	duplex  uint32
	desc    []streamDesc // video send streams of the last SUBSCRIBE
	subs    []subscribeSeen
	pkts    atomic.Int64
	frames  atomic.Int64
	keys    atomic.Int64
	keysPLI atomic.Int64 // keyframes after the fake's PLI
	pliSent atomic.Bool
	w, h    atomic.Int32
	pt      atomic.Int32
	ssrc    atomic.Uint32
	mime    atomic.Value // string
}

type subscribeSeen struct {
	Duplex uint32
	Video  []streamDesc
	SDP    string
}

func (f *fakeChime) send(ctx context.Context, c *websocket.Conn, typ int, body func(w *pbw)) {
	_ = c.Write(ctx, websocket.MessageBinary, buildFrame(typ, body))
}

func (f *fakeChime) serve(w http.ResponseWriter, r *http.Request) {
	if strings.Contains(r.Header.Get("Sec-WebSocket-Protocol"), fakeToken) {
		f.gotToken.Store(true)
	}
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{"_aws_wt_session"}, OriginPatterns: []string{"app.slack.com"}})
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
			f.errf("fake chime: bad frame: %v", err)
			return
		}
		switch fr.Type {
		case ftJoin:
			f.joined.Store(true)
			f.aware.mu.Lock()
			f.aware.send = send
			f.aware.mu.Unlock()
			send(ftJoinAck, func(w *pbw) { w.msg(5, func(w *pbw) {}) }) // no TURN: direct on loopback
			send(ftIndex, func(w *pbw) { f.index(w, nil) })
		case ftSubscribe:
			sub := parseSubscribe(b)
			sdp, recv := sub.SDP, sub.Recv
			f.cam.mu.Lock()
			f.cam.duplex, f.cam.desc = sub.Duplex, sub.Video
			f.cam.subs = append(f.cam.subs, subscribeSeen{Duplex: sub.Duplex, Video: sub.Video, SDP: sub.SDP})
			f.cam.mu.Unlock()
			answer, ssrc, err := f.negotiate(sdp, recv)
			if err != nil {
				f.errf("fake chime: negotiate: %v", err)
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
					for _, v := range sub.Video { // SdkStreamAllocation for what you send
						w.msg(2, func(w *pbw) {
							w.str(1, v.TrackLabel)
							w.uint(2, uint64(v.StreamID))
							w.uint(3, uint64(v.GroupID))
						})
					}
				})
			})
			if f.subs.Load() == 1 {
				go f.chatter(ctx, send)
			}
			// like Chime: a published camera shows in everyone's INDEX
			send(ftIndex, func(w *pbw) { f.index(w, sub.Video) })
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
				} else if err == nil && m.Topic == TopicAwareness {
					f.onAwareness(pm)
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
	solo := f.solo.Load()
	send(ftAudioStreamIDInfo, func(w *pbw) {
		w.msg(19, func(w *pbw) {
			w.msg(1, func(w *pbw) {
				w.uint(1, 1)
				w.str(2, fakeSelfAtt)
				w.str(4, "E1-R0FAKEROOM-"+fakeSelf)
			})
			if solo {
				return
			}
			w.msg(1, func(w *pbw) {
				w.uint(1, 2)
				w.str(2, fakeRemoteAtt)
				w.str(4, "E1-R0FAKEROOM-"+fakeRemote)
				w.boolean(3, false)
			})
		})
	})
	if solo { // nobody to hear, react or caption
		<-ctx.Done()
		return
	}
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
		if i == 5 || i%10 == 0 { // live captions (Chime transcription)
			var tw pbw
			tw.msg(1, func(w *pbw) {
				w.msg(2, func(w *pbw) {
					w.msg(1, func(w *pbw) {
						w.msg(1, func(w *pbw) {
							w.msg(1, func(w *pbw) { w.str(3, fakeRemoteAtt) })
							w.str(2, "Hello from the fake huddle")
						})
						w.str(5, "res-1")
					})
				})
			})
			send(ftDataMessage, func(w *pbw) {
				w.msg(23, func(w *pbw) {
					w.msg(1, func(w *pbw) {
						w.str(1, TopicTranscript)
						w.bytes(2, tw.b)
						w.str(4, "aws:chime")
					})
				})
			})
		}
		if i == 3 || i%20 == 0 {
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

// index is the fake's INDEX: the remote screen share, plus the video
// streams you publish (under your attendee id, as Chime lists them).
func (f *fakeChime) index(w *pbw, mine []streamDesc) {
	w.msg(8, func(w *pbw) {
		if len(f.clip) > 0 { // NewFake(nil): nobody shares
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
		}
		for _, v := range mine {
			w.msg(2, func(w *pbw) {
				w.uint(1, uint64(v.StreamID))
				w.uint(2, uint64(v.Framerate))
				w.uint(3, uint64(v.MaxBitrateKbps))
				w.uint(6, uint64(v.GroupID))
				w.str(8, v.AttendeeID)
				w.uint(9, mediaVideo)
				w.str(10, "E1-R0FAKEROOM-"+fakeSelf)
			})
		}
		w.uint(4, 2)
	})
}

type parsedSubscribe struct {
	Duplex uint32
	Audio  []streamDesc
	Video  []streamDesc
	Recv   []uint32
	SDP    string
}

func parseSubscribe(b []byte) (p parsedSubscribe) {
	p.Duplex = streamRX
	_ = pbEach(b[1:], func(f pbField) error {
		if f.num == 6 {
			return pbEach(f.b, func(g pbField) error {
				switch g.num {
				case 1:
					p.Duplex = g.u32()
				case 2:
					d := decodeStream(g.b)
					if d.MediaType == mediaVideo {
						p.Video = append(p.Video, d)
					} else {
						p.Audio = append(p.Audio, d)
					}
				case 3:
					p.Recv = pbUints(p.Recv, g)
				case 4:
					p.SDP = g.str()
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
		f.errf("offer origin not rewritten")
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
		f.api = webrtc.NewAPI(webrtc.WithMediaEngine(m), webrtc.WithSettingEngine(se))
		pc, err := f.api.NewPeerConnection(webrtc.Configuration{})
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
		// Chime sends a stream on the video m-section whose receive slot
		// asked for it (never on your camera's, slot 0)
		var tr *webrtc.RTPTransceiver
		i := 0
		for _, t := range pc.GetTransceivers() {
			if t.Kind() != webrtc.RTPCodecTypeVideo {
				continue
			}
			if i < len(recv) && recv[i] == shareStream {
				tr = t
			}
			i++
		}
		if tr == nil {
			return "", 0, errors.New("no m-section for the share")
		}
		sender, err := f.api.NewRTPSender(vt, pc.SCTP().Transport())
		if err != nil {
			return "", 0, err
		}
		if err := tr.SetSender(sender, vt); err != nil {
			return "", 0, err
		}
		go func() { // drain RTCP (pion's interceptors want it read)
			for {
				if _, _, err := sender.ReadRTCP(); err != nil {
					return
				}
			}
		}()
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
		f.errf("%v", err)
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
	if len(f.clip) == 0 {
		return
	}
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
	if track.Kind() == webrtc.RTPCodecTypeVideo {
		f.recvCamera(track)
		return
	}
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

// recvCamera decodes your camera like a Chime receiver would, and after a
// few pictures asks for a keyframe (PLI) to check you answer it.
func (f *fakeChime) recvCamera(track *webrtc.TrackRemote) {
	c := &f.cam
	c.pt.Store(int32(track.PayloadType()))
	c.ssrc.Store(uint32(track.SSRC()))
	c.mime.Store(track.Codec().MimeType)
	sb := samplebuilder.New(512, &codecs.H264Packet{}, track.Codec().ClockRate, samplebuilder.WithMaxTimeDelay(time.Second))
	dec := goh264.NewDecoder()
	for {
		pkt, _, err := track.ReadRTP()
		if err != nil {
			return
		}
		c.pkts.Add(1)
		sb.Push(pkt)
		for s := sb.Pop(); s != nil; s = sb.Pop() {
			key := hasIDR(s.Data)
			frames, err := dec.DecodeFrames(s.Data)
			if err != nil {
				continue
			}
			for _, fr := range frames {
				c.frames.Add(1)
				c.w.Store(int32(fr.Width))
				c.h.Store(int32(fr.Height))
			}
			if key {
				c.keys.Add(1)
				if c.pliSent.Load() {
					c.keysPLI.Add(1)
				}
			}
			if c.frames.Load() >= 5 && c.pliSent.CompareAndSwap(false, true) {
				f.mu.Lock()
				pc := f.pc
				f.mu.Unlock()
				if pc != nil {
					_ = pc.WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{MediaSSRC: uint32(track.SSRC())}})
				}
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

// buildFrame encodes a server frame the way Chime would.
func buildFrame(typ int, body func(w *pbw)) []byte {
	var w pbw
	frameHeader(&w, typ)
	body(&w)
	return wrapRTC(w.b)
}

// Fake is a running fake Slack + Chime meeting with one other person in
// it (FakeRemoteUser): they talk (a 660 Hz tone), react with :tada:, and
// share their screen (clip, Annex B H.264; nil = no share).
type Fake struct {
	f   *fakeChime
	srv *httptest.Server
	log callLog
}

// FakeRoomID is the fake huddle's room (call) id.
const FakeRoomID = "R0FAKEROOM"

// Calls is every Slack Web API call the fake got, in order.
func (x *Fake) Calls() []FakeCall {
	x.log.mu.Lock()
	defer x.log.mu.Unlock()
	return append([]FakeCall(nil), x.log.calls...)
}

// FakeRingUpdate is the sh_room_update Slack sends while it rings user
// for the fake room (status "pending", "joining_soon", "declined"…).
func FakeRingUpdate(channel, user, status string) json.RawMessage {
	b, _ := json.Marshal(map[string]any{"type": "sh_room_update", "huddle": map[string]any{"channel_id": channel},
		"room": map[string]any{"id": FakeRoomID, "channels": []string{channel},
			"last_invite_status_by_user": map[string]string{user: status}}})
	return b
}

// FakeRemoteUser is the other person in a Fake huddle; FakeSelfUser is you.
const (
	FakeRemoteUser = fakeRemote
	FakeSelfUser   = fakeSelf
)

// NewFake starts the fake (loopback only). errf gets protocol errors.
func NewFake(clip []byte, errf func(string, ...any)) *Fake {
	f := &fakeChime{errf: errf, clip: splitAU(clip)}
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	// ICE on loopback for the client side too
	testSettings = func(se *webrtc.SettingEngine) {
		se.SetIncludeLoopbackCandidate(true)
		se.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4})
	}
	return &Fake{f: f, srv: srv}
}

// SoloRoom makes the meeting yours alone from the next join on: the
// other person isn't in Chime's audio (the alone timeout's tests).
func (x *Fake) SoloRoom() { x.f.solo.Store(true) }

// Caller answers rooms.join with the fake meeting.
func (x *Fake) Caller() Caller {
	return fakeCaller{signal: "ws" + strings.TrimPrefix(x.srv.URL, "http") + "/control/m-1", log: &x.log}
}

// Close stops the fake.
func (x *Fake) Close() {
	x.f.left.Store(true)
	x.srv.Close()
	x.f.mu.Lock()
	if x.f.pc != nil {
		_ = x.f.pc.Close()
	}
	x.f.mu.Unlock()
	testSettings = nil
}

func (f *fakeChime) onAwareness(pm *PeerMessage) {
	f.aware.mu.Lock()
	answer := false
	switch {
	case pm.AwarenessQuery != nil:
		f.aware.seen.Queries++
		answer = true // everyone answers a query with a broadcast
	case pm.AwarenessBroadcast != nil:
		f.aware.seen.Broadcasts++
		a := *pm.AwarenessBroadcast
		a.State = append([]AwarenessPayload(nil), a.State...)
		f.aware.seen.Last = &a
	case pm.AwarenessClose != nil:
		f.aware.seen.Closes++
	}
	f.aware.mu.Unlock()
	if answer {
		f.remoteAwareness(&PeerMessage{AwarenessBroadcast: f.remoteState()})
	}
}

func (f *fakeChime) remoteState() *Awareness {
	f.aware.mu.Lock()
	defer f.aware.mu.Unlock()
	a := &Awareness{Channel: FakeRoomID, User: fakeRemote}
	if f.aware.sticker != "" {
		a.State = []AwarenessPayload{{FieldName: AwarenessSticky, FieldValue: f.aware.sticker}}
	}
	return a
}

// remoteAwareness sends m on the awareness topic from the other person.
func (f *fakeChime) remoteAwareness(m *PeerMessage) error {
	f.aware.mu.Lock()
	send := f.aware.send
	f.aware.mu.Unlock()
	if send == nil {
		return errors.New("fake chime: not joined")
	}
	b := m.Encode()
	send(ftDataMessage, func(w *pbw) {
		w.msg(23, func(w *pbw) {
			w.msg(1, func(w *pbw) {
				w.str(1, TopicAwareness)
				w.bytes(2, b)
				w.str(4, fakeRemoteAtt)
			})
		})
	})
	return nil
}

// Awareness is what the fake saw of your awareness messages so far.
func (x *Fake) Awareness() FakeAwareness {
	x.f.aware.mu.Lock()
	defer x.f.aware.mu.Unlock()
	return x.f.aware.seen
}

// RemoteSticker has the other person put emoji on their tile ("" takes
// it off): their awareness broadcast, as Slack's client sends it.
func (x *Fake) RemoteSticker(emoji string) error {
	x.f.aware.mu.Lock()
	x.f.aware.sticker = emoji
	x.f.aware.mu.Unlock()
	return x.f.remoteAwareness(&PeerMessage{AwarenessBroadcast: x.f.remoteState()})
}

// lastSub is the last SUBSCRIBE the fake got.
func (f *fakeChime) lastSub() subscribeSeen {
	f.cam.mu.Lock()
	defer f.cam.mu.Unlock()
	if len(f.cam.subs) == 0 {
		return subscribeSeen{}
	}
	return f.cam.subs[len(f.cam.subs)-1]
}
