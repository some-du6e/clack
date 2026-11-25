package slack

import (
	"testing"
	"time"
)

// troubleLog collects OnTrouble's changes.
func troubleLog(r *RTM) chan bool {
	ch := make(chan bool, 64)
	setOnTrouble(r, func(on bool) { ch <- on })
	return ch
}

func waitTrouble(t *testing.T, ch chan bool, want bool, what string) {
	t.Helper()
	select {
	case got := <-ch:
		if got != want {
			t.Fatalf("%s: trouble went %v, want %v", what, got, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("%s: trouble never went %v", what, want)
	}
}

func noTrouble(t *testing.T, ch chan bool, what string) {
	t.Helper()
	select {
	case got := <-ch:
		t.Fatalf("%s: trouble went %v", what, got)
	default:
	}
}

// Slack's socket manager's troubleConnecting
// (_maybeDispatchTroubleConnectingAction): before any hello, the first
// failed connect; after one, the third failure in a row once 30 s have
// passed since the first of them, on the wall clock; a hello ends it.
func TestRTMTroubleConnectingLikeSlack(t *testing.T) {
	t.Run("never connected", func(t *testing.T) {
		fastBackoff(t)
		newFakeClock(t)
		g, c := newFakeGateway(t)
		g.refuse.Store(1)
		r := c.NewRTM()
		trouble := troubleLog(r)
		runRTM(t, r)
		waitTrouble(t, trouble, true, "the first failure with no connection before")
		if !rtmTrouble(r) {
			t.Error("Trouble() is false after OnTrouble(true)")
		}
		gc := g.next(t)
		gc.send(t, `{"type":"hello"}`)
		waitTrouble(t, trouble, false, "a hello")
	})
	t.Run("after a connection", func(t *testing.T) {
		fastBackoff(t)
		clock := newFakeClock(t)
		g, c := newFakeGateway(t)
		r := c.NewRTM()
		trouble := troubleLog(r)
		runRTM(t, r)
		gc := g.next(t)
		gc.send(t, `{"type":"hello"}`)
		gc.sync(t, r)
		// three refused, then one that opens and closes without hello:
		// the fourth failure
		g.refuse.Store(3)
		gc.close()
		gc = g.next(t)
		noTrouble(t, trouble, "three failures within the window")
		clock.pass(31 * time.Second)
		gc.close()
		waitTrouble(t, trouble, true, "the fourth failure, 31 s after the first")
		gc = g.next(t)
		gc.send(t, `{"type":"hello"}`)
		waitTrouble(t, trouble, false, "a hello")
	})
	t.Run("ConnectNow", func(t *testing.T) {
		b := rtmBackoffBase
		rtmBackoffBase = time.Hour // a wait only ConnectNow cuts
		t.Cleanup(func() { rtmBackoffBase = b })
		newFakeClock(t)
		g, c := newFakeGateway(t)
		g.refuse.Store(1)
		r := c.NewRTM()
		trouble := troubleLog(r)
		runRTM(t, r)
		waitTrouble(t, trouble, true, "the first failure")
		for dl := time.Now().Add(5 * time.Second); !rtmWaiting(r); time.Sleep(5 * time.Millisecond) {
			if time.Now().After(dl) {
				t.Fatal("Run never waited out its backoff")
			}
		}
		connectNow(r)
		g.next(t) // dialled at once, not an hour on
	})
}
