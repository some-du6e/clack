package huddle

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Preview is the join preview's devices before a huddle (Slack's
// HuddleInviteWindowWithPreview: useJoinPreviewCameraUI's camera preview,
// useJoinPreviewMic and the mic level that lights the talking pill). It
// opens the microphone and, on request, the camera, with the same
// permission flow as a Session, and sends nothing anywhere. Its methods
// are safe for concurrent use and never block on the devices.
type Preview struct {
	o      Options
	ctx    context.Context
	cancel context.CancelFunc

	mu      sync.Mutex
	mic     MicStatus
	micErr  string
	micDev  Device
	level   float64 // the last 250 ms's RMS, 0..1
	levelAt time.Time
	camWant bool
	camGen  uint64
	camSt   CameraStatus
	cam     Camera
	closed  bool

	preview selfPreview
}

// StartPreview opens the preview's microphone (Options.Device,
// Options.MicAccess/OpenAudio, or the default one) and, if camera, the
// camera. OnChange runs (coalesced by the caller) when something changed.
func StartPreview(o Options, camera bool) *Preview {
	if o.Logf == nil {
		o.Logf = func(string, ...any) {}
	}
	p := &Preview{o: o, mic: MicStarting}
	p.ctx, p.cancel = context.WithCancel(context.Background())
	go p.openMic()
	if camera {
		p.SetCamera(true)
	}
	return p
}

func (p *Preview) changed() {
	if p.o.OnChange != nil {
		p.o.OnChange()
	}
}

func (p *Preview) openMic() {
	var dev Device
	var err error
	switch {
	case p.o.Device != nil:
		dev = p.o.Device
	default:
		access := p.o.MicAccess
		if access != nil {
			var st MicStatus
			select {
			case s := <-access():
				st = micFromPerm(s)
			case <-p.ctx.Done():
				return
			}
			if st != MicOK {
				p.setMic(st, "", nil)
				return
			}
		}
		open := p.o.OpenAudio
		if open == nil {
			open = openDefaultDevice
		}
		if open == nil {
			p.setMic(MicFailed, "this build has no audio support", nil)
			return
		}
		dev, err = open(p.o.Logf)
	}
	if err == nil && dev == nil {
		err = errors.New("no microphone")
	}
	if err == nil {
		err = dev.Start(p.capture, nil)
	}
	if err != nil {
		if dev != nil {
			dev.Close()
		}
		p.setMic(MicFailed, err.Error(), nil)
		return
	}
	p.setMic(MicOK, "", dev)
}

func (p *Preview) setMic(st MicStatus, why string, dev Device) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		if dev != nil {
			dev.Close()
		}
		return
	}
	p.mic, p.micErr, p.micDev = st, why, dev
	p.mu.Unlock()
	p.changed()
}

// capture keeps a 250 ms level (Slack throttles the level subscription to
// 250 ms, leading and trailing).
func (p *Preview) capture(pcm []int16) {
	lv := level(pcm)
	now := time.Now()
	p.mu.Lock()
	old := p.level
	if now.Sub(p.levelAt) > 250*time.Millisecond {
		p.level, p.levelAt = lv, now
	} else {
		p.level = max(p.level, lv)
	}
	changed := (old > speakingLevel) != (p.level > speakingLevel)
	p.mu.Unlock()
	if changed {
		p.changed()
	}
}

// speakingLevel is the RMS over which you count as talking (the session's
// SpokeAt threshold).
const speakingLevel = 0.02

// Mic is the microphone's status (MicOK once it delivers).
func (p *Preview) Mic() (MicStatus, string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.mic, p.micErr
}

// Level is the microphone's recent RMS, 0..1.
func (p *Preview) Level() float64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	if time.Since(p.levelAt) > time.Second {
		return 0
	}
	return p.level
}

// Talking is Slack's talking pill and border: the level is above zero
// (getLevel > 0) on a live microphone.
func (p *Preview) Talking() bool { return p.Level() > speakingLevel }

// SetCamera turns the preview camera on or off; it returns at once.
func (p *Preview) SetCamera(on bool) {
	p.mu.Lock()
	p.camWant = on
	p.camGen++
	gen := p.camGen
	cam := p.cam
	if on {
		if p.camSt.State != CameraOn {
			p.camSt = CameraStatus{State: CameraStarting}
		}
	} else {
		p.camSt, p.cam = CameraStatus{}, nil
	}
	p.mu.Unlock()
	if !on {
		if cam != nil {
			cam.Close()
		}
		p.preview.clear()
		p.changed()
		return
	}
	p.changed()
	if cam != nil {
		return
	}
	go func() {
		c, err := openCameraWith(p.ctx, p.o, p.o.Logf)
		p.mu.Lock()
		stale := gen != p.camGen || p.closed
		if !stale {
			if err != nil {
				st := CameraStatus{State: CameraFailed, Err: err.Error()}
				switch {
				case errors.Is(err, ErrCameraUnsupported):
					st.State = CameraUnsupported
				case errors.Is(err, errCameraDenied):
					st.State = CameraDenied
				}
				p.camSt, p.camWant = st, false
			} else {
				p.cam = c
			}
		}
		p.mu.Unlock()
		if stale {
			if c != nil {
				c.Close()
			}
			return
		}
		if err != nil {
			p.changed()
			return
		}
		p.preview.start()
		if err := c.Start(func(f *CameraFrame) { p.preview.offer(f, p.changed) }); err != nil {
			p.SetCamera(false)
			return
		}
		p.mu.Lock()
		if gen == p.camGen {
			p.camSt = CameraStatus{State: CameraOn}
		}
		p.mu.Unlock()
		p.changed()
	}()
}

// Camera is the preview camera's status.
func (p *Preview) Camera() CameraStatus {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.camSt
}

// Frame is the latest mirrored camera picture (Img nil while off).
func (p *Preview) Frame() VideoFrame { return p.preview.latest() }

// Close stops the devices. The Device passed in Options is the caller's
// (it may become the session's) and isn't closed.
func (p *Preview) Close() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	mic, cam := p.micDev, p.cam
	p.micDev, p.cam = nil, nil
	p.mu.Unlock()
	p.cancel()
	if mic != nil && mic != p.o.Device {
		mic.Close()
	}
	if cam != nil {
		cam.Close()
	}
	p.preview.clear()
}
