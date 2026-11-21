package popups

// The caret in popups' shared input (field: c-input_text, the emoji
// picker's search, section and theme modals, profile fields) against its
// placeholder and text, by Blink's rules (theme/carettest). Pixels: CI
// runs this with CLACK_PIXEL_TESTS=1 on llvmpipe, WARP and Metal
// (flake-check only=caret-align).

import (
	"fmt"
	"image"
	"testing"
	"time"

	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/carettest"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

func TestCaretAlignPopupsField(t *testing.T) {
	for _, dark := range []bool{false, true} {
		for _, sc := range []float32{1, 1.5, 2} {
			mode := "light"
			if dark {
				mode = "dark"
			}
			t.Run(fmt.Sprintf("%s-%gx", mode, sc), func(t *testing.T) {
				th := theme.New(dark)
				sh := theme.NewShaper()
				th.SetShapers(sh, sh)
				dpx := func(v float32) int { return int(v*sc + .5) }
				for _, c := range []struct {
					name, ph, icon string
					h, size        float32
				}{
					{"search field (36/15, icon)", "Search all emoji", "search", 36, 15},
					{"profile field (44/18)", "Full name", "", 44, 18},
				} {
					ed := &widget.Editor{SingleLine: true}
					w, h := dpx(400), dpx(c.h)+2*20
					var r input.Router
					var ops op.Ops
					frame := func() {
						ops.Reset()
						gtx := layout.Context{Ops: &ops, Source: r.Source(), Metric: unit.Metric{PxPerDp: sc, PxPerSp: sc},
							Constraints: layout.Exact(image.Pt(w, h)), Now: time.Now()}
						if !gtx.Focused(ed) {
							gtx.Execute(key.FocusCmd{Tag: ed})
						}
						cl := colorsOf(th)
						theme.Fill(gtx, image.Rectangle{Max: image.Pt(w, h)}, cl.Bg)
						st := op.Offset(image.Pt(20, 20)).Push(gtx.Ops)
						gtx.Constraints = layout.Constraints{Max: image.Pt(w-40, h-40)}
						field(gtx, th, ed, c.ph, c.icon, c.h, c.size)
						st.Pop()
						r.Frame(&ops)
					}
					shot := func() *image.RGBA {
						frame()
						frame()
						frame()
						return pixeltest.Render(t, w, h, &ops)
					}
					// the text starts 12px in (40 past the icon): look from
					// 4px before it (the border and the icon left out)
					left := 20 + dpx(8)
					if c.icon != "" {
						left = 20 + dpx(36)
					}
					in := dpx(3)
					box := image.Rect(left, 20+in, 20+w-40-in, 20+dpx(c.h)-in)
					carettest.Check(t, carettest.Field{Name: c.name, Ed: ed, Shot: shot, Box: box,
						Size: unit.Sp(c.size), Scale: sc, Placeholder: c.ph, Th: th})
				}
			})
		}
	}
}
