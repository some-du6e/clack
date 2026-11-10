package huddleui

import (
	"image"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/huddle"
	"github.com/some-du6e/clack/internal/huddle/bgblur"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// fxHost is avHost with the device's camera background effect.
type fxHost struct {
	avHost
	fx huddle.VideoEffect
}

func (h *fxHost) HuddleVideoEffect() huddle.VideoEffect { return h.fx }

// personCamera sends bgblur's synthetic person, a still webcam picture.
type personCamera struct {
	f    *bgblur.Frame
	stop chan struct{}
	once sync.Once
}

func newPersonCamera(w, h int) *personCamera {
	img, _ := bgblur.SyntheticPerson(w, h)
	return &personCamera{f: bgblur.FrameFromRGBA(img), stop: make(chan struct{})}
}

func (c *personCamera) Start(onFrame func(*huddle.CameraFrame)) error {
	go func() {
		t := time.NewTicker(time.Second / 15)
		defer t.Stop()
		for {
			select {
			case <-c.stop:
				return
			case <-t.C:
			}
			onFrame(&huddle.CameraFrame{W: c.f.W, H: c.f.H, Y: append([]byte(nil), c.f.Y...), UV: append([]byte(nil), c.f.UV...),
				YStride: c.f.YStride, UVStride: c.f.UVStride, At: time.Now()})
		}
	}()
	return nil
}

func (c *personCamera) Close() { c.once.Do(func() { close(c.stop) }) }

// "Blur your video background…" (the device's prefs.video.effect) reaches
// the session's options, the live session and the open join preview.
func TestHuddleBlurPref(t *testing.T) {
	e := newShotEnv(t, false)
	h := &fxHost{avHost: avHost{hcHost: *e.h, agc: true}, fx: huddle.VideoEffectBlur}
	e.c.h = h
	if o := e.c.options(e.h.app); o.VideoEffect != huddle.VideoEffectBlur {
		t.Fatalf("options VideoEffect %q, want blur", o.VideoEffect)
	}
	s := avJoin(t, e)
	e.c.SetVideoEffect(huddle.VideoEffectNone)
	if s.VideoEffect() != huddle.VideoEffectNone {
		t.Fatalf("live session effect %q after turning blur off", s.VideoEffect())
	}
	e.c.SetVideoEffect(huddle.VideoEffectBlur)
	if s.VideoEffect() != huddle.VideoEffectBlur {
		t.Fatalf("live session effect %q after turning blur on", s.VideoEffect())
	}
	// the join preview window follows on its next frame
	w := &previewWindow{c: e.c, fx: huddle.VideoEffectNone}
	w.p = huddle.StartPreview(huddle.Options{Device: huddle.NewToneDevice(440)}, false)
	defer w.p.Close()
	e.c.mu.Lock()
	e.c.pwin = w
	e.c.mu.Unlock()
	e.c.SetVideoEffect(huddle.VideoEffectBlur)
	w.view()
	if w.p.VideoEffect() != huddle.VideoEffectBlur {
		t.Fatalf("join preview effect %q", w.p.VideoEffect())
	}
	e.c.mu.Lock()
	e.c.pwin = nil
	e.c.mu.Unlock()
}

// TestHuddleShotsBlur: the join preview's self view of a synthetic person,
// without and with the blur (blur-self-preview-{off,on}-{light,dark}).
func TestHuddleShotsBlur(t *testing.T) {
	for _, dark := range []bool{false, true} {
		mode := "light"
		if dark {
			mode = "dark"
		}
		e := newShotEnv(t, dark)
		c := e.c
		th := theme.New(dark)
		var cl previewClicks
		frames := map[string]*image.RGBA{}
		shots := map[string]*image.RGBA{}
		for _, fx := range []huddle.VideoEffect{huddle.VideoEffectNone, huddle.VideoEffectBlur} {
			cam := newPersonCamera(640, 360)
			p := huddle.StartPreview(huddle.Options{Device: huddle.NewToneDevice(440), VideoEffect: fx,
				OpenCamera: func(func(string, ...any)) (huddle.Camera, error) { return cam, nil }}, true)
			var f huddle.VideoFrame
			for end := time.Now().Add(10 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
				if f = p.Frame(); f.Img != nil && f.Seq >= 3 {
					break
				}
			}
			p.Close()
			if f.Img == nil {
				t.Fatalf("%s: no preview frame with effect %q", mode, fx)
			}
			name := "off"
			if fx == huddle.VideoEffectBlur {
				name = "on"
			}
			frames[name] = f.Img
			v := previewView{Self: huddle.FakeSelfUser, MicOn: true, CamOn: true, Frame: f.Img, FrameSeq: f.Seq,
				Labels: [3]string{"MacBook Pro Microphone", "MacBook Pro Speakers", "FaceTime HD Camera"}}
			v.Title, v.Avatars, v.JoinLabel = c.previewTitle(hcChannel)
			var vid videoOps // its own: the image cache is keyed by frame seq, which both previews share
			shots[name] = render(t, "blur-self-preview-"+name+"-"+mode, previewW, previewH, func(gtx C) {
				layoutPreview(gtx, th, c, v, &cl, &vid)
			})
		}
		// the window shows it: thousands of pixels of the video differ
		diff := 0
		for i := 0; i+3 < len(shots["on"].Pix) && i+3 < len(shots["off"].Pix); i += 4 {
			d := 0
			for k := 0; k < 3; k++ {
				v := int(shots["on"].Pix[i+k]) - int(shots["off"].Pix[i+k])
				d += max(v, -v)
			}
			if d > 24 {
				diff++
			}
		}
		if diff < 5000 {
			t.Errorf("%s: the blurred and plain previews differ in only %d px", mode, diff)
		}
		// the frames themselves: the face is the camera's, the shelf isn't
		off, on := frames["off"], frames["on"]
		w, h := off.Rect.Dx(), off.Rect.Dy()
		block := func(x, y int) (d int) {
			for dy := 0; dy < 8; dy++ {
				for dx := 0; dx < 8; dx++ {
					i := (y+dy)*off.Stride + 4*(x+dx)
					for k := 0; k < 3; k++ {
						v := int(off.Pix[i+k]) - int(on.Pix[i+k])
						d += max(v, -v)
					}
				}
			}
			return
		}
		face, shelf := block(w/2-4, h*44/100), block(w*12/100, h*55/100) // mirrored: the shelf is on the left
		if face > 40 || shelf < 300 {
			t.Errorf("%s: face changed by %d, shelf by %d", mode, face, shelf)
		}
	}
}
