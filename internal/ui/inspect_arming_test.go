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
