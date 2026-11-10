package huddle

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/huddle/bgblur"
)

// stillCamera sends one picture over and over, like a webcam pointed at
// someone sitting still. After each frame comes back from the session (on
// the camera goroutine, so without racing the self view's buffers) it
// records how much the background and the face changed.
type stillCamera struct {
	f        *bgblur.Frame
	fps      int
	stop     chan struct{}
	once     sync.Once
	bg, face atomic.Int64
	n        atomic.Int64
}

func newStillCamera(w, h int) *stillCamera {
	img, _ := bgblur.SyntheticPerson(w, h)
	return &stillCamera{f: bgblur.FrameFromRGBA(img), fps: 15, stop: make(chan struct{})}
}

func (c *stillCamera) frame() *CameraFrame {
	return &CameraFrame{W: c.f.W, H: c.f.H, Y: append([]byte(nil), c.f.Y...), UV: append([]byte(nil), c.f.UV...),
		YStride: c.f.YStride, UVStride: c.f.UVStride, At: time.Now()}
}

func (c *stillCamera) Start(onFrame func(*CameraFrame)) error {
	go func() {
		t := time.NewTicker(time.Second / time.Duration(c.fps))
		defer t.Stop()
		for {
			select {
			case <-c.stop:
				return
			case <-t.C:
			}
			fr := c.frame()
			onFrame(fr)
			bg, face := c.changed(fr)
			c.bg.Store(int64(bg))
			c.face.Store(int64(face))
			c.n.Add(1)
		}
	}()
	return nil
}

// changed sums |ΔY| over 8×8 blocks of the shelf (right of the person)
// and of the face, between fr and the camera's picture.
func (c *stillCamera) changed(fr *CameraFrame) (bg, face int) {
	w, h := c.f.W, c.f.H
	block := func(x, y int) (d int) {
		for dy := 0; dy < 8; dy++ {
			for dx := 0; dx < 8; dx++ {
				i := (y+dy)*c.f.YStride + x + dx
				v := int(fr.Y[i]) - int(c.f.Y[i])
				d += max(v, -v)
			}
		}
		return
	}
	for _, p := range [][2]int{{w * 86 / 100, h * 30 / 100}, {w * 80 / 100, h * 55 / 100}, {w * 90 / 100, h * 80 / 100}} {
		bg += block(p[0], p[1])
	}
	return bg, block(w/2-4, h*44/100)
}

// blurred: the last frame's background changed and its face didn't.
func (c *stillCamera) blurred() bool { return c.bg.Load() > 150 && c.face.Load() < 20 }
func (c *stillCamera) plain() bool   { return c.n.Load() > 0 && c.bg.Load() == 0 && c.face.Load() == 0 }
func (c *stillCamera) info() string  { return itoa64(c.bg.Load()) + " / " + itoa64(c.face.Load()) }

func (c *stillCamera) Close() { c.once.Do(func() { close(c.stop) }) }

// TestVideoEffectToggle: with "blur" the camera frames are blurred before
// the self view and the encoder; switching it off mid-huddle stops it and
// frees the model, switching it back on resumes. Fake Chime only.
func TestVideoEffectToggle(t *testing.T) {
	fk := NewFake(nil, t.Errorf)
	defer fk.Close()
	cam := newStillCamera(320, 180)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, err := Join(ctx, Options{
		Client: fk.Caller(), SelfID: fakeSelf, Device: NewToneDevice(440), Logf: t.Logf, VideoEffect: VideoEffectBlur,
		OpenCamera: func(func(string, ...any)) (Camera, error) { return cam, nil },
	}, "C0TEST")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Leave()
	if s.VideoEffect() != VideoEffectBlur {
		t.Fatalf("effect %q from Options", s.VideoEffect())
	}
	waitFor(t, 15*time.Second, "media", func() bool { return s.State().Phase == Connected }, func() string { return s.State().Phase.String() })
	s.SetCamera(true)
	fx := &s.cam.fx
	waitFor(t, 20*time.Second, "blurred frames sent", func() bool {
		return fx.applied.Load() >= 6 && s.cam.sent.Load() >= 4 && cam.blurred()
	}, func() string { return s.State().Camera.State.String() + " " + s.Stats() + "; " + cam.info() })
	t.Logf("blur: %d frames processed, last %v", fx.applied.Load(), time.Duration(fx.took.Load()))
	if img := s.Frame(SelfVideoKey).Img; img == nil {
		t.Error("no self view")
	}

	s.SetVideoEffect(VideoEffectNone)
	waitFor(t, 5*time.Second, "the plain camera", func() bool {
		fx.runMu.Lock()
		freed := fx.fx == nil
		fx.runMu.Unlock()
		return freed && cam.plain()
	}, cam.info)
	n := fx.applied.Load()
	sent := s.cam.sent.Load()
	waitFor(t, 5*time.Second, "frames still sent", func() bool { return s.cam.sent.Load() >= sent+3 }, func() string { return s.Stats() })
	if fx.applied.Load() != n {
		t.Errorf("frames processed with the effect off")
	}

	s.SetVideoEffect(VideoEffectBlur)
	waitFor(t, 5*time.Second, "blur again", func() bool { return fx.applied.Load() > n && cam.blurred() }, cam.info)
	// a background image isn't implemented: the camera goes out unchanged
	s.SetVideoEffect(VideoEffectImage)
	waitFor(t, 5*time.Second, "bg means plain", cam.plain, cam.info)
}

// TestVideoEffectPreview: the join preview's camera shows the effect too.
func TestVideoEffectPreview(t *testing.T) {
	cam := newStillCamera(320, 180)
	dev := NewToneDevice(440)
	defer dev.Close()
	p := StartPreview(Options{Device: dev, VideoEffect: VideoEffectBlur, OpenCamera: func(func(string, ...any)) (Camera, error) { return cam, nil }}, true)
	defer p.Close()
	until(t, "a blurred preview", func() bool { return cam.blurred() && p.Frame().Img != nil })
	p.SetVideoEffect(VideoEffectNone)
	until(t, "a plain preview", cam.plain)
}
