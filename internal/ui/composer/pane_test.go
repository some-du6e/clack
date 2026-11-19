package composer

import (
	"image"
	"strings"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/slackc"
)

// A composer drawn with Layout inside a list's pane (an edit composer in
// its row, a Block Kit field in a message) keys its plugin roots in that
// pane, and its vanish guard remounts them there. Only LayoutIn recorded a
// pane, so the guard looked in pane "" and found none of its roots: a
// patched input that came back without the box was never mounted again
// while the composer was open. The guard's empty-tree case
// (TestComposerBoxSurvivesEmptyPluginTree), drawn inside a pane.
// Synthetic data.
func TestComposerVanishRemountsInItsPane(t *testing.T) {
	e := newVanishEnv(t, []string{"MessagePaneInput"})
	const pane = "C0LISTPANE"
	frame := func(dt time.Duration) layout.Dimensions {
		e.now = e.now.Add(dt)
		e.ops.Reset()
		gtx := layout.Context{Ops: e.ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Constraints{Min: image.Pt(900, 0), Max: image.Pt(900, 500)}, Now: e.now, Source: e.r.Source()}
		slackc.Frame(gtx)
		defer slackc.EndPane(slackc.Pane(pane)) // the list's, as its row draws an edit composer
		d := e.c.Layout(gtx)
		e.r.Frame(e.ops)
		return d
	}
	e.boxDrawn("before the plugin's tree (its native draws)", frame(0))
	first := e.render("MessagePaneInput")
	if got := slackc.KeyedPanes("MessagePaneInput", e.c.key()); len(got) != 1 || got[0] != pane {
		t.Fatalf("the composer's root is keyed in panes %q; want the list's %q", got, pane)
	}
	e.deliver(first.root, []byte("[]")) // the plugin's tree comes back empty
	e.boxDrawn("the plugin's tree empty", frame(16*time.Millisecond))
	if !e.b.wasUnmounted(first.root) {
		t.Errorf("the guard's remount didn't reach the root keyed in the list's pane (lines %q)", e.lines)
	}
	if len(e.lines) == 0 || !strings.Contains(e.lines[0], "remounted 1 plugin root") {
		t.Errorf("log lines %q; want the one root remounted", e.lines)
	}
	e.boxDrawn("the frame after", frame(16*time.Millisecond))
	if fresh := e.render("MessagePaneInput"); fresh.root == first.root {
		t.Error("no fresh root mounted in the pane")
	}
}
