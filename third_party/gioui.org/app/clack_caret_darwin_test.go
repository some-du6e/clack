// SPDX-License-Identifier: Unlicense OR MIT

//go:build darwin && !ios

package app

import (
	"testing"
	"time"

	"gioui.org/app/cocoatest"
	"gioui.org/widget"
)

// Clack patch (CLACK_PATCHES.md §47) on a real macOS desktop
// (CLACK_MACOS_WINDOW_TESTS, ci's macos job): a window opens with the
// caret blink period Chromium's OsSettingsProviderMac::CaretBlinkInterval
// reads from the user defaults (TextInsertionCaretBlinkPeriodFromDefaults:
// the average of NSTextInsertionPointBlinkPeriodOn and ...Off when either
// is set, no blinking for an on period over a minute, Chromium's 500 ms
// when neither is set or either is negative), and reads it again when the
// app becomes active. The test sets the two in its own process's defaults
// (over the runner's NSGlobalDomain) and removes them after.
func TestCaretBlinkFromOSMac(t *testing.T) {
	if !macWindowTestsEnabled() {
		t.Skip("CLACK_MACOS_WINDOW_TESTS not set")
	}
	ms := func(v int64) *int64 { return &v }
	set := func(on, off *int64, post bool) {
		cocoatest.OnMain(func() { clackCaretDefaults(on, off, post) })
	}
	t.Cleanup(func() {
		set(nil, nil, false)
		widget.SetCaretBlinkInterval(-1)
	})
	var runner int64
	set(nil, nil, false)
	cocoatest.OnMain(func() { runner = clackCaretPeriodMs() })
	t.Logf("the runner's own period (NSGlobalDomain, AXPrefersNonBlinkingTextInsertionIndicator): %d ms (-1: none set)", runner)
	if runner != -1 {
		t.Skipf("the runner sets its own period (%d ms: NSGlobalDomain's, or a caret that doesn't blink), which the cases with one period unset would read", runner)
	}

	set(ms(300), ms(900), false)
	widget.SetCaretBlinkInterval(-1)
	h := newMacTestWindow(t, func() bool { return false })
	defer h.destroy(t)
	macWaitUntil(t, "a frame", func() bool { return h.frames.Load() > 0 })
	if got := widget.CaretBlinkInterval(); got != 600*time.Millisecond {
		t.Errorf("a window opened with on 300 ms, off 900 ms: CaretBlinkInterval %v, want their average, 600 ms", got)
	}
	for _, c := range []struct {
		name    string
		on, off *int64
		want    time.Duration
	}{
		{"on 200 ms, off 400 ms", ms(200), ms(400), 300 * time.Millisecond},
		{"on 700 ms only", ms(700), nil, 350 * time.Millisecond},
		{"off 500 ms only", nil, ms(500), 250 * time.Millisecond},
		{"an on period over a minute (no blinking)", ms(61000), ms(500), 0},
		{"a negative on period (Chromium's default)", ms(-5), ms(500), 500 * time.Millisecond},
		{"on 0, off 0 (as if neither were set)", ms(0), ms(0), 500 * time.Millisecond},
	} {
		set(c.on, c.off, true) // and the app becomes active
		if got := widget.CaretBlinkInterval(); got != c.want {
			t.Errorf("%s, then the app became active: CaretBlinkInterval %v, want %v", c.name, got, c.want)
		}
	}
	set(nil, nil, true)
	if got := widget.CaretBlinkInterval(); got != 500*time.Millisecond {
		t.Errorf("neither set: CaretBlinkInterval %v, want Chromium's 500 ms", got)
	}
}
