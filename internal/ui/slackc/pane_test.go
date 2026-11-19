package slackc

import (
	"image"
	"image/color"
	"testing"
	"time"

	"gioui.org/layout"
)

// Two panes drawing the same patched component under the same "$key" with
// props of their own (a reply also sent to the channel: the channel's list
// and the thread pane both draw its MessageWrapper, viewContext and $pane
// differing) each get a root, as Slack's two React subtrees each have a
// component. Sharing one, the panes sent JS their props in turn and it
// rendered again every frame. The same pane drawn by two windows gets a
// root per window too.

// paneCards draws Card key k in pane (outside any when ""), with props
// that differ by pane.
func paneCards(gtx layout.Context, pane, k string) {
	defer EndPane(Pane(pane))
	Render(gtx, "Card", Props{"$key": k, "card": k, "viewContext": "in " + pane}, func(gtx layout.Context, p Props) layout.Dimensions {
		return layout.Dimensions{Size: image.Pt(100, 40)}
	})
}

func TestPanesHaveRootsOfTheirOwn(t *testing.T) {
	b, at := sweepEnv(t)
	frame := func() {
		LockFrame()
		defer UnlockFrame()
		gtx := cardCtx(at)
		Frame(gtx)
		paneCards(gtx, "C1", "1700000000.000200")
		paneCards(gtx, "C1/1700000000.000100", "1700000000.000200")
		at = at.Add(20 * time.Millisecond)
	}
	for i := 0; i < 3; i++ {
		frame()
	}
	roots := map[uint64]bool{}
	for _, r := range b.renders {
		roots[r.root] = true
	}
	if len(roots) != 2 || len(b.renders) != 2 {
		t.Fatalf("two panes drawing Card under one $key: %d renders into roots %v; want one render each into a root per pane", len(b.renders), roots)
	}
	for r := range roots {
		if err := Deliver(r, []byte(cardTree)); err != nil {
			t.Fatal(err)
		}
	}
	n := len(b.renders)
	for i := 0; i < 30; i++ {
		frame()
	}
	if k := len(b.renders) - n; k != 0 {
		t.Errorf("nothing changed and the two panes sent JS %d renders over 30 frames; want 0", k)
	}
	if len(b.unmounted) != 0 {
		t.Errorf("roots unmounted while both panes draw them: %v", b.unmounted)
	}

	// the pane in the main window and in another: a root each
	var w SubWindow
	n = len(b.renders)
	for i := 0; i < 3; i++ {
		LockFrame()
		gtx := cardCtx(at)
		restore := w.Frame(gtx, "p-huddle_window", color.NRGBA{A: 0xff})
		paneCards(gtx, "C1", "1700000000.000200")
		restore()
		UnlockFrame()
		frame()
	}
	fresh := map[uint64]bool{}
	for _, r := range b.renders[n:] {
		if roots[r.root] {
			t.Errorf("the other window drew the pane into the main window's root %d", r.root)
		}
		fresh[r.root] = true
	}
	if len(fresh) != 1 {
		t.Errorf("the pane drawn in a second window rendered into roots %v; want one of its own", fresh)
	}
	w.Close()
	closing.Wait()
	for r := range fresh {
		if !b.gone(r) {
			t.Errorf("the second window closed and its root %d is still mounted (unmounted %v)", r, b.unmounted)
		}
	}
	for r := range roots {
		if b.gone(r) {
			t.Errorf("the second window closing unmounted the main window's root %d", r)
		}
	}

	// outside a pane, the drawing window doesn't key (HuddleMiniPanel is
	// one root the dock and the presenter panel draw:
	// TestRootSharedByTwoWindowsOutlivesOneClosing)
	if curPane != "" {
		t.Errorf("a pane outlived its EndPane: %q", curPane)
	}
}

// RemountKeyedIn drops one pane's roots for a key and leaves another
// pane's with the same key: a composer whose box vanished remounts its own
// plugin roots, not the other composer's of the same thread (the thread
// pane's and a Threads view card's), which may be holding a send.
func TestRemountKeyedInPane(t *testing.T) {
	b, at := sweepEnv(t)
	const key = "C1/1700000000.000100"
	frame := func() {
		LockFrame()
		defer UnlockFrame()
		gtx := cardCtx(at)
		Frame(gtx)
		paneCards(gtx, "C1/1700000000.000100", key)
		paneCards(gtx, "threads:C1/1700000000.000100", key)
		at = at.Add(20 * time.Millisecond)
	}
	frame()
	byPane := map[string]uint64{}
	for k, in := range insts {
		if k.key == key {
			byPane[k.pane] = in.root
		}
	}
	if len(byPane) != 2 {
		t.Fatalf("roots by pane %v; want one in each", byPane)
	}
	if n := RemountKeyedIn(key, "threads:C1/1700000000.000100"); n != 1 {
		t.Errorf("RemountKeyedIn dropped %d instances; want the one in its pane", n)
	}
	if !b.gone(byPane["threads:C1/1700000000.000100"]) || b.gone(byPane["C1/1700000000.000100"]) {
		t.Errorf("unmounted %v: want the card's root only (card %d, pane %d)", b.unmounted, byPane["threads:C1/1700000000.000100"], byPane["C1/1700000000.000100"])
	}
}
