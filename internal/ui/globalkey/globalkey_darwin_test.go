//go:build darwin && cgo

package globalkey

import "testing"

// Registering and releasing doesn't fail (a test binary has no main run
// loop: the Carbon calls are queued for it, so presses aren't testable).
func TestRegisterDarwin(t *testing.T) {
	stop, err := Register(func() {})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Register(func() {}); err != ErrTaken {
		t.Fatalf("second registration: %v, want ErrTaken", err)
	}
	stop()
	stop()
}
