//go:build windows

package awake

import (
	"testing"
	"time"
)

// The clock really is QueryUnbiasedInterruptTime here (Go's own clock
// counts sleep on Windows), and the call works on this Windows.
func TestUnbiasedInterruptTime(t *testing.T) {
	u0, ok := unbiased()
	if !ok || u0 <= 0 {
		t.Fatalf("QueryUnbiasedInterruptTime: %d, %v", u0, ok)
	}
	n0 := Now()
	time.Sleep(100 * time.Millisecond)
	u1, _ := unbiased()
	n1 := Now()
	if d := time.Duration(u1 - u0); d < 80*time.Millisecond || d > time.Second {
		t.Errorf("unbiased interrupt time moved %v over a 100 ms sleep", d)
	}
	// Now is the unbiased clock: the two moved alike (a clock tick apart at most)
	if d := time.Duration((n1 - n0) - (u1 - u0)); d < -20*time.Millisecond || d > 20*time.Millisecond {
		t.Errorf("Now moved %v, the unbiased clock %v", time.Duration(n1-n0), time.Duration(u1-u0))
	}
	if s := time.Duration(slept.Load()); s < -20*time.Millisecond || s > 20*time.Millisecond {
		t.Errorf("%v between Go's clock and Now on a runner that never slept", s)
	}
}
