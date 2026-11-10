//go:build darwin && cgo

package screenlock

import "testing"

// The observers register and come off again (the notifications arrive on
// the main run loop, which a test binary doesn't run).
func TestWatchDarwin(t *testing.T) {
	stop, err := Watch(func(bool) {})
	if err != nil {
		t.Fatal(err)
	}
	stop()
}
