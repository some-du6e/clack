package ui

import (
	"net/url"
	"sync"
	"testing"
	"time"
)

func TestFocusExpiration(t *testing.T) {
	now := time.Unix(1900000000, 0)
	for _, c := range []struct {
		opt  string
		want int64
	}{{"half_hour", 1900001800}, {"one_hour", 1900003600}, {"two_hours", 1900007200}, {"no_expiration", focusForever}, {"", focusForever}} {
		if got := focusExpiration(c.opt, now); got != c.want {
			t.Errorf("%q: %d want %d", c.opt, got, c.want)
		}
	}
	loc := time.FixedZone("t", 0)
	n := time.Date(2026, 11, 10, 14, 0, 0, 0, loc)
	if got := focusSubLabel(focusForever, n); got != "Notifications paused" {
		t.Errorf("forever: %q", got)
	}
	if got := focusSubLabel(time.Date(2026, 11, 10, 14, 30, 0, 0, loc).Unix(), n); got != "Notifications paused until 2:30 PM" {
		t.Errorf("today: %q", got)
	}
	if got := focusSubLabel(time.Date(2026, 11, 11, 0, 30, 0, 0, loc).Unix(), n); got != "Notifications paused until tomorrow at 12:30 AM" {
		t.Errorf("tomorrow: %q", got)
	}
}

// Turning focus mode on writes focus_mode_expiration_ts (Don’t turn off by
// default); it then quiets the sidebar and swaps in the silent sidebar
// theme, with sidebarInverted from focus_mode_force_dark_mode_enabled;
// turning it off writes 0.
func TestFocusModeToggle(t *testing.T) {
	a, ca, fs := fakeApp(t, map[string]any{"focus_mode_force_dark_mode_enabled": false})
	ca.SetExperimentGroup("silent_sidebar", "on")
	var mu sync.Mutex
	var sets []url.Values
	fs.SetHook(func(method string, f url.Values) (any, bool) {
		if method == "users.prefs.set" {
			mu.Lock()
			sets = append(sets, f)
			mu.Unlock()
		}
		return nil, false
	})
	if !a.focusModeOn() || a.focusModeActive() {
		t.Fatalf("on %v active %v", a.focusModeOn(), a.focusModeActive())
	}
	a.toggleFocusMode()
	if !a.focusModeActive() || !a.focusQuiet() {
		t.Fatal("not active right after the toggle (optimistic)")
	}
	waitTheme(t, a, "the write", func() bool { mu.Lock(); defer mu.Unlock(); return len(sets) == 1 && !a.fm().pending })
	mu.Lock()
	if sets[0].Get("name") != "focus_mode_expiration_ts" || sets[0].Get("value") != "9999999999" {
		t.Errorf("write %v", sets[0])
	}
	mu.Unlock()
	if ca.PrefInt("focus_mode_expiration_ts", 0) != focusForever || !a.focusModeActive() {
		t.Error("the pref didn't land")
	}
	ft, ok := a.focusTheme()
	if !ok || ft.Primary.Palette != "gray" || ft.Highlight2.Palette != "mojito" || ft.SidebarInverted || ft.Brightness != 6 {
		t.Errorf("silent sidebar theme %+v %v", ft, ok)
	}
	// a later duration pick writes now + 1 hour
	a.fm().option = "one_hour"
	before := time.Now().Unix()
	a.setFocusExp(focusExpiration("one_hour", time.Now()))
	waitTheme(t, a, "the duration write", func() bool { mu.Lock(); defer mu.Unlock(); return len(sets) == 2 && !a.fm().pending })
	if exp := a.focusExp(); exp < before+3600 || exp > time.Now().Unix()+3600 {
		t.Errorf("one hour: %d", exp)
	}
	a.toggleFocusMode()
	waitTheme(t, a, "the off write", func() bool { mu.Lock(); defer mu.Unlock(); return len(sets) == 3 && !a.fm().pending })
	mu.Lock()
	if sets[2].Get("value") != "0" {
		t.Errorf("off write %v", sets[2])
	}
	mu.Unlock()
	if a.focusModeActive() || a.focusQuiet() {
		t.Error("still active")
	}
	if _, ok := a.focusTheme(); ok {
		t.Error("theme still swapped")
	}
	// without the experiment there is no focus mode at all
	ca.SetExperimentGroup("silent_sidebar", "off")
	a.toggleFocusMode()
	if a.focusModeActive() || a.fm().pending {
		t.Error("toggled without silent_sidebar")
	}
}
