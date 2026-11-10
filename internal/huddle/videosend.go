package huddle

// Camera video send. Chime publishes a camera the way amazon-chime-sdk-js
// does without simulcast (DefaultVideoStreamIndex + DefaultSignalingClient):
//
//   - The first video m-section of the offer is the local camera. It is
//     recvonly (inactive in the SDK) while the camera is off, and becomes
//     sendrecv with the camera track when it is on; each change is a fresh
//     offer → SUBSCRIBE → SUBSCRIBE_ACK → answer.
//   - SUBSCRIBE's duplex goes from RX to DUPLEX, and send_streams gets a
//     second SdkStreamDescriptor after the audio one: stream_id 2,
//     group_id 2, track_label "AmazonChimeExpressVideo", media_type VIDEO,
//     your attendee id, framerate and max_bitrate_kbps from the uplink
//     policy (15 fps, 1500 kbps for a small huddle), avg_bitrate_bps 0.
//   - receive_stream_ids keeps 0 for the camera's own m-section.
//   - Chime then lists the stream in everyone's INDEX under your attendee
//     id (the receive side already skips your own), and asks for keyframes
//     with RTCP PLI or FIR, and caps the rate with REMB.
//
// Slack's huddle clients send simulcast layers (INDEX shows several streams
// per camera group); the SDK's single-stream mode is what Chime accepts from
// browsers without simulcast, and is all Clack sends.

import (
	"context"
	"errors"
	"fmt"
	"image"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"

	"github.com/some-du6e/clack/internal/huddle/avperm"
)

// CameraState is where your camera is.
type CameraState int

const (
	CameraOff         CameraState = iota
	CameraStarting                // asking for access, opening the camera, renegotiating
	CameraOn                      // capturing and publishing
	CameraDenied                  // the OS denied access: avperm.SettingsURL(avperm.Camera)
	CameraUnsupported             // no camera backend on this platform
	CameraFailed                  // Err says why
)

func (c CameraState) String() string {
	return [...]string{"off", "starting", "on", "denied", "unsupported", "failed"}[c]
}

// CameraStatus is your camera's state and, after a failure, why.
type CameraStatus struct {
	State CameraState
	Err   string
}

// SelfVideoKey is Session.Frame's key for your own camera preview, already
// mirrored like Slack's self view (what others see isn't mirrored).
const SelfVideoKey = "self#camera"

// The camera stream as the Chime SDK describes it (see the top of the file).
const (
	camStreamID   = 2
	camGroupID    = 2
	camTrackLabel = "AmazonChimeExpressVideo"
	camFPS        = 15
	camMaxKbps    = 1500
	camBitrate    = 1_000_000 // encoder target, bps
	camMinBitrate = 150_000
	streamDuplex  = 3 // SdkStreamServiceType.DUPLEX
)

// camPipe is the camera half of a Session.
type camPipe struct {
	opMu sync.Mutex // serializes turning the camera on and off

	mu     sync.Mutex
	want   bool
	gen    uint64
	status CameraStatus
	cam    Camera
	track  *webrtc.TrackLocalStaticSample
	sender *webrtc.RTPSender
	trans  *webrtc.RTPTransceiver // the current pc's first video m-section
	pc     *webrtc.PeerConnection
	api    *webrtc.API

	encMu   sync.Mutex // the encoder belongs to the camera goroutine
	enc     videoEncoder
	encSize [2]int
	encBps  int
	lastAt  time.Time
	errAt   time.Time
	keyReq  atomic.Bool
	remb    atomic.Int64
	sent    atomic.Int64
	preview selfPreview
	fx      videoFX // the background effect, before preview and encoder
}

// SetCamera turns your camera on or off. It returns at once: asking the OS
// for access, opening the camera, starting the encoder and renegotiating
// run on a goroutine; watch State().Camera.
func (s *Session) SetCamera(on bool) {
	c := &s.cam
	c.mu.Lock()
	c.want = on
	c.gen++
	gen := c.gen
	if on {
		if c.status.State != CameraOn {
			c.status = CameraStatus{State: CameraStarting}
		}
	} else {
		c.status = CameraStatus{}
	}
	c.mu.Unlock()
	s.changed()
	go s.applyCamera(gen)
}

// CameraStatus is your camera's state (also State().Camera).
func (s *Session) CameraStatus() CameraStatus {
	s.cam.mu.Lock()
	defer s.cam.mu.Unlock()
	return s.cam.status
}

func (s *Session) setCamStatus(gen uint64, st CameraStatus) {
	c := &s.cam
	c.mu.Lock()
	if gen == c.gen {
		c.status = st
	}
	c.mu.Unlock()
	s.changed()
}

func (s *Session) applyCamera(gen uint64) {
	c := &s.cam
	c.opMu.Lock()
	defer c.opMu.Unlock()
	c.mu.Lock()
	cur, want := c.gen, c.want
	c.mu.Unlock()
	if gen != cur || s.ctx.Err() != nil {
		return // a newer SetCamera runs after this one
	}
	if !want {
		s.cameraOff()
		return
	}
	if err := s.cameraOn(gen); err != nil {
		st := CameraStatus{State: CameraFailed, Err: err.Error()}
		switch {
		case errors.Is(err, ErrCameraUnsupported):
			st.State = CameraUnsupported
		case errors.Is(err, errCameraDenied):
			st.State = CameraDenied
		}
		s.logf("huddle: camera: %v", err)
		s.cameraOff()
		c.mu.Lock()
		if gen == c.gen {
			c.want = false
		}
		c.mu.Unlock()
		s.setCamStatus(gen, st)
	}
}

var errCameraDenied = errors.New("camera access denied")

func (s *Session) cameraOn(gen uint64) error {
	c := &s.cam
	c.mu.Lock()
	cam := c.cam
	c.mu.Unlock()
	if cam == nil {
		var err error
		if cam, err = s.openCamera(); err != nil {
			return err
		}
		c.mu.Lock()
		stale := gen != c.gen
		if !stale {
			c.cam = cam
		}
		c.mu.Unlock()
		if stale { // turned off (or on again) while we asked
			cam.Close()
			return nil
		}
		c.keyReq.Store(true)
		c.preview.start()
		if err := cam.Start(s.onCameraFrame); err != nil {
			return err
		}
	}
	if err := s.publishCamera(); err != nil {
		return fmt.Errorf("publish: %w", err)
	}
	s.setCamStatus(gen, CameraStatus{State: CameraOn})
	s.logf("huddle: camera on")
	return nil
}

// openCamera asks for access (the macOS prompt, without blocking any UI)
// and opens the camera: Options.OpenCamera, the test pattern
// (CLACK_HUDDLE_TESTCAM), or the system default.
func (s *Session) openCamera() (Camera, error) { return openCameraWith(s.ctx, s.o, s.logf) }

// openCameraWith is openCamera for a session's options or the join
// preview's.
func openCameraWith(ctx context.Context, o Options, logf func(string, ...any)) (Camera, error) {
	if o.OpenCamera != nil {
		return o.OpenCamera(logf)
	}
	if testCamEnv() {
		w, h := testCamSize()
		return NewTestCamera(w, h, camFPS), nil
	}
	if openDefaultCamera == nil {
		return nil, ErrCameraUnsupported
	}
	st := avperm.Check(avperm.Camera)
	if st == avperm.NotDetermined {
		select {
		case st = <-avperm.Request(avperm.Camera):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	switch st {
	case avperm.Authorized:
	case avperm.Denied, avperm.Restricted:
		return nil, fmt.Errorf("%w (%s)", errCameraDenied, st)
	default:
		return nil, fmt.Errorf("can't ask for camera access: %s", st)
	}
	return openDefaultCamera(logf)
}

// cameraAttach is setupMedia's hook for each new peer connection: t is the
// first video transceiver. With the camera on, it gets the camera track
// (sendrecv) before the first offer.
func (s *Session) cameraAttach(api *webrtc.API, pc *webrtc.PeerConnection, t *webrtc.RTPTransceiver) {
	c := &s.cam
	c.mu.Lock()
	defer c.mu.Unlock()
	c.api, c.pc, c.trans, c.sender = api, pc, t, nil
	if c.want && c.cam != nil {
		if err := s.attachLocked(); err != nil {
			s.logf("huddle: camera: %v", err)
		}
	}
}

// attachLocked puts the camera track on the camera m-section (c.mu held).
func (s *Session) attachLocked() error {
	c := &s.cam
	if c.sender != nil {
		return nil
	}
	if c.pc == nil || c.trans == nil || c.api == nil {
		return errors.New("no peer connection")
	}
	if c.track == nil {
		t, err := webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264, ClockRate: 90000,
			SDPFmtpLine: "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42e01f"}, "camera", "clack")
		if err != nil {
			return err
		}
		c.track = t
	}
	sender, err := c.api.NewRTPSender(c.track, c.pc.SCTP().Transport())
	if err != nil {
		return err
	}
	if err := c.trans.SetSender(sender, c.track); err != nil {
		return err
	}
	c.sender = sender
	c.keyReq.Store(true)
	go s.readCameraRTCP(sender)
	return nil
}

// publishCamera sends the camera on the current connection (if there is
// none yet, setupMedia attaches it when media connects).
func (s *Session) publishCamera() error {
	c := &s.cam
	c.mu.Lock()
	if c.pc == nil {
		c.mu.Unlock()
		return nil
	}
	had := c.sender != nil
	err := s.attachLocked()
	c.mu.Unlock()
	if err != nil || had {
		return err
	}
	return s.subscribe(false)
}

// cameraOff stops capture and, if the camera was published, renegotiates
// the camera m-section back to recvonly.
func (s *Session) cameraOff() {
	c := &s.cam
	c.mu.Lock()
	cam, sender, pc := c.cam, c.sender, c.pc
	c.cam, c.sender = nil, nil
	c.mu.Unlock()
	if cam != nil {
		cam.Close()
	}
	c.encMu.Lock()
	if c.enc != nil {
		c.enc.Close()
		c.enc = nil
	}
	c.encMu.Unlock()
	c.preview.clear()
	c.fx.close()
	if sender != nil && pc != nil {
		if err := pc.RemoveTrack(sender); err != nil {
			s.logf("huddle: camera: remove track: %v", err)
		}
		if s.ctx.Err() == nil {
			if err := s.subscribe(false); err != nil {
				s.logf("huddle: camera off: resubscribe: %v", err)
			}
		}
		s.logf("huddle: camera off")
	}
	s.changed()
}

// closeCamera is finish's: stop everything without renegotiating.
func (s *Session) closeCamera() {
	c := &s.cam
	c.mu.Lock()
	cam := c.cam
	c.cam, c.sender, c.pc, c.trans, c.want = nil, nil, nil, nil, false
	c.gen++
	if c.status.State != CameraOff {
		c.status = CameraStatus{}
	}
	c.mu.Unlock()
	if cam != nil {
		cam.Close()
	}
	c.encMu.Lock()
	if c.enc != nil {
		c.enc.Close()
		c.enc = nil
	}
	c.encMu.Unlock()
	c.preview.clear()
	c.fx.close()
}

// cameraDesc is SUBSCRIBE's video send stream while the camera is
// published (nil otherwise).
func (s *Session) cameraDesc() *streamDesc {
	c := &s.cam
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sender == nil {
		return nil
	}
	return &streamDesc{StreamID: camStreamID, Framerate: camFPS, MaxBitrateKbps: camMaxKbps, TrackLabel: camTrackLabel,
		GroupID: camGroupID, AttendeeID: s.join.Attendee.AttendeeID, MediaType: mediaVideo}
}

// readCameraRTCP turns Chime's PLI/FIR into a keyframe and its REMB into
// the encoder's target. It ends when the sender stops (RemoveTrack, close).
func (s *Session) readCameraRTCP(sender *webrtc.RTPSender) {
	c := &s.cam
	for {
		pkts, _, err := sender.ReadRTCP()
		if err != nil {
			return
		}
		for _, p := range pkts {
			switch p := p.(type) {
			case *rtcp.PictureLossIndication, *rtcp.FullIntraRequest:
				c.keyReq.Store(true)
			case *rtcp.ReceiverEstimatedMaximumBitrate:
				c.remb.Store(int64(p.Bitrate))
			}
		}
	}
}

// onCameraFrame runs on the camera's goroutine: the background effect,
// the preview, then (when published) encode and send.
func (s *Session) onCameraFrame(f *CameraFrame) {
	c := &s.cam
	if f == nil || f.W < 16 || f.H < 16 {
		return
	}
	c.fx.apply(f, s.logf)
	c.preview.offer(f, s.changed)
	c.mu.Lock()
	track, sending := c.track, c.sender != nil && c.want
	c.mu.Unlock()
	if !sending {
		return
	}
	c.encMu.Lock()
	defer c.encMu.Unlock()
	c.mu.Lock()
	sending = c.sender != nil && c.want // cameraOff may have run meanwhile
	c.mu.Unlock()
	if !sending {
		return
	}
	if c.enc == nil || c.encSize != [2]int{f.W, f.H} {
		if c.enc != nil {
			c.enc.Close()
			c.enc = nil
		}
		enc, err := newVideoEncoder(f.W, f.H, camFPS, camBitrate, s.logf)
		if err != nil {
			if time.Since(c.errAt) > 10*time.Second {
				c.errAt = time.Now()
				s.logf("huddle: camera encoder: %v", err)
			}
			return
		}
		s.logf("huddle: camera %dx%d, %s H.264", f.W, f.H, enc.Name())
		c.enc, c.encBps = enc, camBitrate
		c.encSize = [2]int{f.W, f.H}
		c.keyReq.Store(true)
	}
	if r := c.remb.Load(); r > 0 {
		bps := int(max(camMinBitrate, min(camBitrate, r*9/10)))
		if d := bps - c.encBps; d > c.encBps/10 || -d > c.encBps/10 {
			c.enc.SetBitrate(bps)
			c.encBps = bps
		}
	}
	key := c.keyReq.Swap(false)
	au, err := c.enc.Encode(f, key)
	if err != nil || au == nil {
		if key {
			c.keyReq.Store(true)
		}
		if err != nil && time.Since(c.errAt) > 10*time.Second {
			c.errAt = time.Now()
			s.logf("huddle: camera encode: %v", err)
		}
		return
	}
	dur := time.Second / camFPS
	if !c.lastAt.IsZero() {
		dur = min(max(f.At.Sub(c.lastAt), time.Millisecond), 200*time.Millisecond)
	}
	c.lastAt = f.At
	if track.WriteSample(media.Sample{Data: au, Duration: dur}) == nil {
		c.sent.Add(1)
	}
}

// ---- self preview ----

// selfPreview keeps your own camera as a mirrored RGBA picture, converted
// on the camera goroutine only while someone looks (Session.Frame) and at
// most maxFPS times a second, scaled down to at most 480 px wide.
type selfPreview struct {
	mu        sync.Mutex
	on        bool // between start and clear: a late frame doesn't come back
	frame     VideoFrame
	pool      [3]*image.RGBA
	next      int
	wanted    atomic.Int64
	lastShown time.Time
}

func (p *selfPreview) offer(f *CameraFrame, notify func()) {
	now := time.Now()
	p.mu.Lock()
	shown, on := !p.lastShown.IsZero(), p.on
	last := p.lastShown
	p.mu.Unlock()
	if !on {
		return
	}
	if shown && now.UnixNano()-p.wanted.Load() > int64(2*time.Second) {
		return
	}
	if now.Sub(last) < time.Second/maxFPS {
		return
	}
	step := 1
	for f.W/step > 480 {
		step *= 2
	}
	w, h := f.W/step, f.H/step
	p.mu.Lock()
	img := p.pool[p.next]
	if img == nil || img.Rect.Dx() != w || img.Rect.Dy() != h {
		img = image.NewRGBA(image.Rect(0, 0, w, h))
		p.pool[p.next] = img
	}
	p.next = (p.next + 1) % len(p.pool)
	p.mu.Unlock()
	nv12ToRGBAMirrored(img, f, step)
	out := &image.RGBA{Pix: img.Pix, Stride: img.Stride, Rect: img.Rect}
	p.mu.Lock()
	if !p.on {
		p.mu.Unlock()
		return
	}
	p.frame = VideoFrame{Img: out, Seq: p.frame.Seq + 1, At: now}
	p.lastShown = now
	p.mu.Unlock()
	if notify != nil {
		notify()
	}
}

func (p *selfPreview) latest() VideoFrame {
	p.wanted.Store(time.Now().UnixNano())
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.frame
}

func (p *selfPreview) start() {
	p.mu.Lock()
	p.on = true
	p.mu.Unlock()
}

func (p *selfPreview) clear() {
	p.mu.Lock()
	p.on = false
	p.frame = VideoFrame{Seq: p.frame.Seq + 1}
	p.lastShown = time.Time{}
	p.mu.Unlock()
}

// nv12ToRGBAMirrored converts f into dst (f's size divided by step),
// flipped left to right.
func nv12ToRGBAMirrored(dst *image.RGBA, f *CameraFrame, step int) {
	w, h := dst.Rect.Dx(), dst.Rect.Dy()
	for y := 0; y < h; y++ {
		sy := y * step
		yrow := f.Y[sy*f.YStride:]
		crow := f.UV[(sy/2)*f.UVStride:]
		pix := dst.Pix[y*dst.Stride:]
		for x := 0; x < w; x++ {
			sx := f.W - 1 - x*step
			yy := int32(yrow[sx])
			cb := int32(crow[sx&^1]) - 128
			cr := int32(crow[sx|1]) - 128
			var r, g, b int32
			if f.FullRange {
				yy <<= 16
				r = (yy + 91881*cr) >> 16
				g = (yy - 22554*cb - 46802*cr) >> 16
				b = (yy + 116130*cb) >> 16
			} else {
				yy = (yy - 16) * 76309
				r = (yy + 104597*cr) >> 16
				g = (yy - 25675*cb - 53279*cr) >> 16
				b = (yy + 132201*cb) >> 16
			}
			i := x * 4
			pix[i] = clamp8(r)
			pix[i+1] = clamp8(g)
			pix[i+2] = clamp8(b)
			pix[i+3] = 0xff
		}
	}
}
