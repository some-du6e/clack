package richview

// A message emoji whose image fails to load: Slack's rich-text Emoji keeps
// the broken <img alt=":name:"> (no shouldFallbackOnLoadError), and
// Chromium draws its broken image icon at the top left of the emoji's 22px
// box (brokenimage.go). Clack drew nothing: a blank gap. Synthetic URL,
// drawn headlessly (Mesa on CI).

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/imgcache"
	rt "github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

func TestFailedEmojiDrawsBrokenImageLikeSlack(t *testing.T) {
	const u = "https://emoji.slack-edge.com/T1/gone/0123456789abcdef.png"
	c := imgcache.New(func(_ context.Context, url string) ([]byte, error) {
		return nil, errors.New("synthetic: 404")
	}, "", nil)
	d := doc(para(txt("before "), rt.Span{Kind: rt.Emoji, ID: "gone", Text: ":gone:", URL: u}, txt(" after")))
	hs := &harness{st: newTestStyle(), v: New(d, Options{}), h: &recHandler{}, w: 400}
	hs.st.Images = c
	hs.frame()
	fi := -1
	for i := range hs.v.d.frags {
		if f := &hs.v.d.frags[i]; f.kind == fImage && f.url == u {
			fi = i
		}
	}
	if fi < 0 {
		t.Fatal("no image frag for the emoji")
	}
	f := hs.v.d.frags[fi]
	for dl := time.Now().Add(10 * time.Second); !c.Failed(u, f.size); time.Sleep(10 * time.Millisecond) {
		hs.frame()
		if time.Now().After(dl) {
			t.Fatal("the emoji's load never failed: the case tests nothing")
		}
	}
	for i := 0; i < 3; i++ {
		hs.frame()
	}
	img := pixeltest.Render(t, hs.w, 100, &hs.ops)
	x0, y0 := int(f.x+0.5), f.y
	bg := img.RGBAAt(hs.w-5, y0+f.size/2) // past the text: the background
	ink := 0
	for y := y0; y < y0+f.size; y++ {
		for x := x0; x < x0+f.size; x++ {
			p := img.RGBAAt(x, y)
			if d := max(absI(int(p.R)-int(bg.R)), absI(int(p.G)-int(bg.G)), absI(int(p.B)-int(bg.B))); d > 24 {
				ink++
			}
		}
	}
	// Chromium's icon (a page with a torn corner and a picture) inks a
	// good part of its 16x16 square
	if ink < 30 {
		t.Errorf("the failed emoji's %dpx box has %d inked pixels: an empty gap, where Slack shows the broken image icon", f.size, ink)
	}
	// the icon sits at the box's top left: its bottom rows are empty
	for y := y0 + 17; y < y0+f.size; y++ {
		for x := x0; x < x0+f.size; x++ {
			p := img.RGBAAt(x, y)
			if d := max(absI(int(p.R)-int(bg.R)), absI(int(p.G)-int(bg.G)), absI(int(p.B)-int(bg.B))); d > 24 {
				t.Errorf("ink at (%d,%d), under the 16px icon at the box's top", x-x0, y-y0)
				return
			}
		}
	}
}

func absI(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
