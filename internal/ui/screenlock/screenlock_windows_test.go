//go:build windows

package screenlock

import (
	"testing"
	"time"
)

// The message-only window turns WM_WTSSESSION_CHANGE's lock and unlock
// into callbacks (posted here by hand: nobody locks a CI runner).
func TestWatchWTS(t *testing.T) {
	hw := make(chan uintptr, 1)
	testHookWindowCreated = func(h uintptr) { hw <- h }
	defer func() { testHookWindowCreated = nil }()
	got := make(chan bool, 4)
	stop, err := Watch(func(l bool) { got <- l })
	if err != nil {
		t.Skip("WTSRegisterSessionNotification:", err)
	}
	defer stop()
	h := <-hw
	for _, c := range []struct {
		code uintptr
		want bool
	}{{wtsSessionLock, true}, {wtsSessionUnlock, false}} {
		procPostMessageW.Call(h, wmWTSSessionChange, c.code, 0)
		select {
		case l := <-got:
			if l != c.want {
				t.Fatalf("code %d: locked=%v", c.code, l)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("code %d: no callback", c.code)
		}
	}
}
