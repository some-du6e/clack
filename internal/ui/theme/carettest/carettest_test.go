package carettest

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
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// TestCaretAlignEditorBox is a single-line Editor in a box taller than
// its line (layout.Exact, as Lists' search and cell editors give theirs)
// with theme.EditorPlaceholder beside it: Blink centres the line in the
// box and puts the placeholder on its baseline.
func TestCaretAlignEditorBox(t *testing.T) {
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
				dp := func(v float32) int { return int(v*sc + .5) }
				for _, c := range []struct {
					name string
					size unit.Sp
					boxH float32
					ph   string
				}{
					{"Lists search (13 in 20)", 13, 20, "Search"},
					{"Lists cell (15 in 22)", 15, 22, "Add a comment…"},
					{"tall box (15 in 36)", 15, 36, "Search agents and apps"},
				} {
					ed := &widget.Editor{SingleLine: true}
					w, h := dp(360), dp(c.boxH)+40
					bw, bh := dp(320), dp(c.boxH)
					var r input.Router
					var ops op.Ops
					frame := func() {
						ops.Reset()
						gtx := layout.Context{Ops: &ops, Source: r.Source(), Metric: unit.Metric{PxPerDp: sc, PxPerSp: sc},
							Constraints: layout.Exact(image.Pt(w, h)), Now: time.Now()}
						if !gtx.Focused(ed) {
							gtx.Execute(key.FocusCmd{Tag: ed})
						}
						theme.Fill(gtx, image.Rectangle{Max: image.Pt(w, h)}, th.C.Bg)
						st := op.Offset(image.Pt(20, 20)).Push(gtx.Ops)
						gtx.Constraints = layout.Exact(image.Pt(bw, bh))
						if ed.Len() == 0 {
							editorPlaceholder(th, gtx, c.size, th.C.FgSubtle, c.ph)
						}
						th.Editor(gtx, ed, c.size, th.C.Fg, th.C.Selection)
						st.Pop()
						r.Frame(&ops)
					}
					shot := func() *image.RGBA {
						frame()
						frame()
						frame()
						return pixeltest.Render(t, w, h, &ops)
					}
					Check(t, Field{Name: c.name, Ed: ed, Shot: shot, Box: image.Rect(18, 18, 20+bw, 22+bh),
						Size: c.size, Scale: sc, Placeholder: c.ph, Th: th})
				}
			})
		}
	}
}
