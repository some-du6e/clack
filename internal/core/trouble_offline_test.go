package core

// The socket manager's WAIT_FOR_CONNECTIVITY (boot-render RpHI, build
// 889ac449a32d): after a failure's backoff the socket connects only while
// the client is online, so with no network it dials nothing and trouble
// connecting never turns on: the offline states show alone, never the
// DegradedBanner beside them. Back online, it connects. Synthetic data on
// internal/fakeslack.

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
)

func TestTroubleWaitsForTheNetworkLikeSlack(t *testing.T) {
	var up atomic.Bool
	up.Store(true)
	oldProbe, oldPoll, oldThrottle := clientOnlineProbe, clientOnlinePoll, clientOfflineThrottle
	clientOnlineProbe, clientOnlinePoll, clientOfflineThrottle = func() bool { return up.Load() }, 20*time.Millisecond, 20*time.Millisecond
	t.Cleanup(func() { clientOnlineProbe, clientOnlinePoll, clientOfflineThrottle = oldProbe, oldPoll, oldThrottle })
	t.Cleanup(setTroubleThresholds(0, 1)) // any failed connect would turn trouble on
	a, fs, _ := rtApp(t)
	stubNetworkProbe(t) // the socket's wait is the network wait: its pixel, loaded once the watcher says online
	up.Store(false)
	waitFor(t, "the client offline", 5*time.Second, func() bool { return !a.ClientOnline() })
	fs.SetFault("ws", fakeslack.Fault{Status: 503})
	dials := fs.CallCount("ws")
	fs.DropSockets()
	time.Sleep(4 * time.Second) // the backoff's first two tries (1 s, then 2 s)
	if n := fs.CallCount("ws") - dials; n != 0 {
		t.Errorf("offline, the socket dialled %d times", n)
	}
	if connectionTrouble(a) {
		t.Error("offline, trouble connecting turned on (the DegradedBanner over the offline states)")
	}
	fs.SetFault("ws", fakeslack.Fault{})
	up.Store(true)
	waitFor(t, "connected again once online", 10*time.Second, func() bool { return fs.Sockets() > 0 })
}
