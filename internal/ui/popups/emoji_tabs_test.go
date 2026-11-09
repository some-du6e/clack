package popups

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme/pixeltest"
)

// TestEmojiTabRow draws the picker's "Emoji | GIFs" row in light and dark,
// on either tab, at 1x and 2x. The active tab's icon is Slack's coloured
// one inked #1d1c1d in both modes: Karim's dark-mode picker showed a blank
// yellow disc because the face was drawn in the (light) label colour. The
// row is saved to ICON_SHOTS_DIR when set.
func TestEmojiTabRow(t *testing.T) {
	face := rgb(0x1d1c1d)
	for _, dark := range []bool{false, true} {
		for _, gif := range []bool{false, true} {
			for _, scale := range []int{1, 2} {
				mode, tab := "light", "emoji"
				if dark {
					mode = "dark"
				}
				if gif {
					tab = "gifs"
				}
				name := fmt.Sprintf("emoji-tabs-%s-%s-%dx", mode, tab, scale)
				t.Run(name, func(t *testing.T) {
					th := theme.Bare(dark)
					th.Shaper = theme.NewShaper()
					p := &EmojiPicker{}
					p.gifMode = gif
					img := epTabRowShot(t, th, p, scale)
					k := scale * scale
					// the tabs' halves; icons sit in each half's left part
					w := img.Bounds().Dx()
					emoji, gifs := image.Rect(0, 0, w/2, img.Bounds().Dy()), image.Rect(w/2, 0, w, img.Bounds().Dy())
					act, inact := emoji, gifs
					disc := rgb(0xf2c744)
					if gif {
						act, inact, disc = gifs, emoji, rgb(0x9ed6fa)
					}
					if n := pixeltest.Count(img, act, disc, 24); n < 60*k {
						t.Errorf("active tab: %d coloured icon pixels, want ≥ %d", n, 60*k)
					}
					if n := pixeltest.Count(img, act, face, 40); n < 20*k {
						t.Errorf("active tab: %d #1d1c1d face pixels, want ≥ %d (a blank disc)", n, 20*k)
					}
					if n := pixeltest.Count(img, inact, disc, 24); n > 2*k {
						t.Errorf("inactive tab: %d coloured icon pixels, want none", n)
					}
					if n := pixeltest.Count(img, act, colorsOf(th).Aubergine, 24); n < (w/2-4)*2*k/scale {
						t.Errorf("active tab: %d underline pixels", n)
					}
					epSaveShot(t, os.Getenv("ICON_SHOTS_DIR"), name, img)
				})
			}
		}
	}
}

// epTabRowShot renders the picker's tab row (layoutTop, epW×epTopH dp)
// over the card background through the GPU renderer.
func epTabRowShot(t *testing.T, th *theme.Theme, p *EmojiPicker, scale int) *image.RGBA {
	t.Helper()
	cl := colorsOf(th)
	w, h := epW*scale, epTopH*scale
	var ops op.Ops
	paint.FillShape(&ops, cl.Bg, clip.Rect{Max: image.Pt(w, h)}.Op())
	gtx := layout.Context{Ops: &ops, Metric: unit.Metric{PxPerDp: float32(scale), PxPerSp: float32(scale)}, Constraints: layout.Exact(image.Pt(w, h))}
	p.layoutTop(gtx, th, cl, w)
	return pixeltest.Render(t, w, h, &ops)
}

func epSaveShot(t *testing.T, dir, name string, img image.Image) {
	t.Helper()
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(dir, name+".png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}
