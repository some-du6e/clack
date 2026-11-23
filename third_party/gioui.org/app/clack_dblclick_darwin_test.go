// SPDX-License-Identifier: Unlicense OR MIT

//go:build darwin && !ios

package app

import (
	"slices"
	"testing"
	"time"
)

// Clack patch 48 on a real macOS window (CLACK_MACOS_WINDOW_TESTS): a
// click's count is AppKit's, NSEvent clickCount, as Chromium's mac input
// builder takes it, whatever the time between the presses; and the rule
// for presses without one reads NSEvent.doubleClickInterval.
func TestClackClickCountMac(t *testing.T) {
	if !macWindowTestsEnabled() {
		t.Skip("CLACK_MACOS_WINDOW_TESTS not set")
	}
	iv, slop := macDoubleClick()
	t.Logf("NSEvent.doubleClickInterval %v", iv)
	if iv <= 0 || iv > 5*time.Second || slop.X != 2 || slop.Y != 2 {
		t.Errorf("the rule without AppKit's count: %v and %v", iv, slop)
	}
	h := newMacTestWindowWith(t, func() bool { return false })
	defer h.destroy(t)
	h.makeKey(t)
	const x, y = 400, 300 // window points, under the band
	got := func(n int) []int {
		t.Helper()
		var out []int
		for range n {
			select {
			case c := <-h.clicks:
				out = append(out, c)
			case <-time.After(5 * time.Second):
				t.Fatalf("%d clicks of %d (window %+v)", len(out), n, h.state())
			}
		}
		return out
	}
	// a slow double click AppKit counted (a long double-click speed in
	// System Settings): 1, then 2 nine tenths of a second later
	h.click(x, y, 1)
	time.Sleep(900 * time.Millisecond)
	h.click(x, y, 2)
	if c := got(2); !slices.Equal(c, []int{1, 2}) {
		t.Errorf("AppKit's 1 then 2, 900 ms apart: %v, want [1 2]", c)
	}
	time.Sleep(time.Second)
	// two singles AppKit counted 50 ms apart (another place, say): the rule
	// alone would have made them a double click
	h.click(x, y, 1)
	time.Sleep(50 * time.Millisecond)
	h.click(x, y, 1)
	if c := got(2); !slices.Equal(c, []int{1, 1}) {
		t.Errorf("AppKit's 1 and 1, 50 ms apart: %v, want [1 1]", c)
	}
	time.Sleep(time.Second)
	// a triple click
	h.click(x, y, 1)
	h.click(x, y, 2)
	h.click(x, y, 3)
	if c := got(3); !slices.Equal(c, []int{1, 2, 3}) {
		t.Errorf("AppKit's 1, 2, 3: %v, want [1 2 3]", c)
	}
}
