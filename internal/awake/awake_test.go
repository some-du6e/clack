package awake

import (
	"testing"
	"time"
)

// Now never goes backwards and, while the computer stays awake (as it
// does on a CI runner), keeps pace with Go's monotonic clock: on Windows
// QueryUnbiasedInterruptTime and InterruptTime only part over sleep.
func TestNowKeepsPaceAwake(t *testing.T) {
	n0, m0 := Now(), time.Now()
	if n0 <= 0 {
		t.Fatalf("Now() = %d, want > 0", n0)
	}
	last := n0
	for i := 0; i < 200; i++ {
		n := Now()
		if n < last {
			t.Fatalf("Now went backwards: %d after %d", n, last)
		}
		last = n
		time.Sleep(time.Millisecond)
	}
	got, want := Since(n0), time.Since(m0)
	// both clocks tick with the clock interrupt on Windows (15.6 ms at worst)
	if d := got - want; d < -20*time.Millisecond || d > 20*time.Millisecond {
		t.Errorf("Since says %v, time.Since %v", got, want)
	}
	if got < 150*time.Millisecond {
		t.Errorf("Since says %v after 200 sleeps of 1 ms", got)
	}
}
