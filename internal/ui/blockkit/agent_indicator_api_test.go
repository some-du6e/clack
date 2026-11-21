package blockkit

import "testing"

// setIndicatorActive makes the window active or not for the spinners
// until the test ends. The red-evidence commit replaces this file with a
// stub that does nothing (a9cebac9's spinners always ask for frames).
func setIndicatorActive(t *testing.T, active bool) {
	prev := IndicatorActive
	IndicatorActive = func() bool { return active }
	t.Cleanup(func() { IndicatorActive = prev })
}
