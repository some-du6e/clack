//go:build linux && cgo

package huddle

import (
	"image"
	"image/color"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/thesyncim/goh264"
)

// TestOpenH264Encode: with libopenh264 installed, a 1280x720 test screen
// goes through its screen-content mode, keyframes come when asked for,
// and goh264 (a receiver) decodes the right picture.
func TestOpenH264Encode(t *testing.T) {
	if err := openH264Available(); err != nil {
		t.Skip(err)
	}
	t.Logf("OpenH264 %s", openH264Version())
	enc, err := newOpenH264(encOpts{W: 1280, H: 720, FPS: 15, Bps: 1_200_000, Screen: true})
	if err != nil {
		t.Fatal(err)
	}
	defer enc.Close()
	img := image.NewRGBA(image.Rect(0, 0, 1280, 720))
	dec := goh264.NewDecoder()
	var f CameraFrame
	start := time.Now()
	keys, pics := 0, 0
	var last *goh264.Frame
	for i := 0; i < 12; i++ {
		paintTestScreen(img, i)
		rgbaToNV12(&f, img)
		f.At = start.Add(time.Duration(i) * time.Second / 15)
		au, err := enc.Encode(&f, i == 0 || i == 7)
		if err != nil {
			t.Fatalf("frame %d: %v", i, err)
		}
		if au == nil {
			continue
		}
		if hasIDR(au) {
			keys++
		}
		frames, err := dec.DecodeFrames(au)
		if err != nil {
			t.Fatalf("decode frame %d: %v", i, err)
		}
		for _, fr := range frames {
			pics++
			last = fr
		}
	}
	if keys < 2 || pics < 8 || last == nil {
		t.Fatalf("%d keyframes, %d pictures decoded", keys, pics)
	}
	if last.Width != 1280 || last.Height != 720 {
		t.Fatalf("decoded %dx%d", last.Width, last.Height)
	}
	out := image.NewRGBA(image.Rect(0, 0, 1280, 720))
	yuv420ToRGBA(out, last.Y, last.Cb, last.Cr, last.YStride, last.CStride, last.CropLeft, last.CropTop, last.VideoFullRangeFlag != 0)
	near := func(c color.RGBA, r, g, b int) bool {
		d := func(x uint8, y int) int { return max(int(x)-y, y-int(x)) }
		return d(c.R, r) < 24 && d(c.G, g) < 24 && d(c.B, b) < 24
	}
	if c := out.RGBAAt(4, 4); !near(c, 0x1d, 0x1c, 0x1d) { // the menu bar
		t.Errorf("menu bar decoded as %v", c)
	}
	if c := out.RGBAAt(1270, 300); !near(c, 0xf4, 0xed, 0xe4) { // the desktop
		t.Errorf("desktop decoded as %v", c)
	}
}

// TestX11ScreenCapture runs under Xvfb: the root is painted one colour
// (xsetroot), the picker lists the screen with a thumbnail of it, and
// capture delivers NV12 frames of it.
func TestX11ScreenCapture(t *testing.T) {
	if os.Getenv("DISPLAY") == "" || waylandSession() {
		t.Skip("no X11 display")
	}
	if err := exec.Command("xsetroot", "-solid", "#1264a3").Run(); err != nil {
		t.Skipf("xsetroot: %v", err)
	}
	b := &x11Shares{}
	l, err := b.List(ShareScreen, ShareThumbSize)
	if err != nil || len(l) == 0 {
		t.Fatalf("screens: %v %+v", err, l)
	}
	s := l[0]
	t.Logf("screen %s %q %v", s.ID, s.Name, s.Bounds)
	if s.Name != "Entire Screen" || s.Kind() != ShareScreen || s.Bounds.Dx() < 16 {
		t.Errorf("screen source: %+v", s)
	}
	near := func(c color.RGBA) bool {
		d := func(x uint8, y int) int { return max(int(x)-y, y-int(x)) }
		return d(c.R, 0x12) < 12 && d(c.G, 0x64) < 12 && d(c.B, 0xa3) < 12
	}
	if s.Thumb == nil {
		t.Fatal("no thumbnail")
	}
	if tb := s.Thumb.Bounds(); tb.Dx() > 346 || tb.Dy() > 260 || !near(s.Thumb.RGBAAt(tb.Dx()/2, tb.Dy()/2)) {
		t.Errorf("thumbnail %v, centre %v", tb, s.Thumb.RGBAAt(tb.Dx()/2, tb.Dy()/2))
	}
	if _, err := b.List(ShareWindow, ShareThumbSize); err != nil {
		t.Errorf("windows: %v", err)
	}
	c, err := b.Open(s, false, t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	got := make(chan *image.RGBA, 1)
	if err := c.Start(func(f *CameraFrame) {
		img := image.NewRGBA(image.Rect(0, 0, f.W, f.H))
		nv12ToRGBA(img, f, 1)
		select {
		case got <- img:
		default:
		}
	}, nil, nil); err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	select {
	case img := <-got:
		w, h := capSize(s.Bounds.Dx(), s.Bounds.Dy())
		if b := img.Bounds(); b.Dx() != w || b.Dy() != h {
			t.Errorf("frame %v, want %dx%d", b, w, h)
		}
		if p := img.RGBAAt(img.Bounds().Dx()/3, img.Bounds().Dy()/3); !near(p) {
			t.Errorf("captured %v, want #1264a3", p)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no frame")
	}
}
