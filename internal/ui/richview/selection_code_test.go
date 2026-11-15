package richview

import (
	"image"
	"image/color"
	"testing"

	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"

	rt "github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// TestSelectionOverCode: selected text in a code block and in an inline
// code chip shows the selection highlight, under the glyphs. Chromium
// paints ::selection with the text, after the backgrounds below it, so
// in Slack the highlight is visible over the code block and the chip;
// here their fills are opaque (Palette.flat), and painted over the
// selection they hid it (Karim, α.85). Light and dark.
func TestSelectionOverCode(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name, pal := "light", LightPalette()
		if dark {
			name, pal = "dark", DarkPalette()
		}
		t.Run(name, func(t *testing.T) {
			st := NewStyle(themeShaper(), noImages{}, pal)
			p := &st.Palette
			d := doc(
				para(txt("before the code")),
				rt.Block{Kind: rt.Preformatted, Spans: []rt.Span{txt("selectedcodeline")}},
				para(txt("a "), rt.Span{Kind: rt.Text, Text: "chipword", Style: rt.Code}, txt(" b")),
			)
			const w = 420
			v := layoutAt(st, d, Options{}, w)
			pre := findFrag(t, &v.d, "selectedcodeline")
			chip := findFrag(t, &v.d, "chipword")
			h := v.d.size.Y + 4

			render := func(selected bool) *image.RGBA {
				var ops op.Ops
				paint.FillShape(&ops, p.Panel, clip.Rect{Max: image.Pt(w, h)}.Op())
				v.selA, v.selB = 0, 0
				if selected {
					v.selA, v.selB = 0, 1<<20 // all of it
				}
				v.Layout(testGtx(&ops, w), st, &recHandler{})
				return pixeltest.Render(t, w, h, &ops)
			}
			inner := func(f *frag) image.Rectangle {
				r := fragRect(f)
				return image.Rect(r.Min.X+1, r.Min.Y+1, r.Max.X-1, r.Max.Y-1)
			}
			plain, sel := render(false), render(true)
			for _, c := range []struct {
				what string
				f    *frag
				bg   color.NRGBA
			}{{"code block", pre, p.PreBg}, {"inline code", chip, p.CodeBg}} {
				r := inner(c.f)
				area := r.Dx() * r.Dy()
				bg := c.bg
				// unselected, the frag's box is mostly its background
				if n := pixeltest.Count(plain, r, bg, 3); n < area/3 {
					t.Fatalf("%s: only %d of %d pixels are its background %v unselected (wrong box?)", c.what, n, area, bg)
				}
				// selected, its background shows nowhere under the text: the
				// highlight covers it
				if n := pixeltest.Count(sel, r, bg, 3); n > area/10 {
					t.Errorf("%s: %d of %d pixels still the bare background %v with the text selected: the box hides the selection", c.what, n, area, bg)
				}
				// and the glyphs are drawn over the highlight: as many of
				// their pixels keep the text colour as unselected
				before, after := pixeltest.Count(plain, r, c.f.col, 60), pixeltest.Count(sel, r, c.f.col, 60)
				if before < 2 {
					t.Fatalf("%s: %d text-coloured pixels unselected (no glyphs?)", c.what, before)
				}
				if after < before/2 {
					t.Errorf("%s: %d text-coloured pixels selected, %d unselected: the highlight covers the glyphs", c.what, after, before)
				}
			}
		})
	}
}
