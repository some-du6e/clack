package theme

import (
	"image"
	"math"
	"testing"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"golang.org/x/image/math/fixed"
)

// Slack's --custom-font-tracking-* per type size (boot-shared oWug, the
// CSS rules that pair each variable with its size), in CSS px.
func TestTrackingForSize(t *testing.T) {
	ui := Font(Regular)
	for _, c := range []struct {
		size float32
		pt   float32
	}{
		{12, 0.17}, {13, 0.12}, {15, 0.06}, {18, -0.15}, {22, -0.43}, {28, -0.36},
		// off the scale: inherited from body
		{14, 0.06}, {16, 0.06}, {11, 0.06}, {20, 0.06},
		// a rounded pixel size at a fractional scale still finds its step
		{19.0 / 1.25, 0.06}, {16.0 / 1.25, 0.12}, {28.0 / 1.25, -0.43},
	} {
		if got, want := TrackingFor(ui, c.size), c.pt*4/3; math.Abs(float64(got-want)) > 1e-6 {
			t.Errorf("TrackingFor(UI, %v) = %v px, want %v (%vpt)", c.size, got, want, c.pt)
		}
	}
	// code inherits its paragraph's (body's); emoji are images
	if got := TrackingFor(font.Font{Typeface: MonoTypeface}, 12); math.Abs(float64(got-0.08)) > 1e-6 {
		t.Errorf("mono 12px = %v, want body's 0.08px", got)
	}
	if got := TrackingFor(font.Font{Typeface: EmojiSlotTypeface}, 15); got != 0 {
		t.Errorf("emoji slot = %v, want 0", got)
	}
}

// trackRun lays txt out on one line and returns its glyphs.
func trackRun(sh *text.Shaper, p text.Parameters, txt string) []text.Glyph {
	if p.MaxWidth == 0 {
		p.MaxWidth = 1 << 20
	}
	sh.LayoutString(p, txt)
	var gs []text.Glyph
	for g, ok := sh.NextGlyph(); ok; g, ok = sh.NextGlyph() {
		gs = append(gs, g)
	}
	return gs
}

func runWidth(gs []text.Glyph) fixed.Int26_6 {
	var w fixed.Int26_6
	for _, g := range gs {
		w += g.Advance
	}
	return w
}

// Gio patch 12: Blink's letter-spacing. Every cluster gets the spacing
// after it, a zero-width space doesn't, ligatures are off, and the
// tracking function sees the size in sp through PxPerSp or the shaper's
// scale.
func TestLetterSpacingShaper(t *testing.T) {
	sh := newShaper(FacesFor(false))
	ui := Font(Regular)
	base := text.Parameters{Font: ui, PxPerEm: fixed.I(30)}
	plain := runWidth(trackRun(sh, base, "Hello"))

	const ls = fixed.Int26_6(10)
	p := base
	p.LetterSpacing = ls
	if got, want := runWidth(trackRun(sh, p, "Hello")), plain+5*ls; got != want {
		t.Fatalf("Hello with letter-spacing %v: width %v, want %v (5 clusters)", ls, got, want)
	}

	// ZWSP is one of Blink's TreatAsZeroWidthSpace runes
	zw := runWidth(trackRun(sh, base, "a​b"))
	if got, want := runWidth(trackRun(sh, p, "a​b")), zw+2*ls; got != want {
		t.Fatalf("a ZWSP b: width %v, want %v (no spacing after the ZWSP)", got, want)
	}

	// Lato's liga makes "fi" one glyph; Blink turns it off with spacing
	if n := len(trackRun(sh, base, "fi")); n != 1 {
		t.Logf("fi without spacing shapes as %d glyphs (no ligature in this Lato?)", n)
	}
	if gs := trackRun(sh, p, "fi"); len(gs) != 2 {
		t.Fatalf("fi with letter-spacing: %d glyphs, want 2 (liga off)", len(gs))
	}

	// the tracking function: 15sp at 2x is body's 0.06pt = 0.08px, 0.16
	// device px, 10/64 rounded
	sh.SetTracking(TrackingFor)
	q := text.Parameters{Font: ui, PxPerEm: fixed.I(30), PxPerSp: 2}
	if got, want := runWidth(trackRun(sh, q, "Hello")), plain+5*fixed.Int26_6(10); got != want {
		t.Fatalf("tracked 15sp at 2x: width %v, want %v", got, want)
	}
	// without PxPerSp the shaper's scale says the same
	q.PxPerSp = 0
	sh.SetTrackingScale(2)
	if got, want := runWidth(trackRun(sh, q, "Hello")), plain+5*fixed.Int26_6(10); got != want {
		t.Fatalf("tracked 15sp at the shaper's 2x: width %v, want %v", got, want)
	}
	// 28sp at 1x is headline's -0.36pt = -0.48px, -31/64
	h := text.Parameters{Font: ui, PxPerEm: fixed.I(28), PxPerSp: 1}
	hp := runWidth(trackRun(newShaper(FacesFor(false)), h, "Hello"))
	if got, want := runWidth(trackRun(sh, h, "Hello")), hp-5*31; got != want {
		t.Fatalf("tracked 28sp: width %v, want %v", got, want)
	}
	// an explicit LetterSpacing wins over the function
	h.LetterSpacing = 3
	if got, want := runWidth(trackRun(sh, h, "Hello")), hp+5*3; got != want {
		t.Fatalf("explicit spacing: width %v, want %v", got, want)
	}
}

// The painters and Gio's widgets pass the context's PxPerSp, so labels,
// the theme's text and editors space the same way; Lato draws as before.
func TestTrackedLabelsAndEditors(t *testing.T) {
	gtxAt := func(sc float32) layout.Context {
		return layout.Context{Ops: new(op.Ops), Metric: unit.Metric{PxPerDp: sc, PxPerSp: sc},
			Constraints: layout.Constraints{Max: image.Pt(4000, 400)}}
	}
	themed := func(sh *text.Shaper) *Theme {
		th := Bare(false)
		th.SetShapers(sh, sh)
		return th
	}
	lato := themed(newShaper(FacesFor(false)))
	trk := newShaper(FacesFor(false))
	trk.SetTracking(TrackingFor)
	tracked := themed(trk)
	for _, sc := range []float32{1, 2} {
		gtx := gtxAt(sc)
		a := lato.Label(gtx, 15, Regular, lato.C.Fg, "Hello world")
		b := tracked.Label(gtxAt(sc), 15, Regular, lato.C.Fg, "Hello world")
		// 11 clusters (the trailing one is the last letter) at 0.08px·sc
		ls := float32(math.Round(float64(0.08*sc*64))) / 64
		if d := float32(b.Size.X - a.Size.X); math.Abs(float64(d-11*ls)) > 1.01 {
			t.Errorf("scale %v: tracked label %dpx, Lato's %dpx; want +%.2f", sc, b.Size.X, a.Size.X, 11*ls)
		}
		// headline size draws narrower
		a = lato.Label(gtxAt(sc), 28, Black, lato.C.Fg, "Hello world")
		b = tracked.Label(gtxAt(sc), 28, Black, lato.C.Fg, "Hello world")
		if b.Size.X >= a.Size.X {
			t.Errorf("scale %v: tracked 28sp label %dpx, not narrower than %dpx", sc, b.Size.X, a.Size.X)
		}
	}
	// widget.Label and widget.Editor set PxPerSp from the context
	var lw [2]int
	for i, sh := range []*text.Shaper{newShaper(FacesFor(false)), trk} {
		gtx := gtxAt(2)
		d := widget.Label{MaxLines: 1}.Layout(gtx, sh, Font(Regular), 15, "Hello world", op.CallOp{})
		lw[i] = d.Size.X
	}
	if lw[1] <= lw[0] {
		t.Errorf("widget.Label: tracked %dpx, Lato %dpx", lw[1], lw[0])
	}
	var ew [2]int
	for i, sh := range []*text.Shaper{newShaper(FacesFor(false)), trk} {
		var ed widget.Editor
		ed.SingleLine = true
		ed.SetText("Hello world")
		gtx := gtxAt(2)
		gtx.Constraints.Min = image.Point{}
		ed.Layout(gtx, sh, Font(Regular), 15, op.CallOp{}, op.CallOp{})
		ed.SetCaret(11, 11)
		pos := ed.CaretCoords()
		ew[i] = int(pos.X)
	}
	if ew[1] <= ew[0] {
		t.Errorf("widget.Editor caret at the end: tracked %dpx, Lato %dpx", ew[1], ew[0])
	}
}

// The tracked fonts get the shaper's function, the rest don't.
func TestSetTrackingFollowsUIFont(t *testing.T) {
	plain := newShaper(FacesFor(false))
	p := text.Parameters{Font: Font(Regular), PxPerEm: fixed.I(15), PxPerSp: 1}
	w0 := runWidth(trackRun(plain, p, "Hello"))
	for _, c := range []struct {
		u    *UIFont
		want bool
	}{{nil, false}, {&UIFont{}, false}, {&UIFont{Tracking: true}, true}} {
		sh := newShaper(FacesFor(false))
		setTracking(sh, c.u)
		w := runWidth(trackRun(sh, p, "Hello"))
		if got := w != w0; got != c.want {
			t.Errorf("UIFont %+v: spaced %v, want %v", c.u, got, c.want)
		}
	}
	defer SetDefaultUIFont(nil)
	SetDefaultUIFont(&UIFont{Tracking: true})
	if v := TrackingVars(); v["--custom-font-tracking-body"] != "0.06pt" || len(v) != 7 {
		t.Fatalf("TrackingVars %v", v)
	}
	SetDefaultUIFont(&UIFont{})
	if TrackingVars() != nil {
		t.Fatal("TrackingVars for an untracked font")
	}
}
