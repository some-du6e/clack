package ui

// An activity entry that breaks through focus mode (is_breakthrough):
// handleActivityRTMEvent adds its conversation to the silent sidebar's
// DND overrides while focus mode is on (addDndOverrideChannel), so its
// sidebar row is bold and badged again (shouldSuppressUnreadEmphasis is
// false for it), and only until focus mode next turns on or off
// (setFocusModeActiveStartTs resets them). Synthetic data on
// internal/fakeslack.

import (
	"testing"

	"github.com/some-du6e/clack/internal/core"
)

func TestFocusModeBreakthrough(t *testing.T) {
	a, ca, _ := fakeApp(t, nil)
	ca.SetExperimentGroup("silent_sidebar", "on")
	const loud, quiet = "C0BREAK0001", "C0BREAK0002"
	// off: nothing to break through
	a.data.handle(a, core.Event{Type: "activity_breakthrough", Channel: loud})
	a.toggleFocusMode()
	if !a.focusModeActive() || !a.focusQuietIn(loud) || !a.focusQuietIn(quiet) {
		t.Fatalf("focus mode on: active %v, %s quiet %v, %s quiet %v (an override from before focus mode counted)",
			a.focusModeActive(), loud, a.focusQuietIn(loud), quiet, a.focusQuietIn(quiet))
	}
	a.focusChanged()
	a.data.handle(a, core.Event{Type: "activity_breakthrough", Channel: loud})
	if a.focusQuietIn(loud) {
		t.Errorf("after the breakthrough %s is still quiet", loud)
	}
	if !a.focusQuietIn(quiet) {
		t.Errorf("the breakthrough made %s loud too", quiet)
	}
	if !a.focusQuiet() {
		t.Error("the breakthrough made the sidebar's links loud (no conversation's)")
	}
	// off and on again: the overrides are gone
	a.toggleFocusMode()
	a.focusChanged()
	a.toggleFocusMode()
	a.focusChanged()
	if !a.focusQuietIn(loud) {
		t.Errorf("focus mode turned on again: %s still loud", loud)
	}
}
