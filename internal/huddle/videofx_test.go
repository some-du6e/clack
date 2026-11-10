package huddle

import (
	"context"
	"image"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/huddle/bgblur"
)

// stillCamera sends one picture over and over, like a webcam pointed at
// someone sitting still.
type stillCamera struct {
	f    *bgblur.Frame
	fps  int
	stop chan struct{}
	once sync.Once
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
			onFrame(c.frame())
		}
	}()
	return nil
}

func (c *stillCamera) Close() { c.once.Do(func() { close(c.stop) }) }

// plainPreview is the self view of the unprocessed picture.
func (c *stillCamera) plainPreview() *image.RGBA {
	f := c.frame()
	img := image.NewRGBA(image.Rect(0, 0, f.W, f.H))
	nv12ToRGBAMirrored(img, f, 1)
	return img
}

// diffAt is the summed RGB difference of a's and b's 8×8 block at (x, y).
func diffAt(a, b *image.RGBA, x, y int) int {
	d := 0
	for dy := 0; dy < 8; dy++ {
		for dx := 0; dx < 8; dx++ {
			i := (y+dy)*a.Stride + 4*(x+dx)
			for c := 0; c < 3; c++ {
				v := int(a.Pix[i+c]) - int(b.Pix[i+c])
				if v < 0 {
					v = -v
				}
				d += v
			}
		}
	}
	return d
}

// blurredView reports whether the self view's background (the shelf, on
// the left once mirrored) is blurred while the face isn't.
func blurredView(img, plain *image.RGBA) (bg, face int) {
	w, h := plain.Rect.Dx(), plain.Rect.Dy()
	for _, p := range [][2]int{{w * 6 / 100, h * 30 / 100}, {w * 12 / 100, h * 55 / 100}, {w * 18 / 100, h * 80 / 100}} {
		bg += diffAt(img, plain, p[0], p[1])
	}
	face = diffAt(img, plain, w/2-4, h*44/100)
	return
}

// TestVideoEffectToggle: with "blur" the camera frames are blurred before
// the self view and the encoder; switching it off mid-huddle stops it and
// frees the model, switching it back on resumes. Fake Chime only.
func TestVideoEffectToggle(t *testing.T) {
	fk := NewFake(nil, t.Errorf)
	defer fk.Close()
	f := fk.f
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
	plain := cam.plainPreview()
	view := func() *image.RGBA { return s.Frame(SelfVideoKey).Img }
	waitFor(t, 20*time.Second, "blurred frames at the fake", func() bool {
		return fx.applied.Load() >= 6 && f.cam.frames.Load() >= 4 && view() != nil
	}, func() string { return s.State().Camera.State.String() + " " + s.Stats() })
	t.Logf("blur: %d frames processed, last %v", fx.applied.Load(), time.Duration(fx.took.Load()))
	waitFor(t, 5*time.Second, "a blurred self view", func() bool {
		bg, face := blurredView(view(), plain)
		return bg > 600 && face < 40
	}, func() string {
		bg, face := blurredView(view(), plain)
		return itoa64(int64(bg)) + " / " + itoa64(int64(face))
	})

	s.SetVideoEffect(VideoEffectNone)
	waitFor(t, 5*time.Second, "the plain camera", func() bool {
		fx.runMu.Lock()
		freed := fx.fx == nil
		fx.runMu.Unlock()
		if view() == nil {
			return false
		}
		bg, _ := blurredView(view(), plain)
		return freed && bg < 40
	}, func() string { bg, _ := blurredView(view(), plain); return itoa64(int64(bg)) })
	n := fx.applied.Load()
	sent := f.cam.frames.Load()
	waitFor(t, 5*time.Second, "frames still sent", func() bool { return f.cam.frames.Load() >= sent+3 }, func() string { return s.Stats() })
	if fx.applied.Load() != n {
		t.Errorf("frames processed with the effect off")
	}

	s.SetVideoEffect(VideoEffectBlur)
	waitFor(t, 5*time.Second, "blur again", func() bool {
		bg, _ := blurredView(view(), plain)
		return fx.applied.Load() > n && bg > 600
	}, func() string { return itoa64(fx.applied.Load()) })
	// a background image isn't implemented: the camera goes out unchanged
	s.SetVideoEffect(VideoEffectImage)
	waitFor(t, 5*time.Second, "bg means plain", func() bool { bg, _ := blurredView(view(), plain); return bg < 40 }, func() string { return "" })
}

// TestVideoEffectPreview: the join preview's camera shows the effect too.
func TestVideoEffectPreview(t *testing.T) {
	cam := newStillCamera(320, 180)
	dev := NewToneDevice(440)
	defer dev.Close()
	p := StartPreview(Options{Device: dev, VideoEffect: VideoEffectBlur, OpenCamera: func(func(string, ...any)) (Camera, error) { return cam, nil }}, true)
	defer p.Close()
	plain := cam.plainPreview()
	view := func() *image.RGBA { return p.Frame().Img }
	until(t, "a blurred preview", func() bool {
		if view() == nil {
			return false
		}
		bg, face := blurredView(view(), plain)
		return bg > 600 && face < 40
	})
	p.SetVideoEffect(VideoEffectNone)
	until(t, "a plain preview", func() bool { bg, _ := blurredView(view(), plain); return bg < 40 })
}
