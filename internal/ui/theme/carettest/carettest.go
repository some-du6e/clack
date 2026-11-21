// Package carettest checks where a text input draws its caret against
// its placeholder and its text, in pixels, the way Chromium draws them.
// Only tests import it.
//
// Blink's rules (Chromium main, 2026-11-20):
//
//   - The bar caret is the font's height on the text's baseline: its
//     ascent and its descent each rounded (FontMetrics,
//     platform/fonts/font_metrics.cc, SkScalarRoundToScalar), from
//     ComputeLocalCaretRectAtTextOffset (core/layout/inline/caret_rect.cc:
//     the text fragment's block size) and, in an empty field,
//     LocalCaretRectForEmptyElement (core/layout/layout_box_model_object.cc:
//     the font's Height(), centred in the line box).
//   - It is LocalFrameView::BarCaretWidth wide: max(1, the device scale)
//     device px, which paints pixel-snapped (1 px at 1x, 2 at 1.5x and
//     2x). Its left edge is the caret's x less half that width, rounded
//     and kept inside the line (ClampAndRound), so in an empty field and
//     at the start of the text it starts at the text's left.
//   - The placeholder takes the editor's first baseline
//     (BlockLayoutAlgorithm::HandleTextControlPlaceholder,
//     core/layout/block_layout_algorithm.cc), and a single-line input
//     centres its editor (html.css:
//     input::-webkit-textfield-decoration-container, align-items: center).
package carettest

import (
	"image"
	"image/color"
	"math"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"golang.org/x/image/math/fixed"

	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// Field is one input to check.
type Field struct {
	Name string
	// Ed is the input's editor; Shot lays the input out (focused: the
	// caller focuses Ed in its frames) for as many frames as it needs and
	// renders the last one. Check sets Ed's text, caret and ReadOnly (a
	// read-only editor draws no caret) between shots.
	Ed   *widget.Editor
	Shot func() *image.RGBA
	// Box is the input's inside in the shot, in px: where its text,
	// placeholder and caret are, and no border or focus ring.
	Box image.Rectangle
	// Size is the editor's font size; Scale the shot's px per dp and sp.
	Size  unit.Sp
	Scale float32
	// Placeholder is the placeholder as drawn (a Regular Lato label at
	// Size, as Clack's inputs draw theirs).
	Placeholder string
	Th          *theme.Theme
	// DeviceScale is the screen's px per dp when Scale includes a zoom
	// (0: Scale): Blink's caret is max(1, device scale) wide at any zoom.
	DeviceScale float32
	// LineBox, when set, is the box the editor's line is centred in (px):
	// the caret's top must be (height - ascent - descent)/2 below its top,
	// exactly, as Blink centres a single-line input's line.
	LineBox image.Rectangle
}

// Geometry is what Check measured, in px.
type Geometry struct {
	Caret, CaretAt0, CaretAtEnd image.Rectangle // empty, before and after the text
	Placeholder, Text           image.Rectangle // their ink (half coverage)
	Baseline                    int             // the placeholder's, from its ink
	Ascent, Descent             int             // the font's, rounded
}

// NoBlink makes carets solid for the test (Blink's blink would hide one
// in half the shots); it restores the interval at cleanup.
func NoBlink(t testing.TB) {
	old := widget.CaretBlinkInterval()
	widget.SetCaretBlinkInterval(0)
	t.Cleanup(func() { widget.SetCaretBlinkInterval(old) })
}

// Check shoots f empty and with its placeholder typed in, with and
// without the caret, and checks the caret against Blink's rules (the
// package doc) and the placeholder against the text. It reports each
// miss with t.Errorf and returns what it measured.
func Check(t *testing.T, f Field) Geometry {
	t.Helper()
	NoBlink(t)
	var g Geometry
	shot := func(txt string, caret int, show bool) *image.RGBA {
		if f.Ed.Text() != txt {
			f.Ed.SetText(txt)
		}
		f.Ed.SetCaret(caret, caret)
		f.Ed.ReadOnly = !show
		defer func() { f.Ed.ReadOnly = false }()
		return f.Shot()
	}
	n := len([]rune(f.Placeholder))
	emptyOff := shot("", 0, false)
	emptyOn := shot("", 0, true)
	textOff := shot(f.Placeholder, 0, false)
	textOn0 := shot(f.Placeholder, 0, true)
	textOnEnd := shot(f.Placeholder, n, true)
	f.Ed.SetText("")

	g.Caret = Diff(emptyOn, emptyOff, f.Box, 24)
	g.CaretAt0 = Diff(textOn0, textOff, f.Box, 24)
	g.CaretAtEnd = Diff(textOnEnd, textOff, f.Box, 24)
	if g.Caret.Empty() {
		t.Fatalf("%s: no caret drawn in the empty field (box %v)", f.Name, f.Box)
	}
	r := rulerOf(t, f)
	ink := f.Box
	ink.Min.X = max(ink.Min.X, g.Caret.Min.X-2)
	ink.Max.X = min(ink.Max.X, g.Caret.Min.X+r.width+4)
	g.Placeholder = InkBox(emptyOff, ink, Mode(emptyOff, ink))
	g.Text = InkBox(textOff, ink, Mode(textOff, ink))
	g.Ascent, g.Descent = r.asc, r.desc
	g.Baseline = g.Placeholder.Min.Y - r.ink.Min.Y
	t.Logf("%s @%gx: caret %v (at 0 %v, at end %v), placeholder ink %v, text ink %v, baseline %d, font %d+%d, ruler ink %v width %d",
		f.Name, f.Scale, g.Caret, g.CaretAt0, g.CaretAtEnd, g.Placeholder, g.Text, g.Baseline, r.asc, r.desc, r.ink, r.width)
	if g.Placeholder.Empty() || g.Text.Empty() {
		t.Fatalf("%s: no ink (placeholder %v, text %v in %v)", f.Name, g.Placeholder, g.Text, ink)
	}

	dev := f.DeviceScale
	if dev == 0 {
		dev = f.Scale
	}
	wantW := int(math.Floor(math.Max(1, float64(dev)) + .5))
	if lb := f.LineBox; !lb.Empty() {
		if top := lb.Min.Y + (lb.Dy()-r.asc-r.desc)/2; g.Caret.Min.Y != top {
			t.Errorf("%s @%gx: the caret's top is y %d, want %d: the line centred in its box %v (font %d+%d)", f.Name, f.Scale, g.Caret.Min.Y, top, lb, r.asc, r.desc)
		}
	}
	for _, c := range []struct {
		what string
		r    image.Rectangle
	}{{"empty", g.Caret}, {"before the text", g.CaretAt0}, {"after the text", g.CaretAtEnd}} {
		if c.r.Dx() != wantW {
			t.Errorf("%s @%gx: the caret %s is %d px wide, want Blink's %d (BarCaretWidth: max(1, device scale), snapped)", f.Name, f.Scale, c.what, c.r.Dx(), wantW)
		}
		if c.r.Dy() != r.asc+r.desc {
			t.Errorf("%s @%gx: the caret %s is %d px tall (%d..%d), want the font's %d (ascent %d + descent %d, each rounded)", f.Name, f.Scale, c.what, c.r.Dy(), c.r.Min.Y, c.r.Max.Y, r.asc+r.desc, r.asc, r.desc)
		}
		if top := g.Baseline - r.asc; iabs(c.r.Min.Y-top) > 1 {
			t.Errorf("%s @%gx: the caret %s spans y %d..%d, want %d..%d: the placeholder's line (baseline %d, ascent %d, descent %d)", f.Name, f.Scale, c.what, c.r.Min.Y, c.r.Max.Y, top, top+r.asc+r.desc, g.Baseline, r.asc, r.desc)
		}
	}
	if g.CaretAt0 != g.Caret {
		t.Errorf("%s @%gx: the caret before the text is %v, the empty field's %v: Blink draws both at the text's start on its line", f.Name, f.Scale, g.CaretAt0, g.Caret)
	}
	// the text starts at the caret: its ink at the pen's left bearing
	if x0 := g.Placeholder.Min.X - r.ink.Min.X; iabs(g.Caret.Min.X-x0) > 1 {
		t.Errorf("%s @%gx: the empty caret's left is x %d, want %d: where the placeholder's text starts (ink at %d, bearing %d)", f.Name, f.Scale, g.Caret.Min.X, x0, g.Placeholder.Min.X, r.ink.Min.X)
	}
	if end := g.Text.Min.X - r.ink.Min.X + r.adv; iabs(g.CaretAtEnd.Min.X-(end-wantW/2)) > 1 {
		t.Errorf("%s @%gx: the caret after the text starts at x %d, want %d (the text's end %d less half the caret)", f.Name, f.Scale, g.CaretAtEnd.Min.X, end-wantW/2, end)
	}
	if iabs(g.Placeholder.Min.Y-g.Text.Min.Y) > 1 || iabs(g.Placeholder.Max.Y-g.Text.Max.Y) > 1 || iabs(g.Placeholder.Min.X-g.Text.Min.X) > 1 {
		t.Errorf("%s @%gx: the placeholder's ink %v, the same text typed %v: Blink puts the placeholder on the text's baseline", f.Name, f.Scale, g.Placeholder, g.Text)
	}
	return g
}

// ruler is the placeholder string drawn by itself: its ink relative to
// its pen origin on the baseline, and the font's rounded metrics.
type ruler struct {
	ink       image.Rectangle
	width     int // px, as drawn
	adv       int // px, the pen's advance, rounded
	asc, desc int
}

func rulerOf(t *testing.T, f Field) ruler {
	t.Helper()
	const pad = 16
	var r ruler
	gtx := layout.Context{Ops: new(op.Ops), Metric: unit.Metric{PxPerDp: f.Scale, PxPerSp: f.Scale}, Now: time.Now()}
	px := gtx.Sp(f.Size)
	sh := f.Th.Shaper
	sh.LayoutString(text.Parameters{Font: theme.EditorFont(theme.Font(theme.Regular)), PxPerEm: fixed.I(px), MaxWidth: 1 << 20, MaxLines: 1}, f.Placeholder)
	var adv fixed.Int26_6
	for gl, ok := sh.NextGlyph(); ok; gl, ok = sh.NextGlyph() {
		if r.asc == 0 && r.desc == 0 {
			r.asc, r.desc = gl.Ascent.Round(), gl.Descent.Round()
		}
		adv = gl.X + gl.Advance
	}
	r.adv = adv.Round()
	w := r.adv + 3*pad
	h := 3*px + 2*pad
	gtx.Constraints = layout.Constraints{Max: image.Pt(w, h)}
	paint.FillShape(gtx.Ops, color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}, clip.Rect{Max: image.Pt(w, h)}.Op())
	st := op.Offset(image.Pt(pad, pad)).Push(gtx.Ops)
	gtx.Constraints.Max = image.Pt(w-2*pad, h-2*pad)
	d := f.Th.Label(gtx, f.Size, theme.Regular, color.NRGBA{A: 0xff}, f.Placeholder)
	st.Pop()
	img := pixeltest.Render(t, w, h, gtx.Ops)
	base := pad + d.Size.Y - d.Baseline
	ink := InkBox(img, img.Bounds(), color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff})
	r.ink = ink.Sub(image.Pt(pad, base))
	r.width = d.Size.X
	return r
}

// Diff is the bounding box of the pixels in r where a and b differ by
// more than tol in a channel.
func Diff(a, b *image.RGBA, r image.Rectangle, tol int) image.Rectangle {
	var box image.Rectangle
	r = r.Intersect(a.Bounds()).Intersect(b.Bounds())
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			p, q := a.RGBAAt(x, y), b.RGBAAt(x, y)
			if iabs(int(p.R)-int(q.R)) > tol || iabs(int(p.G)-int(q.G)) > tol || iabs(int(p.B)-int(q.B)) > tol {
				box = box.Union(image.Rect(x, y, x+1, y+1))
			}
		}
	}
	return box
}

// InkBox is the bounding box of the pixels in r at least half as far
// from bg as the farthest one: what is drawn there, its anti-aliased
// edges cut at half coverage whatever its colour.
func InkBox(img *image.RGBA, r image.Rectangle, bg color.NRGBA) image.Rectangle {
	r = r.Intersect(img.Bounds())
	dist := func(x, y int) int {
		c := img.RGBAAt(x, y)
		return iabs(int(c.R)-int(bg.R)) + iabs(int(c.G)-int(bg.G)) + iabs(int(c.B)-int(bg.B))
	}
	far := 0
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			far = max(far, dist(x, y))
		}
	}
	var box image.Rectangle
	if far < 24 {
		return box
	}
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if 2*dist(x, y) >= far {
				box = box.Union(image.Rect(x, y, x+1, y+1))
			}
		}
	}
	return box
}

// Mode is the commonest colour in r: the background behind text.
func Mode(img *image.RGBA, r image.Rectangle) color.NRGBA {
	n := map[color.RGBA]int{}
	var best color.RGBA
	r = r.Intersect(img.Bounds())
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			c := img.RGBAAt(x, y)
			n[c]++
			if n[c] > n[best] {
				best = c
			}
		}
	}
	return color.NRGBA{R: best.R, G: best.G, B: best.B, A: 0xff}
}

func iabs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
