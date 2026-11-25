package slack

// The socket manager's WAIT_FOR_CONNECTIVITY (boot-render RpHI, build
// 889ac449a32d): after a failure's backoff the socket connects only while
// the client is online; otherwise it awaits the network's return
// (awaitNetworkReconnection) and dials nothing meanwhile, and the wait
// clears its failures' count. A local gateway refuses every upgrade.

import (
	"context"
	"testing"
	"time"
)

func TestRTMWaitForConnectivityLikeSlack(t *testing.T) {
	fastBackoff(t)
	newFakeClock(t)
	g, c := newFakeGateway(t)
	g.refuse.Store(1 << 20) // every upgrade answered 503
	r := c.NewRTM()
	type entry struct{ fails int }
	waits := make(chan entry, 16)
	release := make(chan bool, 16) // true: the network came back while waiting; false: online, no wait
	// the hook runs on Run's goroutine, so it may read r.fails
	setAwaitOnline(r, func(ctx context.Context) bool {
		waits <- entry{r.fails}
		select {
		case waited := <-release:
			return waited
		case <-ctx.Done():
			return true
		}
	})
	dials := func() int32 { return 1<<20 - g.refuse.Load() }
	runRTM(t, r)

	next := func(what string) entry {
		t.Helper()
		select {
		case e := <-waits:
			return e
		case <-time.After(5 * time.Second):
			t.Fatalf("%s: the socket never checked for the client being online before connecting again", what)
			return entry{}
		}
	}
	e := next("after the first failed connect")
	if e.fails != 1 {
		t.Errorf("one failed connect, then the check: %d failures counted", e.fails)
	}
	d := dials()
	time.Sleep(150 * time.Millisecond) // many backoffs' worth (fastBackoff)
	if got := dials(); got != d {
		t.Fatalf("waiting for the network, the socket dialled %d more times", got-d)
	}
	release <- true // the network is back
	e = next("after the wait and a failed connect")
	if dials() != d+1 {
		t.Errorf("after the wait the socket dialled %d times, want once before checking again", dials()-d)
	}
	if e.fails != 1 {
		t.Errorf("the wait didn't clear the failures' count: %d after one more failure, want 1", e.fails)
	}

	// online: no wait, the count runs on
	release <- false
	e = next("online, after a failed connect")
	if e.fails != 2 {
		t.Errorf("online, no wait: %d failures counted, want 2", e.fails)
	}
	release <- false
}
