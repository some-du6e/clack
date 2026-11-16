package views

import (
	"testing"
)

// Activity.Layout ran a full core snapshot every frame (plusClicks built
// the "+" menu's items, the sidebar's sections among them): α.89's freeze
// report caught one 2 s frame there on a 1665-conversation workspace.
// Frames with the menu shut build none; with it open, RecentSnapshot's
// one is reused frame to frame.
func TestActivityLayoutSnapshotsNotPerFrame(t *testing.T) {
	v, _, e, _ := vaFakeActivity(t, false)
	e.until(t, "the saved views", func() bool { return v.viewsLoaded })
	c := v.h.Core()
	e.frame()
	b0 := c.SnapshotBuilds()
	for i := 0; i < 30; i++ {
		e.frame()
	}
	if n := c.SnapshotBuilds() - b0; n > 2 { // a background event (names, user) may drop RecentSnapshot's
		t.Errorf("30 frames with the menu shut built %d snapshots", n)
	}

	v.plus.Click()
	e.frame()
	if v.menu != actMenuPlus {
		t.Fatalf("plus: menu %d", v.menu)
	}
	b1 := c.SnapshotBuilds()
	for i := 0; i < 30; i++ {
		e.frame()
	}
	if n := c.SnapshotBuilds() - b1; n > 2 {
		t.Errorf("30 frames with the menu open built %d snapshots", n)
	}
	// the menu still lists the sidebar's suggestions and runs a click
	row := -1
	for i, it := range v.plusItems() {
		if it.label == "Reactions" {
			row = i
		}
	}
	if row < 0 {
		t.Fatal("no Reactions suggestion in the menu")
	}
	v.plusC[row].Click()
	e.frame()
	if v.menu != actMenuNone {
		t.Errorf("clicked row left the menu %d", v.menu)
	}
	e.until(t, "the Reactions view", func() bool { return actTabsNow(v) == "All,DMs,Mentions,Threads,Reactions" })
}
