package ui

// Round 4 of inspect-blur-fast (review of a614d3b3): a dragged region is
// copied only once its review has been on screen a while, and the banner
// says Enter copies only then; a toast is a layer of the tool's record.

import (
	"image"
	"strings"
	"testing"
	"time"

	"gioui.org/io/key"

	"github.com/some-du6e/clack/internal/ui/inspect"
)

// TestInspectRegionArming: a dragged region's review offers Enter only
// once it has been on screen a while, and an Enter before that copies
// nothing (review of a614d3b3: Enter while the review's first frame was
// still on its way to the screen copied the region unseen).
func TestInspectRegionArming(t *testing.T) {
	_, copied := fakeShots(t)
	e := newInspectEnv(t)
	e.startSession(t)
	from := image.Pt(e.size.X/4, e.size.Y/4)
	to := from.Add(image.Pt(300, 200))
	e.drag(from, from.Add(image.Pt(3, 2)), to)
	e.release(to)
	e.frame()
	e.a.inspectAfterFrame(&e.ops, e.size)
	if !e.a.insp.Reviewing() {
		t.Fatal("no review after the region")
	}
	if drawn := frameStrings(e.perfEnv); drawnContains(drawn, "Enter copy") {
		t.Errorf("the region's review offers Enter before it has been on screen a while")
	}
	e.key(key.NameReturn, 0)
	e.frame()
	e.a.insp.Wait()
	e.frame()
	if *copied != nil || !e.a.insp.Reviewing() {
		t.Fatalf("Enter as the region's review came up: copied %v, reviewing %v; want nothing copied, the review up", *copied != nil, e.a.insp.Reviewing())
	}
	e.settle(450 * time.Millisecond)
	if drawn := frameStrings(e.perfEnv); !drawnContains(drawn, "Enter copy") {
		t.Errorf("the region's review, on screen a while, doesn't offer Enter")
	}
	e.key(key.NameReturn, 0)
	e.frame()
	e.a.insp.Wait()
	e.frame()
	if *copied == nil || e.a.insp.Active() {
		t.Errorf("Enter in the region's review on screen a while: copied %v, session %v", *copied != nil, e.a.insp.Active())
	}
}

// TestInspectPicksToast: a toast in a session is a layer of the record:
// a pick on it outlines the toast and reports it as the region (review of
// a614d3b3: toasts recorded nothing, so a pick on one reported the
// message or the column under it).
func TestInspectPicksToast(t *testing.T) {
	fakeShots(t)
	e := newInspectEnv(t)
	e.a.Toast("Link copied to clipboard")
	e.frame()
	e.startSession(t)
	at := image.Pt(e.size.X/2, e.size.Y-28-22) // the toast: centred, 44 dp tall, 28 dp up
	label, r, ok := e.hoverAt(at)
	if !ok || !strings.HasPrefix(label, "toast") || !at.In(r) || r.Dy() > 60 {
		t.Errorf("over the toast at %v: outline %q %v (%v), want the toast's", at, label, r, ok)
	}
	rows := e.pickReport(t, at)
	if !strings.HasPrefix(rows["region"], "toast") {
		t.Errorf("report region %q, want the toast", rows["region"])
	}
}

// TestInspectSubWindowsGate: huddleui's windows run tools of their own
// while the main window's is on (InspectOn follows it off and on again),
// and a freeze in any window holds the animated images until the last
// one lets go, the main window's freeze through the same count
// (InspectHold; Karim, 2026-11-20: the tool never opened in the huddle
// window).
func TestInspectSubWindowsGate(t *testing.T) {
	fakeShots(t)
	e := newInspectEnv(t)
	if !e.a.InspectOn() {
		t.Fatal("the tool on: InspectOn reports it off")
	}
	e.a.setInspectTool(false)
	if e.a.InspectOn() {
		t.Error("the tool turned off: InspectOn reports it on")
	}
	e.a.setInspectTool(true)
	if !e.a.InspectOn() {
		t.Fatal("the tool turned on again: InspectOn reports it off")
	}
	held := func() int {
		e.a.liveHolds.Lock()
		defer e.a.liveHolds.Unlock()
		return e.a.liveHolds.n
	}
	e.a.InspectHold(true) // the huddle window frozen
	e.startSession(t)
	e.key(inspect.FreezeKey, 0)
	e.frame()
	if !e.a.insp.Frozen() || held() != 2 {
		t.Fatalf("the main window frozen too: frozen %v, %d holds; want two", e.a.insp.Frozen(), held())
	}
	e.key(inspect.FreezeKey, 0)
	e.frame()
	if e.a.insp.Frozen() || held() != 1 {
		t.Errorf("the main window unfrozen: frozen %v, %d holds; want the huddle window's one", e.a.insp.Frozen(), held())
	}
	e.a.InspectHold(false)
	e.a.InspectHold(false) // one too many: nothing to let go of
	if held() != 0 {
		t.Errorf("both let go: %d holds, want none", held())
	}
}
