package huddle

// The content attendee: your screen share's own Chime session (share.go
// has Slack's flow). It is the Chime SDK's contentAudioVideo, a second
// DefaultAudioVideoController made by createContentShareMeetingSession-
// Configure: the same signaling URL, attendee id and join token with
// "#content" appended, JOIN → JOIN_ACK and INDEX, then one peer
// connection with an audio m-section (the system audio, or silence:
// ContentShareMediaStreamBroker's synthesized track) and one video
// m-section sending the screen, SUBSCRIBE with duplex DUPLEX and the two
// send streams, no receive streams (NoVideoDownlinkBandwidthPolicy), and
// LEAVE when the share stops (stopContentShare → contentAudioVideo.stop).

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"

	opus "github.com/kazzmir/opus-go/opus"
)

// contentConn is one screen share's content attendee.
type contentConn struct {
	s      *Session
	att    string // <you>#content
	stereo bool   // the system audio goes out (stereo music Opus)

	ctx    context.Context
	cancel context.CancelFunc

	wmu  sync.Mutex // websocket writes
	conn *websocket.Conn

	mu      sync.Mutex
	waiters map[int][]chan *signalFrame
	pc      *webrtc.PeerConnection
	audio   *webrtc.TrackLocalStaticSample
	video   *webrtc.TrackLocalStaticSample
	leaving bool

	up atomic.Bool // negotiated: frames go out
	h264Pub

	sysMu  sync.Mutex
	sysBuf []int16
	sys    chan []int16 // 20 ms system-audio frames, stereo

	started  atomic.Bool
	done     chan struct{} // the connection loop ended
	stopOnce sync.Once
}

func newContentConn(s *Session, audio bool) *contentConn {
	ctx, cancel := context.WithCancel(s.ctx)
	c := &contentConn{s: s, att: s.join.Attendee.AttendeeID + "#content", stereo: audio, ctx: ctx, cancel: cancel,
		waiters: map[int][]chan *signalFrame{}, sys: make(chan []int16, 8), done: make(chan struct{})}
	c.what, c.fps, c.bps, c.minBps, c.screen = "screen share", shareFPS, shareBitrate, shareMinBitrate, true
	return c
}

// sent is how many screen frames went out (Stats).
func (p *sharePipe) sent() int64 {
	p.mu.Lock()
	c := p.conn
	p.mu.Unlock()
	if c == nil {
		return 0
	}
	return c.h264Pub.sent.Load()
}

// start connects and returns once the share's media is negotiated (or
// failed); the connection then runs, reconnecting after drops, until
// stop.
func (c *contentConn) start() error {
	c.started.Store(true)
	ready := make(chan error, 1)
	go c.run(ready)
	select {
	case err := <-ready:
		return err
	case <-c.ctx.Done():
		return c.ctx.Err()
	}
}

func (c *contentConn) run(ready chan error) {
	defer close(c.done)
	first := true
	backoff := time.Second
	for attempt := 0; ; attempt++ {
		err := c.connect(func() {
			if first {
				first = false
				ready <- nil
			}
			attempt, backoff = 0, time.Second
		})
		if c.ctx.Err() != nil {
			if first {
				ready <- c.ctx.Err()
			}
			return
		}
		if first { // the share never started: report it
			ready <- err
			return
		}
		c.closeConn()
		if attempt >= 3 {
			c.s.logf("huddle: screen share connection lost: %v", err)
			go c.s.shareDropped(c, err)
			return
		}
		c.s.logf("huddle: screen share connection lost: %v (reconnecting)", err)
		select {
		case <-time.After(backoff):
		case <-c.ctx.Done():
			return
		}
		backoff *= 2
	}
}

// shareDropped ends a share whose content attendee couldn't reconnect.
func (s *Session) shareDropped(c *contentConn, err error) {
	p := &s.share
	p.mu.Lock()
	cur := p.conn == c
	gen := p.gen
	p.mu.Unlock()
	if !cur {
		return
	}
	p.opMu.Lock()
	s.shareOff()
	p.opMu.Unlock()
	p.mu.Lock()
	if p.gen == gen {
		p.want = false
	}
	p.mu.Unlock()
	s.setShareStatus(gen, ShareStatus{State: ShareFailed, Err: err.Error()})
}

// stop leaves the meeting as the content attendee (LEAVE, LEAVE_ACK) and
// frees everything.
func (c *contentConn) stop() {
	c.stopOnce.Do(func() {
		c.mu.Lock()
		c.leaving = true
		c.mu.Unlock()
		c.up.Store(false)
		ack := c.wait(ftLeaveAck)
		if c.send(encodeLeave()) == nil {
			select {
			case <-ack:
			case <-time.After(1500 * time.Millisecond):
			}
		}
		c.cancel()
		c.closeConn()
		if c.started.Load() {
			select {
			case <-c.done:
			case <-time.After(5 * time.Second):
			}
		}
		c.closeEncoder()
	})
}

func (c *contentConn) closeConn() {
	c.wmu.Lock()
	conn := c.conn
	c.conn = nil
	c.wmu.Unlock()
	c.mu.Lock()
	pc := c.pc
	c.pc, c.audio, c.video = nil, nil, nil
	c.mu.Unlock()
	if conn != nil {
		_ = conn.Close(websocket.StatusNormalClosure, "")
	}
	if pc != nil {
		_ = pc.Close()
	}
}

func (c *contentConn) send(b []byte) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if c.conn == nil {
		return errors.New("huddle: content share not connected")
	}
	// not the session's context: LEAVE goes out while the session ends
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return c.conn.Write(ctx, websocket.MessageBinary, b)
}

func (c *contentConn) wait(t int) <-chan *signalFrame {
	ch := make(chan *signalFrame, 1)
	c.mu.Lock()
	c.waiters[t] = append(c.waiters[t], ch)
	c.mu.Unlock()
	return ch
}

func (c *contentConn) await(ch <-chan *signalFrame, d time.Duration, what string) (*signalFrame, error) {
	select {
	case f := <-ch:
		if f == nil {
			return nil, fmt.Errorf("huddle: content share connection closed waiting for %s", what)
		}
		return f, nil
	case <-time.After(d):
		return nil, fmt.Errorf("huddle: content share timed out waiting for %s", what)
	case <-c.ctx.Done():
		return nil, c.ctx.Err()
	}
}

// connect runs one signaling + media connection until it drops; up runs
// once media is negotiated.
func (c *contentConn) connect(up func()) error {
	ji := c.s.join
	dctx, cancel := context.WithTimeout(c.ctx, 15*time.Second)
	conn, _, err := websocket.Dial(dctx, signalingURL(ji.Meeting.MediaPlacement.SignalingURL), &websocket.DialOptions{
		Subprotocols: []string{"_aws_wt_session", ji.Attendee.JoinToken + "#content"},
		HTTPHeader:   map[string][]string{"Origin": {"https://app.slack.com"}},
	})
	cancel()
	if err != nil {
		return fmt.Errorf("huddle: content share signaling: %w", err)
	}
	conn.SetReadLimit(4 << 20)
	c.wmu.Lock()
	c.conn = conn
	c.wmu.Unlock()
	readErr := make(chan error, 1)
	connDone := make(chan struct{})
	defer close(connDone)
	go func() { readErr <- c.readLoop(conn) }()

	var sid [8]byte
	_, _ = rand.Read(sid[:])
	ackCh, idxCh := c.wait(ftJoinAck), c.wait(ftIndex)
	if err := c.send(encodeJoin(binary.LittleEndian.Uint64(sid[:]) >> 1)); err != nil {
		return err
	}
	ack, err := c.await(ackCh, 10*time.Second, "JOIN_ACK")
	if err != nil {
		return c.firstErr(readErr, err)
	}
	if _, err := c.await(idxCh, 10*time.Second, "INDEX"); err != nil {
		return c.firstErr(readErr, err)
	}
	if err := c.setupMedia(ack.Turn); err != nil {
		return c.firstErr(readErr, err)
	}
	c.keyReq.Store(true)
	c.up.Store(true)
	up()
	c.s.logf("huddle: screen share media connected (attendee %s)", c.att)
	go c.sendAudio(connDone)
	ping := time.NewTicker(10 * time.Second)
	defer ping.Stop()
	var pid uint32
	for {
		select {
		case err := <-readErr:
			c.up.Store(false)
			return err
		case <-ping.C:
			pid++
			_ = c.send(encodePing(pingTypePing, pid))
		case <-c.ctx.Done():
			c.up.Store(false)
			return c.ctx.Err()
		}
	}
}

func (c *contentConn) firstErr(readErr chan error, err error) error {
	select {
	case e := <-readErr:
		if e != nil {
			return e
		}
	case <-time.After(50 * time.Millisecond):
	}
	return err
}

func (c *contentConn) readLoop(conn *websocket.Conn) error {
	defer func() {
		c.mu.Lock()
		for t, ws := range c.waiters {
			for _, w := range ws {
				close(w)
			}
			delete(c.waiters, t)
		}
		c.mu.Unlock()
	}()
	for {
		_, b, err := conn.Read(c.ctx)
		if err != nil {
			return err
		}
		f, err := decodeFrame(b)
		if err != nil {
			continue
		}
		c.mu.Lock()
		ws := c.waiters[f.Type]
		delete(c.waiters, f.Type)
		c.mu.Unlock()
		for _, w := range ws {
			w <- f
		}
		if f.ErrStatus != 0 || f.ErrDesc != "" {
			c.s.logf("huddle: screen share signaling error %d: %s (frame %d)", f.ErrStatus, f.ErrDesc, f.Type)
		}
		if f.Type == ftPingPong && f.PingType == pingTypePing {
			go c.send(encodePing(pingTypePong, f.PingID))
		}
	}
}

// contentDesc is SUBSCRIBE's video send stream: the screen, described
// like a camera without simulcast (stream 2, group 2) under the content
// attendee.
func (c *contentConn) contentDesc() *streamDesc {
	return &streamDesc{StreamID: camStreamID, Framerate: shareFPS, MaxBitrateKbps: shareMaxKbps, TrackLabel: camTrackLabel,
		GroupID: camGroupID, AttendeeID: c.att, MediaType: mediaVideo}
}

func (c *contentConn) setupMedia(turn *turnCreds) error {
	api, err := newAPI()
	if err != nil {
		return err
	}
	cfg := webrtc.Configuration{BundlePolicy: webrtc.BundlePolicyMaxBundle, SDPSemantics: webrtc.SDPSemanticsUnifiedPlan}
	if turn != nil && len(turn.URIs) > 0 {
		cfg.ICEServers = []webrtc.ICEServer{{URLs: turn.URIs, Username: turn.Username, Credential: turn.Password}}
		cfg.ICETransportPolicy = webrtc.ICETransportPolicyRelay
	}
	pc, err := api.NewPeerConnection(cfg)
	if err != nil {
		return err
	}
	at, err := webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2}, "audio", "clack-content")
	if err != nil {
		_ = pc.Close()
		return err
	}
	if _, err := pc.AddTransceiverFromTrack(at, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionSendrecv}); err != nil {
		_ = pc.Close()
		return err
	}
	vt, err := webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264, ClockRate: 90000,
		SDPFmtpLine: "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42e01f"}, "content", "clack-content")
	if err != nil {
		_ = pc.Close()
		return err
	}
	vtr, err := pc.AddTransceiverFromTrack(vt, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionSendrecv})
	if err != nil {
		_ = pc.Close()
		return err
	}
	go c.readRTCP(vtr.Sender())
	pc.OnConnectionStateChange(func(st webrtc.PeerConnectionState) {
		c.s.logf("huddle: screen share peer connection %s", st)
	})
	c.mu.Lock()
	c.pc, c.audio, c.video = pc, at, vt
	c.mu.Unlock()

	offer, err := pc.CreateOffer(nil)
	if err != nil {
		return err
	}
	gathered := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(offer); err != nil {
		return err
	}
	select {
	case <-gathered:
	case <-time.After(10 * time.Second):
		c.s.logf("huddle: screen share ICE gathering slow; sending what we have")
	}
	sdp := strings.Replace(pc.LocalDescription().SDP, "o=- ", "o=mozilla-chrome ", 1)
	ackCh := c.wait(ftSubscribeAck)
	if err := c.send(encodeSubscribe(subscribeReq{SDP: sdp, AudioHost: c.s.join.Meeting.MediaPlacement.AudioHostURL,
		AttendeeID: c.att, Receive: []uint32{0}, Video: c.contentDesc()})); err != nil {
		return err
	}
	ack, err := c.await(ackCh, 15*time.Second, "SUBSCRIBE_ACK")
	if err != nil {
		return err
	}
	if ack.SDPAnswer == "" {
		return errors.New("huddle: content share SUBSCRIBE_ACK without an SDP answer")
	}
	if err := pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: ack.SDPAnswer}); err != nil {
		return fmt.Errorf("huddle: content share answer: %w", err)
	}
	return nil
}

// sendFrame encodes one captured frame on the content attendee's video.
func (c *contentConn) sendFrame(f *CameraFrame) {
	if !c.up.Load() {
		return
	}
	c.mu.Lock()
	track := c.video
	c.mu.Unlock()
	c.send264(f, track)
}

func (c *contentConn) send264(f *CameraFrame, track *webrtc.TrackLocalStaticSample) {
	c.h264Pub.send(f, track, c.up.Load, c.s.logf)
}

// systemAudio collects the capture's system audio (48 kHz stereo) into
// 20 ms frames for sendAudio.
func (c *contentConn) systemAudio(pcm []int16) {
	c.sysMu.Lock()
	defer c.sysMu.Unlock()
	c.sysBuf = append(c.sysBuf, pcm...)
	for len(c.sysBuf) >= 2*frameSamples {
		f := make([]int16, 2*frameSamples)
		copy(f, c.sysBuf)
		c.sysBuf = append(c.sysBuf[:0], c.sysBuf[2*frameSamples:]...)
		select {
		case c.sys <- f:
		default: // behind: drop
		}
	}
}

// sendAudio sends a 20 ms Opus frame every 20 ms on the content
// attendee's audio: the system audio when it's shared, else silence (the
// SDK's synthesized silent track: Chime lists an attendee once its audio
// flows, which is how the others' clients learn the share started).
func (c *contentConn) sendAudio(done <-chan struct{}) {
	ch, app, bps := 1, opus.ApplicationVoIP, 32000
	if c.stereo {
		ch, app, bps = 2, opus.ApplicationAudio, shareAudioBps
	}
	enc, err := opus.NewEncoder(sampleRate, ch, app)
	if err != nil {
		c.s.logf("huddle: screen share opus encoder: %v", err)
		return
	}
	_ = enc.SetBitrate(bps)
	silence := make([]int16, frameSamples*ch)
	mono := make([]int16, frameSamples)
	buf := make([]byte, 1500)
	t := time.NewTicker(20 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-done:
			return
		case <-t.C:
		}
		pcm := silence
		select {
		case f := <-c.sys:
			for len(c.sys) > 3 { // drift: stay near real time
				f = <-c.sys
			}
			if ch == 2 {
				pcm = f
			} else {
				for i := range mono {
					mono[i] = int16((int32(f[2*i]) + int32(f[2*i+1])) / 2)
				}
				pcm = mono
			}
		default:
		}
		n, err := encodeOpus(enc, pcm, buf)
		if err != nil {
			continue
		}
		c.mu.Lock()
		tr := c.audio
		c.mu.Unlock()
		if tr != nil {
			_ = tr.WriteSample(media.Sample{Data: buf[:n], Duration: 20 * time.Millisecond})
		}
	}
}

// encodeOpus is one frame through enc, a libopus abort turned into an
// error (the transpiled libopus panics on its assertions).
func encodeOpus(enc *opus.Encoder, pcm []int16, out []byte) (n int, err error) {
	defer func() {
		if r := recover(); r != nil {
			n, err = 0, fmt.Errorf("opus encoder: %v", r)
		}
	}()
	return enc.Encode(pcm, frameSamples, out)
}
