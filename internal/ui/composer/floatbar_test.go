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

// calculateToolbarPosition (cj0/), top placement.
func TestFloatBarPosition(t *testing.T) {
	const edW, tw, th = 600, 200, 36
	// wider than the bar: centred on the selection
	if p := floatBarPos(image.Rect(100, 50, 400, 70), 0, edW, tw, th, 900); p != image.Pt(150, 9) {
		t.Errorf("wide %v", p)
	}
	// the editor's whole width: from its left
	if p := floatBarPos(image.Rect(0, 50, edW, 70), 0, edW, tw, th, 900); p.X != 0 {
		t.Errorf("full width %v", p)
	}
	// narrow, in the middle half: centred
	if p := floatBarPos(image.Rect(250, 50, 300, 70), 0, edW, tw, th, 900); p.X != 175 {
		t.Errorf("middle %v", p)
	}
	// narrow, right of the middle half with room: ends at its end
	if p := floatBarPos(image.Rect(500, 50, 550, 70), 0, edW, tw, th, 900); p.X != 350 {
		t.Errorf("right %v", p)
	}
	// narrow, at the start: from its start
	if p := floatBarPos(image.Rect(20, 50, 60, 70), 0, edW, tw, th, 900); p.X != 20 {
		t.Errorf("left %v", p)
	}
	// never closer than 10px to the right edge
	if p := floatBarPos(image.Rect(700, 50, 1000, 70), 0, 1000, tw, th, 900); p.X != 900-10-tw {
		t.Errorf("clamped %v", p)
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
