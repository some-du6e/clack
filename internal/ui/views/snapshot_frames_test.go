package views

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/uiguard"
)

// Unreads and Activity read core's sidebar snapshot in every frame they
// draw with a section filter on (Unreads' list and jump button,
// Activity's In pill) or a menu open (Activity's "+"). A change that drops
// it is built again in the background, and they draw the one they had
// meanwhile: no frame builds one on the UI goroutine. α.100 built it there
// whenever it was a second old and after every change that dropped it
// (each user_change on Hack Club's socket): 8-15 ms for Karim's 1,666
// conversations, a dropped frame each.
func TestUnreadsActivityBuildNoSnapshotOnTheUIGoroutine(t *testing.T) {
	uiguard.MarkUI() // the frames below run here
	defer uiguard.Unmark()
	var onUI, all atomic.Int64
	core.SetSnapshotBuildHook(func() {
		all.Add(1)
		if uiguard.OnUI() {
			onUI.Add(1)
		}
	})
	defer core.SetSnapshotBuildHook(nil)
	churn := func(what string, c *core.App, frame func()) {
		t.Helper()
		frame()
		time.Sleep(100 * time.Millisecond)
		frame()
		onUI.Store(0)
		a0 := all.Load()
		for i := 0; i < 30; i++ {
			c.InvalidateSnapshot() // what a sidebar or DM partner's change does
			frame()
			time.Sleep(40 * time.Millisecond)
		}
		time.Sleep(1100 * time.Millisecond) // past α.100's one-second expiry
		frame()
		frame()
		if n := onUI.Load(); n > 0 {
			t.Errorf("%s: %d sidebar snapshots built on the UI goroutine in 33 frames", what, n)
		}
		if all.Load() == a0 {
			t.Errorf("%s: no snapshot built at all: the frames didn't read one", what)
		}
	}

	u, _, ue, _ := unrEnv(t, false, nil)
	c := u.h.Core()
	var sec string
	for _, s := range c.RecentSnapshot().Sections {
		if s.Type == "channels" {
			sec = s.ID
		}
	}
	if sec == "" {
		t.Fatal("no channels section")
	}
	if err := c.SetPref(context.Background(), "all_unreads_section_filter", sec); err != nil {
		t.Fatal(err)
	}
	ue.until(t, "the section filter", func() bool { return u.sectionFilter() == sec })
	churn("Unreads with a section filter", c, ue.frame)

	v, _, e, _ := vaFakeActivity(t, false)
	e.until(t, "the saved views", func() bool { return v.viewsLoaded })
	v.sections = []string{"L0FAKECH"} // the In pill: one section
	e.frame()
	v.plus.Click() // and the "+" menu, which lists the sidebar's sections
	e.frame()
	if v.menu != actMenuPlus {
		t.Fatalf("plus: menu %d", v.menu)
	}
	churn("Activity with an In section and its + menu open", v.h.Core(), e.frame)
}
