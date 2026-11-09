package composer

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

// TestFormatBarShots draws the focused composer's formatting bar
// (B I U S ¶ | link, lists | quote, code, code block, divider) in light
// and dark at 1x and 2x, checks every button inked something, and saves
// formatbar-<mode>-<scale>x.png to ICON_SHOTS_DIR when set (the
// icon-shots workflow uploads them next to the Slack comparison strips).
func TestFormatBarShots(t *testing.T) {
	dir := os.Getenv("ICON_SHOTS_DIR")
	for _, dark := range []bool{false, true} {
		for _, scale := range []int{1, 2} {
			mode := "light"
			if dark {
				mode = "dark"
			}
			t.Run(fmt.Sprintf("%s-%dx", mode, scale), func(t *testing.T) {
				th := theme.Bare(dark)
				th.Shaper = theme.NewShaper()
				var c chrome
				p := c.palette(th)
				st := &chromeState{Focused: true}
				w, h := 430*scale, 40*scale
				var ops op.Ops
				paint.FillShape(&ops, p.bar, clip.Rect{Max: image.Pt(w, h)}.Op())
				gtx := layout.Context{Ops: &ops, Metric: unit.Metric{PxPerDp: float32(scale), PxPerSp: float32(scale)}, Constraints: layout.Exact(image.Pt(w, h))}
				c.layoutBar(gtx, th, p, st, 0, w, 0)
				img := pixeltest.Render(t, w, h, &ops)
				for _, id := range crFmtOrder {
					r := c.rect[id]
					if r.Empty() {
						continue
					}
					if n := pixeltest.Ink(img, r, p.icon, p.bar); n < 4*scale*scale {
						t.Errorf("button %d at %v: %d icon pixels, want a glyph", id, r, n)
					}
				}
				if dir == "" {
					return
				}
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
				f, err := os.Create(filepath.Join(dir, fmt.Sprintf("formatbar-%s-%dx.png", mode, scale)))
				if err != nil {
					t.Fatal(err)
				}
				defer f.Close()
				if err := png.Encode(f, img); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}
