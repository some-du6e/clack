package composer

import (
	"image"
	"testing"
	"time"

	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// calculateToolbarPosition (cj0/), top placement, over
// toolbarPositionDataForRect (7LIu).
func TestFloatBarPosition(t *testing.T) {
	const edW, tw, th = 600, 200, 36
	ed := image.Point{} // the editor at the window's top-left
	// wider than the bar: centred on the selection
	if p := floatBarPos(image.Rect(100, 50, 400, 70), edW, ed, tw, th, 900, 5, 10); p != image.Pt(150, 9) {
		t.Errorf("wide %v", p)
	}
	// the editor's whole width: from its left
	if p := floatBarPos(image.Rect(0, 50, edW, 70), edW, ed, tw, th, 900, 5, 10); p.X != 0 {
		t.Errorf("full width %v", p)
	}
	// narrow, in the middle half: centred
	if p := floatBarPos(image.Rect(250, 50, 300, 70), edW, ed, tw, th, 900, 5, 10); p.X != 175 {
		t.Errorf("middle %v", p)
	}
	// narrow, right of the middle half with room: ends at its end
	if p := floatBarPos(image.Rect(500, 50, 550, 70), edW, ed, tw, th, 900, 5, 10); p.X != 350 {
		t.Errorf("right %v", p)
	}
	// narrow, at the start: from its start
	if p := floatBarPos(image.Rect(20, 50, 60, 70), edW, ed, tw, th, 900, 5, 10); p.X != 20 {
		t.Errorf("left %v", p)
	}
	// never closer than 10px to the window's right edge
	if p := floatBarPos(image.Rect(700, 50, 1000, 70), 1000, ed, tw, th, 900, 5, 10); p.X != 900-10-tw {
		t.Errorf("clamped %v", p)
	}
	// Slack tests the selection's left edge in the window against the
	// editor's width: past the sidebar a selection at the editor's start
	// is centred, not left-aligned
	if p := floatBarPos(image.Rect(20, 50, 60, 70), edW, image.Pt(300, 400), tw, th, 1400, 5, 10); p != image.Pt(240, 409) {
		t.Errorf("after the sidebar %v", p)
	}
	// a narrow composer (a thread) on the window's right: the bar may
	// stick out of it, kept 10px inside the window only
	const bar = 470
	if p := floatBarPos(image.Rect(180, 50, 220, 70), 400, image.Pt(900, 500), bar, th, 1400, 5, 10); p != image.Pt(865, 509) {
		t.Errorf("past the composer %v", p)
	}
	if p := floatBarPos(image.Rect(180, 50, 220, 70), 400, image.Pt(900, 500), bar, th, 1300, 5, 10); p.X != 1300-10-bar {
		t.Errorf("window clamp %v", p)
	}
}

// winHost is a host that knows the window's size (the ui root's frame).
type winHost struct {
	*ovHost
	w int
}

func (h winHost) WindowSize() image.Point { return image.Pt(h.w, 800) }

// The floating bar's right bound is the window (viewportWidth), not the
// composer: a composer 300px from a 1,400px window's left lets the bar
// pass its own right edge.
func TestFloatBarWindowBounds(t *testing.T) {
	app, _ := syntheticApp(t, func(*store.Store) {})
	stubTyping(t)
	old := showFormatting
	t.Cleanup(func() { showFormatting = old })
	formattingWrite.Lock()
	formattingWrite.pending, formattingWrite.on = 1, false
	formattingWrite.Unlock()
	t.Cleanup(func() { formattingWrite.Lock(); formattingWrite.pending = 0; formattingWrite.Unlock() })
	showFormatting = false
	h := winHost{ovHost: &ovHost{slickHost: &slickHost{app: app, th: theme.New(false)}}, w: 1400}
	c := freshComposer(t, h)
	c.Origin = image.Pt(1000, 300) // a 380px composer at the window's right
	var r input.Router
	var ops op.Ops
	now := time.Now()
	frame := func() {
		ops.Reset()
		gtx := layout.Context{Ops: &ops, Source: r.Source(), Now: now, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Constraints{Max: image.Pt(380, 400)}}
		slackc.Frame(gtx)
		c.Layout(gtx)
		r.Frame(&ops)
		now = now.Add(16 * time.Millisecond)
	}
	d := NewDoc()
	d.Insert(0, "one two three four five six seven", Attr{})
	c.ed.SetDoc(d)
	c.Focus()
	for range 4 {
		frame()
	}
	c.ed.SetSelection(24, 27) // "six", near the editor's middle
	for range 4 {
		frame()
	}
	if !c.fbar.shown {
		t.Fatal("no floating bar")
	}
	gtx := layout.Context{Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	tw, th := c.fbar.size(gtx)
	r1, r2 := c.ed.PosRect(24), c.ed.PosRect(27)
	sel := image.Rect(r1.Min.X, r1.Min.Y, r2.Max.X, r2.Max.Y)
	ed := c.ui.InputOffset().Add(image.Pt(0, c.top)).Add(c.Origin)
	want := floatBarPos(sel, c.ed.width, ed, tw, th, 1400, 5, 10)
	if got := c.fbar.origin.Add(c.Origin); got != want {
		t.Errorf("bar at %v in the window, want %v (bounded by the 1,400px window)", got, want)
	}
	if boxed := floatBarPos(sel, c.ed.width, ed, tw, th, c.Origin.X+380, 5, 10); boxed == want {
		t.Fatalf("the scenario doesn't tell the bounds apart: %v either way (sel %v, editor %v, bar %d)", want, sel, ed, tw)
	}
	right := want.X + tw
	if right > 1400-10 {
		t.Errorf("the bar ends at %d, past 10px inside the window (1400)", right)
	}
}

// TextyFloatingFormattingBar: with Aa off and text selected the bar shows;
// with the toolbar on, a collapsed or blank selection, or after Escape it
// doesn't; its buttons run the toolbar's actions.
func TestFloatBarShowsLikeSlack(t *testing.T) {
	app, _ := syntheticApp(t, func(*store.Store) {})
	stubTyping(t)
	old := showFormatting
	t.Cleanup(func() { showFormatting = old })
	h := &ovHost{slickHost: &slickHost{app: app, th: theme.New(false)}}
	c := freshComposer(t, h)
	var r input.Router
	var ops op.Ops
	now := time.Now()
	frame := func() {
		ops.Reset()
		gtx := layout.Context{Ops: &ops, Source: r.Source(), Now: now, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Constraints{Max: image.Pt(900, 400)}}
		slackc.Frame(gtx)
		c.Layout(gtx)
		r.Frame(&ops)
		now = now.Add(16 * time.Millisecond)
	}
	settle := func() {
		for range 4 {
			frame()
		}
	}
	d := NewDoc()
	d.Insert(0, "make this bold", Attr{})
	c.ed.SetDoc(d)
	c.Focus()
	syncOff := func(on bool) {
		formattingWrite.Lock()
		formattingWrite.pending, formattingWrite.on = 1, on // stands in for the pref
		formattingWrite.Unlock()
		showFormatting = on
	}
	t.Cleanup(func() { formattingWrite.Lock(); formattingWrite.pending = 0; formattingWrite.Unlock() })
	syncOff(false)
	settle()
	if !c.ed.Focused(layout.Context{Source: r.Source()}) {
		t.Fatal("the editor didn't take the focus")
	}
	c.ed.SetSelection(5, 9) // "this"
	settle()
	if !c.fbar.shown {
		t.Fatal("no floating bar with Aa off and text selected")
	}
	// a button runs the toolbar's action: Bold
	c.fbar.btns[0].Click()
	settle()
	if c.ed.Doc().Attrs[6].Style&Bold == 0 {
		t.Error("the floating Bold didn't bold the selection")
	}
	// Escape hides it for this selection
	r.Queue(key.Event{Name: key.NameEscape, State: key.Press})
	settle()
	if c.fbar.shown {
		t.Error("Escape left the bar up")
	}
	c.ed.SetSelection(0, 4)
	settle()
	if !c.fbar.shown {
		t.Error("a new selection didn't bring it back")
	}
	c.ed.SetSelection(4, 4)
	settle()
	if c.fbar.shown {
		t.Error("shown with nothing selected")
	}
	c.ed.SetSelection(4, 5) // a blank
	settle()
	if c.fbar.shown {
		t.Error("shown for a selected space")
	}
	syncOff(true)
	c.ed.SetSelection(0, 4)
	settle()
	if c.fbar.shown {
		t.Error("shown with the toolbar on")
	}
}
