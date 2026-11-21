package composer

import (
	"image"
	"testing"
	"time"

	"gioui.org/io/input"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// TestComposerToolbarFoldRule is TextyButtonOverflow's choice as Slack's
// build 1f2d42be66b2 makes it (overflow.go): the toolbar folds only when
// it's narrower than its buttons in a row (inlineButtonsWidth), Taut's
// composer.addButton buttons among them. (Taut's own patch counts only
// the slots that render, and with one or two plugin buttons leaves a row
// that doesn't fit running under the send group: Clack folds it.)
func TestComposerToolbarFoldRule(t *testing.T) {
	gtx := layout.Context{Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	var c chrome
	c.pxPerDp = 1
	c.cur.Mode = modeChannel
	c.f.minOverflow = crOverflowDefault
	c.f.hide = crNoRecorder // Clack has no clip recorder

	// Slack's buttons in a row: Aa, emoji, mention, the divider, [/]
	if got := c.inlineGuess(gtx, slackc.Styled{}); got != 137 {
		t.Errorf("the buttons' own width %d, want 137 (2+28+2, 2+28+2, 2+28, 6+1+6, 2+28)", got)
	}
	g2 := layout.Context{Metric: unit.Metric{PxPerDp: 2, PxPerSp: 2}}
	if got := c.inlineGuess(g2, slackc.Styled{}); got != 274 {
		t.Errorf("at 2x: %d, want 274", got)
	}
	c.f.hide |= 1 << crEmoji
	if got := c.inlineGuess(gtx, slackc.Styled{}); got != 105 {
		t.Errorf("without the emoji button: %d, want 105", got)
	}
	c.f.hide &^= 1 << crEmoji

	c.inlineW = 137
	for _, tc := range []struct {
		eq   int
		fold bool
	}{{0, false}, {900, false}, {579, false}, {137, false}, {136, true}, {60, true}} {
		c.f.eq = tc.eq
		if got := c.foldFooter(); got != tc.fold {
			t.Errorf("toolbar %d wide, buttons 137: fold %v, want %v", tc.eq, got, tc.fold)
		}
	}

	// a thread composer with OwlMode's button: 16 children, and it folds
	// as soon as its buttons don't fit (OwlMode's draws nothing outside an
	// owl thread), not only once the moves bring Taut's count to five
	c.cur.Mode, c.cur.ThreadTS = modeThread, tbThreadTS
	c.f.plugBtns = 1
	for _, tc := range []struct {
		eq   int
		n    int
		fold bool
	}{{500, 16, false}, {137, 16, false}, {136, 16, true}, {122, 16, true}, {104, 16, true}} {
		c.f.eq = tc.eq
		if n, real := c.overflowKids(); n != tc.n || !real {
			t.Errorf("OwlMode, toolbar %d: %d children (real %v), want %d", tc.eq, n, real, tc.n)
		}
		if got := c.foldFooter(); got != tc.fold {
			t.Errorf("OwlMode, toolbar %d: fold %v, want %v", tc.eq, got, tc.fold)
		}
	}
	c.f.plugBtns = 2 // and SilentTyping's: its button is in the row too
	c.inlineW = 137 + 2 + 28
	for _, tc := range []struct {
		eq   int
		fold bool
	}{{170, false}, {167, false}, {166, true}, {159, true}} {
		c.f.eq = tc.eq
		if got := c.foldFooter(); got != tc.fold {
			t.Errorf("OwlMode and SilentTyping, toolbar %d: fold %v, want %v", tc.eq, got, tc.fold)
		}
	}
	if c.tautKind() != "thread" {
		t.Errorf("a thread composer is Taut's %q", c.tautKind())
	}
	c.cur.Mode = modeField
	if c.tautKind() != "other" || c.foldFooter() {
		t.Errorf("a field is Taut's %q and folded %v; want other, never", c.tautKind(), c.foldFooter())
	}
	c.cur.Mode = modeEdit
	if c.tautKind() != "edit" || c.foldFooter() {
		t.Errorf("an edit composer is Taut's %q and folded %v; want edit, never", c.tautKind(), c.foldFooter())
	}
}

// TextyButtons measures its buttons from the toolbar's own layout while
// nothing is folded (measureInlineButtons) and keeps the measure while
// folded: a channel composer's are 137px at 1x, whatever its width.
func TestComposerToolbarMeasuresItsButtons(t *testing.T) {
	app, _ := syntheticApp(t, func(*store.Store) {})
	stubTyping(t)
	h := &ovHost{slickHost: &slickHost{app: app, th: theme.New(false)}}
	c := freshComposer(t, h)
	ops := new(op.Ops)
	var r input.Router
	w := 900
	frame := func() {
		ops.Reset()
		gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Now: time.Now(), Source: r.Source(),
			Constraints: layout.Constraints{Min: image.Pt(w, 0), Max: image.Pt(w, 400)}}
		slackc.Frame(gtx)
		c.Layout(gtx)
		r.Frame(ops)
	}
	for range 3 {
		frame()
	}
	if c.ui.inlineW != 137 || c.ui.f.fold {
		t.Fatalf("a 900px composer: buttons %d wide, fold %v; want 137, unfolded", c.ui.inlineW, c.ui.f.fold)
	}
	w = 260 // the toolbar is 114: folded
	for range 3 {
		frame()
	}
	if !c.ui.f.fold || c.ui.inlineW != 137 {
		t.Fatalf("a 260px composer: fold %v, buttons %d; want folded, the measure kept", c.ui.f.fold, c.ui.inlineW)
	}
	w = 283 // toolbar 137: the buttons fit again
	for range 3 {
		frame()
	}
	if c.ui.f.fold {
		t.Errorf("a 283px composer (toolbar %d) still folded", c.ui.f.eq)
	}
}
