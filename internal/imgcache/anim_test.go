package imgcache

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/color/palette"
	"image/gif"
	"sync/atomic"
	"testing"
	"time"
)

// synthGIF: a 32×32 canvas; frame 0 red over the left half only (so the
// first frame doesn't cover the canvas), frame 1 blue over the right half
// (disposal none: red stays), frame 2 green everywhere with a 0 delay.
func synthGIF(t *testing.T) []byte {
	t.Helper()
	fr := func(r image.Rectangle, c color.Color) *image.Paletted {
		p := image.NewPaletted(r, palette.Plan9)
		for y := r.Min.Y; y < r.Max.Y; y++ {
			for x := r.Min.X; x < r.Max.X; x++ {
				p.Set(x, y, c)
			}
		}
		return p
	}
	g := &gif.GIF{
		Image: []*image.Paletted{
			fr(image.Rect(0, 0, 16, 32), color.RGBA{255, 0, 0, 255}),
			fr(image.Rect(16, 0, 32, 32), color.RGBA{0, 0, 255, 255}),
			fr(image.Rect(0, 0, 32, 32), color.RGBA{0, 255, 0, 255}),
		},
		Delay:    []int{5, 20, 0}, // 50 ms, 200 ms, 0 (= 100 ms)
		Disposal: []byte{gif.DisposalNone, gif.DisposalNone, gif.DisposalNone},
		Config:   image.Config{Width: 32, Height: 32, ColorModel: color.Palette(palette.Plan9)},
	}
	var b bytes.Buffer
	if err := gif.EncodeAll(&b, g); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func readyEntry(t *testing.T, c *Cache, url string, px int) *entry {
	t.Helper()
	for dl := time.Now().Add(5 * time.Second); time.Now().Before(dl); time.Sleep(5 * time.Millisecond) {
		if _, ok := c.Get(url, px); ok {
			c.mu.Lock()
			e := c.entries[key{url, px}].Value.(*entry)
			c.mu.Unlock()
			return e
		}
	}
	t.Fatal("image never loaded")
	return nil
}

func TestAnimatedGIF(t *testing.T) {
	data := synthGIF(t)
	var invalidated atomic.Int32
	c := New(func(context.Context, string) ([]byte, error) { return data, nil }, "", func() { invalidated.Add(1) })
	const u = "https://emoji.slack-edge.com/T1/party/abc.gif"

	e := readyEntry(t, c, u, 32)
	if !c.Animated(u, 32) || e.anim == nil {
		t.Fatal("not animated")
	}
	a := e.anim
	if len(a.frames) != 3 || a.ends[0] != 50*time.Millisecond || a.ends[1] != 250*time.Millisecond || a.total != 350*time.Millisecond {
		t.Fatalf("frames %d, ends %v, total %v", len(a.frames), a.ends, a.total)
	}
	if e.size != image.Pt(32, 32) {
		t.Fatalf("size %v: the first frame's bounds, not the canvas", e.size)
	}
	for _, c := range []struct {
		at   time.Duration
		want int
	}{{0, 0}, {49 * time.Millisecond, 0}, {50 * time.Millisecond, 1}, {249 * time.Millisecond, 1}, {300 * time.Millisecond, 2}, {360 * time.Millisecond, 0}} {
		if i, _ := a.frameAt(a.start.Add(c.at)); i != c.want {
			t.Errorf("at %v: frame %d, want %d", c.at, i, c.want)
		}
	}
	if _, next := a.frameAt(a.start.Add(10 * time.Millisecond)); next != a.start.Add(50*time.Millisecond) {
		t.Errorf("next frame due %v after start", next.Sub(a.start))
	}

	// frame 1 is composed over frame 0: red left, blue right
	frames, _, err := decodeGIF(data, 32, false)
	if err != nil {
		t.Fatal(err)
	}
	if l, r := frames[1].RGBAAt(4, 16), frames[1].RGBAAt(28, 16); l.R < 200 || l.B > 50 || r.B < 200 || r.R > 50 {
		t.Errorf("frame 1 left %v right %v, want red | blue", l, r)
	}
	if o := frames[0].RGBAAt(28, 16); o.A != 0 {
		t.Errorf("frame 0 right half %v, want transparent canvas", o)
	}

	// Get schedules a repaint for the next frame
	before := invalidated.Load()
	c.Get(u, 32)
	time.Sleep(450 * time.Millisecond)
	if invalidated.Load() == before {
		t.Error("no repaint for the next frame")
	}

	// large sizes don't animate: the first frame, on its canvas
	big := readyEntry(t, c, u, 400)
	if big.anim != nil || c.Animated(u, 400) {
		t.Error("a 400px GIF animates")
	}
	if big.size != image.Pt(32, 32) {
		t.Errorf("static GIF size %v, want the 32×32 canvas", big.size)
	}
}
