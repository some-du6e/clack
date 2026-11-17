package composer

import (
	"image"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// A composer drawn with Layout inside a list's pane (an edit composer in
// its row, a Block Kit field) keys its plugin roots in that pane, and the
// vanish guard remounts them there. Only LayoutIn recorded a pane, so an
// edit composer's guard looked in pane "" and found none of its roots:
// a patched MessageInput that came back without the box was never mounted
// again for the rest of the edit. Synthetic data.
func TestComposerKeysItsRootsInTheListsPane(t *testing.T) {
	app, _ := fakeslackApp(t)
	th := theme.New(false)
	h := &ovHost{slickHost: &slickHost{app: app, th: th}}
	const ch = "C0PANEED1"
	ec := NewEdit(h, core.Message{Channel: ch, TS: "1700000000.000300", Text: "edit me"}, nil)
	t.Cleanup(ec.Close)
	gtx := layout.Context{Ops: new(op.Ops), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Now: time.Now(),
		Constraints: layout.Constraints{Max: image.Pt(700, 160)}}
	func() {
		defer slackc.EndPane(slackc.Pane(ch)) // the channel's list, as its row draws the edit
		ec.Layout(gtx)
	}()
	if ec.pane != ch {
		t.Errorf("the edit composer drawn in the list's pane %q records pane %q; its guard remounts there", ch, ec.pane)
	}
	// and LayoutIn's pane, as before
	c := New(h, ch, "")
	t.Cleanup(c.Close)
	c.LayoutIn(gtx, "split:"+ch)
	if c.pane != "split:"+ch {
		t.Errorf("LayoutIn's composer records pane %q", c.pane)
	}
	if slackc.CurrentPane() != "" {
		t.Errorf("a pane outlived its composer: %q", slackc.CurrentPane())
	}
}
