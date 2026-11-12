package huddle

// The fake Chime's side of a screen share: a second websocket that joins
// with the "#content" join token, its own loopback peer connection that
// decodes the screen like a Chime receiver, and what the main connection
// is told (the content attendee's audio stream and the shared video in
// INDEX), as Chime tells every attendee.

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v4"
)

// The content attendee's streams as the fake's INDEX and audio info list
// them (Chime's ids, not the SUBSCRIBE's local ones).
const (
	fakeContentAudioStream = 3
	fakeContentVideoStream = 21
)

// fakeContentIn is what the fake saw of your content attendee.
type fakeContentIn struct {
	mu      sync.Mutex
	pc      *webrtc.PeerConnection
	subs    []parsedSubscribe
	present bool
	joins   int
	leaves  int
	token   string

	audioPkts atomic.Int64
	fakeVideoIn
	mime atomic.Value // string
}

// FakeContentShare is what the fake saw of your screen share's content
// attendee.
type FakeContentShare struct {
	Joins, Leaves int    // JOIN and LEAVE frames on "#content" sockets
	Token         string // the join token it connected with
	Present       bool   // listed in the main connection's INDEX and audio info
	Subscribes    int
	Duplex        uint32
	Video, Audio  []streamDesc // the last SUBSCRIBE's send streams
	Receive       []uint32
	AudioPackets  int64
	Frames, Keys  int64
	KeysAfterPLI  int64
	Packets       int64 // video RTP packets
	Undecodable   int64 // access units that didn't decode
	KeyRequests   int64 // the PLIs those asked for
	W, H          int
	Mime          string
}

// ContentShare reports what the fake saw of your content attendee.
func (x *Fake) ContentShare() FakeContentShare {
	c := &x.f.content
	c.mu.Lock()
	out := FakeContentShare{Joins: c.joins, Leaves: c.leaves, Token: c.token, Present: c.present, Subscribes: len(c.subs)}
	if n := len(c.subs); n > 0 {
		l := c.subs[n-1]
		out.Duplex, out.Video, out.Audio, out.Receive = l.Duplex, l.Video, l.Audio, l.Recv
	}
	c.mu.Unlock()
	out.AudioPackets = c.audioPkts.Load()
	out.Frames, out.Keys, out.KeysAfterPLI = c.frames.Load(), c.keys.Load(), c.keysPLI.Load()
	out.Packets, out.Undecodable, out.KeyRequests = c.pkts.Load(), c.undecodable.Load(), c.keyReqs.Load()
	out.W, out.H = int(c.w.Load()), int(c.h.Load())
	out.Mime, _ = c.mime.Load().(string)
	return out
}

// serveContent is the content attendee's signaling socket.
func (f *fakeChime) serveContent(w http.ResponseWriter, r *http.Request, token string) {
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
	ct := &f.content
	ct.mu.Lock()
	ct.token = token
	ct.mu.Unlock()
	defer f.contentGone()
	for {
		_, b, err := c.Read(ctx)
		if err != nil {
			return
		}
		fr, err := decodeFrame(b)
		if err != nil {
			f.errf("fake chime: bad content frame: %v", err)
			return
		}
		switch fr.Type {
		case ftJoin:
			ct.mu.Lock()
			ct.joins++
			ct.mu.Unlock()
			send(ftJoinAck, func(w *pbw) { w.msg(5, func(w *pbw) {}) })
			send(ftIndex, func(w *pbw) { f.index(w, nil) })
		case ftSubscribe:
			sub := parseSubscribe(b)
			ct.mu.Lock()
			ct.subs = append(ct.subs, sub)
			ct.mu.Unlock()
			answer, err := f.negotiateContent(sub.SDP)
			if err != nil {
				f.errf("fake chime: content negotiate: %v", err)
				return
			}
			send(ftSubscribeAck, func(w *pbw) {
				w.msg(7, func(w *pbw) {
					w.str(3, answer)
					for _, v := range sub.Video {
						w.msg(2, func(w *pbw) {
							w.str(1, v.TrackLabel)
							w.uint(2, uint64(v.StreamID))
							w.uint(3, uint64(v.GroupID))
						})
					}
				})
			})
			f.contentPresent(len(sub.Video) > 0)
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
		case ftLeave:
			ct.mu.Lock()
			ct.leaves++
			ct.mu.Unlock()
			send(ftLeaveAck, func(w *pbw) { w.msg(12, func(w *pbw) {}) })
			return
		}
	}
}

// contentGone is the content socket closing: Chime drops the attendee.
func (f *fakeChime) contentGone() {
	ct := &f.content
	ct.mu.Lock()
	pc := ct.pc
	ct.pc = nil
	ct.mu.Unlock()
	if pc != nil {
		_ = pc.Close()
	}
	f.contentPresent(false)
}

// contentPresent tells the main connection the content attendee came or
// went: its audio stream in AUDIO_STREAM_ID_INFO (present, or dropped)
// and its video in INDEX.
func (f *fakeChime) contentPresent(on bool) {
	ct := &f.content
	ct.mu.Lock()
	changed := ct.present != on
	ct.present = on
	ct.mu.Unlock()
	if !changed {
		return
	}
	f.aware.mu.Lock()
	send := f.aware.send
	f.aware.mu.Unlock()
	if send == nil {
		return
	}
	send(ftAudioStreamIDInfo, func(w *pbw) {
		w.msg(19, func(w *pbw) {
			w.msg(1, func(w *pbw) {
				w.uint(1, fakeContentAudioStream)
				if on {
					w.str(2, fakeSelfAtt+"#content")
					w.str(4, "E1-R0FAKEROOM-"+fakeSelf)
				}
			})
		})
	})
	f.cam.mu.Lock()
	mine := append([]streamDesc(nil), f.cam.desc...)
	f.cam.mu.Unlock()
	send(ftIndex, func(w *pbw) { f.index(w, mine) })
}

// contentStreams is what INDEX lists for the content attendee.
func (f *fakeChime) contentStreams() []streamDesc {
	ct := &f.content
	ct.mu.Lock()
	defer ct.mu.Unlock()
	if !ct.present {
		return nil
	}
	return []streamDesc{{StreamID: fakeContentVideoStream, GroupID: fakeContentVideoStream, Framerate: shareFPS,
		MaxBitrateKbps: shareMaxKbps, AttendeeID: fakeSelfAtt + "#content", MediaType: mediaVideo}}
}

func (f *fakeChime) negotiateContent(offer string) (string, error) {
	if !strings.Contains(offer, "o=mozilla-chrome") {
		f.errf("content offer origin not rewritten")
	}
	ct := &f.content
	ct.mu.Lock()
	pc := ct.pc
	ct.mu.Unlock()
	if pc == nil {
		m := &webrtc.MediaEngine{}
		if err := m.RegisterDefaultCodecs(); err != nil {
			return "", err
		}
		se := webrtc.SettingEngine{}
		se.SetIncludeLoopbackCandidate(true)
		se.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4})
		api := webrtc.NewAPI(webrtc.WithMediaEngine(m), webrtc.WithSettingEngine(se))
		var err error
		if pc, err = api.NewPeerConnection(webrtc.Configuration{}); err != nil {
			return "", err
		}
		pc.OnTrack(func(t *webrtc.TrackRemote, _ *webrtc.RTPReceiver) { f.recvContent(pc, t) })
		ct.mu.Lock()
		ct.pc = pc
		ct.mu.Unlock()
	}
	if err := pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: offer}); err != nil {
		return "", err
	}
	ans, err := pc.CreateAnswer(nil)
	if err != nil {
		return "", err
	}
	g := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(ans); err != nil {
		return "", err
	}
	select {
	case <-g:
	case <-time.After(10 * time.Second):
	}
	return pc.LocalDescription().SDP, nil
}

// recvContent counts the content attendee's audio and decodes its video
// like a receiver (fakeVideoIn.recv).
func (f *fakeChime) recvContent(pc *webrtc.PeerConnection, track *webrtc.TrackRemote) {
	ct := &f.content
	if track.Kind() == webrtc.RTPCodecTypeAudio {
		for {
			if _, _, err := track.ReadRTP(); err != nil {
				return
			}
			ct.audioPkts.Add(1)
		}
	}
	ct.mime.Store(track.Codec().MimeType)
	ct.recv(track, 1024, func() {
		_ = pc.WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{MediaSSRC: uint32(track.SSRC())}})
	})
}
