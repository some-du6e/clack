package carettest

import (
	"fmt"
	"image"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// boxEnv is a single-line Editor in a w×h px box at (20,20), laid out
// with layout.Exact (as Lists' search and cell editors give theirs), its
// placeholder beside it.
type boxEnv struct {
	th      *theme.Theme
	ed      *widget.Editor
	r       input.Router
	ops     op.Ops
	sc, dev float32 // the Metric's px per dp (zoomed) and the device's
	size    unit.Sp
	ph      string
	bw, bh  int
	frameW  int
	frameH  int
	box     image.Rectangle // the editor's box in the frame
}

func newBoxEnv(th *theme.Theme, sc, dev float32, size unit.Sp, bw, bh int, ph string) *boxEnv {
	e := &boxEnv{th: th, ed: &widget.Editor{SingleLine: true}, sc: sc, dev: dev, size: size, ph: ph, bw: bw, bh: bh,
		frameW: bw + 40, frameH: bh + 40}
	e.box = image.Rect(20, 20, 20+bw, 20+bh)
	return e
}

func (e *boxEnv) frame() {
	e.ops.Reset()
	gtx := layout.Context{Ops: &e.ops, Source: e.r.Source(), Metric: unit.Metric{PxPerDp: e.sc, PxPerSp: e.sc},
		Constraints: layout.Exact(image.Pt(e.frameW, e.frameH)), Now: time.Now()}
	if e.dev != 0 {
		setDeviceScale(gtx.Source, e.dev)
	}
	if !gtx.Focused(e.ed) {
		gtx.Execute(key.FocusCmd{Tag: e.ed})
	}
	theme.Fill(gtx, image.Rectangle{Max: image.Pt(e.frameW, e.frameH)}, e.th.C.Bg)
	st := op.Offset(e.box.Min).Push(gtx.Ops)
	gtx.Constraints = layout.Exact(e.box.Size())
	if e.ed.Len() == 0 {
		editorPlaceholder(e.th, gtx, e.size, e.th.C.FgSubtle, e.ph)
	}
	e.th.Editor(gtx, e.ed, e.size, e.th.C.Fg, e.th.C.Selection)
	st.Pop()
	e.r.Frame(&e.ops)
}

func (e *boxEnv) shot(t *testing.T) *image.RGBA {
	e.frame()
	e.frame()
	e.frame()
	return pixeltest.Render(t, e.frameW, e.frameH, &e.ops)
}

func themes(t *testing.T, f func(t *testing.T, th *theme.Theme)) {
	for _, dark := range []bool{false, true} {
		mode := "light"
		if dark {
			mode = "dark"
		}
		t.Run(mode, func(t *testing.T) {
			th := theme.New(dark)
			sh := theme.NewShaper()
			th.SetShapers(sh, sh)
			f(t, th)
		})
	}
}

// TestCaretAlignEditorBox is a single-line Editor in a box taller than
// its line with theme.EditorPlaceholder beside it: Blink centres the line
// in the box ((height - ascent - descent)/2, the leading floored) and puts
// the placeholder on its baseline.
func TestCaretAlignEditorBox(t *testing.T) {
	themes(t, func(t *testing.T, th *theme.Theme) {
		for _, sc := range []float32{1, 1.5, 2} {
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
				e := newBoxEnv(th, sc, 0, c.size, dp(320), dp(c.boxH), c.ph)
				Check(t, Field{Name: fmt.Sprintf("%s @%gx", c.name, sc), Ed: e.ed, Shot: func() *image.RGBA { return e.shot(t) },
					Box: e.box.Inset(-2), Size: c.size, Scale: sc, Placeholder: c.ph, Th: th, LineBox: e.box})
			}
		}
	})
}

// TestCaretAlignZoomedWidth: View > Zoom scales Clack's Metric, and
// Blink's caret stays max(1, device scale) wide at any page zoom
// (LocalFrameView::BarCaretWidth: WindowToViewportScalar(1), the screen's
// scale): 2 px at 2x zoomed to 125% or 150%, 1 px at 1x zoomed to 150%.
func TestCaretAlignZoomedWidth(t *testing.T) {
	themes(t, func(t *testing.T, th *theme.Theme) {
		for _, z := range []struct{ dev, zoom float32 }{{2, 1.25}, {2, 1.5}, {1, 1.5}} {
			sc := z.dev * z.zoom
			dp := func(v float32) int { return int(v*sc + .5) }
			e := newBoxEnv(th, sc, z.dev, 15, dp(320), dp(36), "Search")
			Check(t, Field{Name: fmt.Sprintf("device %gx zoomed %g%%", z.dev, z.zoom*100), Ed: e.ed, Shot: func() *image.RGBA { return e.shot(t) },
				Box: e.box.Inset(-2), Size: 15, Scale: sc, DeviceScale: z.dev, Placeholder: "Search", Th: th, LineBox: e.box})
		}
	})
}

// TestCaretScrolledOutOfView: a single-line field scrolled away from its
// caret by a touchpad's sideways swipe (a precise pointer.Scroll) draws no
// caret. Blink keeps the caret inside the line in the text's own
// coordinates (ClampAndRound) and the scrolled view clips it; it never
// pulls it onto the field's edge, over text where typing won't go.
func TestCaretScrolledOutOfView(t *testing.T) {
	NoBlink(t)
	themes(t, func(t *testing.T, th *theme.Theme) {
		for _, sc := range []float32{1, 2} {
			dp := func(v float32) int { return int(v*sc + .5) }
			txt := strings.Repeat("the quick brown fox jumps over the lazy dog ", 4)
			n := len([]rune(txt))
			for _, c := range []struct {
				name   string
				caret  int
				scroll float32
			}{
				{"caret at the start, scrolled right", 0, 2000},
				{"caret at the end, scrolled back left", n, -100000},
			} {
				e := newBoxEnv(th, sc, 0, 15, dp(160), dp(36), "")
				e.ed.SetText(txt)
				e.ed.SetCaret(c.caret, c.caret)
				e.ed.ReadOnly = true
				before := e.shot(t)
				// the pointer over the field, then the swipe
				mid := f32.Pt(float32(e.box.Min.X+e.box.Dx()/2), float32(e.box.Min.Y+e.box.Dy()/2))
				e.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: mid})
				e.frame()
				e.r.Queue(pointer.Event{Kind: pointer.Scroll, Source: pointer.Mouse, Position: mid, Scroll: f32.Pt(c.scroll, 0)})
				off := e.shot(t)
				if d := Diff(before, off, e.box, 24); d.Empty() {
					t.Fatalf("%s @%gx: the swipe moved no text", c.name, sc)
				}
				e.ed.ReadOnly = false
				on := e.shot(t)
				if d := Diff(on, off, e.box.Inset(-2), 24); !d.Empty() {
					t.Errorf("%s @%gx: a caret drawn at %v, scrolled out of the view (Blink clips it; it was pulled onto the field's edge)", c.name, sc, d)
				}
			}
		}
	})
}
