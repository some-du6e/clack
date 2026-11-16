//go:build go1.25

package tracerec

import "testing"

// parseWindow: there's no reader for Go 1.25's trace format here (x/exp's
// stops at Go 1.24's, and this build doesn't link x/exp at all), so the
// test checks the window's header and size only.
func parseWindow(t *testing.T, b []byte) (events, stw, gc int, ok bool) {
	t.Logf("tracerec: a %d-byte window, not parsed (no reader for this trace format here)", len(b))
	return 0, 0, 0, false
}
