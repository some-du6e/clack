package ui

import (
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Karim, 2026-11-13 (macOS): "menu bar and app icon lying": the title
// said "16 new items" over an Activity badge of 25. Slack's title
// (WorkspaceDocumentTitle → DocumentTitle appBadgeCount), the Dock badge
// and the Windows taskbar overlay (getDesktopUnreads' unreadHighlights)
// are one count, getAppBadgeCount: under activity_inbox the Activity
// badge. Each platform's decision is checked: the Dock (macOS), the
// overlay (Windows) and the title (every OS).
func TestTitleDockTaskbarAgreeLikeSlack(t *testing.T) {
	e := badgeEnv(t, map[string]int{"at_user": 20, "thread_v2": 5, "message_reaction": 7})
	a := e.a
	var mu sync.Mutex
	var dock []string
	dockAnyOS = true
	oldB := dockBadge
	dockBadge = func(l string) { mu.Lock(); dock = append(dock, l); mu.Unlock() }
	t.Cleanup(func() { dockAnyOS, dockBadge = false, oldB })
	tb := fakeTaskbar(t)
	lastDock := func() string {
		mu.Lock()
		defer mu.Unlock()
		if len(dock) == 0 {
			return "<none>"
		}
		return dock[len(dock)-1]
	}
	lastOverlay := func() string {
		c := tb.take()
		if len(c) == 0 {
			return ""
		}
		return c[len(c)-1]
	}
	check := func(name string, want int) {
		t.Helper()
		var title string
		e.until(name+": the counts", 5*time.Second, func() bool {
			a.dock.set, a.dock.tbSet = false, false // set again
			a.updateDock()
			title = a.windowTitle()
			return a.desktopUnreads().Count == want
		})
		suffix := " - " + strconv.Itoa(want) + " new items - "
		if want == 1 {
			suffix = " - 1 new item - "
		}
		if want > 0 && !strings.Contains(title, suffix) || want == 0 && strings.Contains(title, "new item") {
			t.Errorf("%s: title %q, want %q in it", name, title, suffix)
		}
		if want > 0 && lastDock() != strconv.Itoa(want) {
			t.Errorf("%s: Dock badge %q, want %d", name, lastDock(), want)
		}
		if o := lastOverlay(); !strings.Contains(o, " "+strconv.Itoa(want)+" ") {
			t.Errorf("%s: taskbar overlay %q, want %d", name, o, want)
		}
	}
	// activity_inbox: the Activity badge (reactions not summed), not Home's
	check("inbox", 25)
	if n := activityBadge(e); n != 25 {
		t.Errorf("rail Activity badge %d, want 25 like the title", n)
	}
	// badge_counts_updated moves all three; DMs are in Activity by default,
	// so activity_v2's dm is in the Activity badge (and so in the app's)
	injectBadges(e, map[string]int{"at_user": 20, "thread_v2": 5, "dm": 3})
	check("inbox, new counts", 28)

	// nothing counted but something unread: the Dock's "•" (mac_ssb_bullet)
	injectBadges(e, map[string]int{})
	e.until("no activity badge", 5*time.Second, func() bool { return a.Core().ActivityBadgeCount() == 0 })
	m := a.model()
	m.HomeUnread = true
	a.dock.set = false
	a.updateDock()
	if l := lastDock(); a.desktopUnreads().Count == 0 && l != "•" {
		t.Errorf("unread, no count: Dock %q, want •", l)
	}

	// without the inbox: the unread highlights (Home's count) everywhere
	a.Core().SetExperimentGroup("activity_inbox", "off")
	e.until("the classic rail", 5*time.Second, func() bool { return !a.model().Rail.ActivityInbox })
	check("no inbox", a.model().HomeCount)
}
