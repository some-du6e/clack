package core

// Slack's API queue's offline rules (client-boot 7quX, d55I, 3h1+, 2Aob,
// aZ2/; build 889ac449a32d). Ten tries in a row with no answer make the
// client offline until the CDN's test pixel loads; ten answers of 500 or
// more make the service offline until api.test answers. Synthetic calls on
// internal/fakeslack, its Slack hosts routed (RouteSlackHosts) and the
// interface watcher pinned (PinNetwork: online, never polled).

import (
	"context"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// connApp is an App on the fake with its Slack hosts routed, the network
// pinned and the waits' seconds made milliseconds.
func connApp(t *testing.T) (*App, *fakeslack.Server, <-chan Event) {
	t.Helper()
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	a := New(slack.New(fs.Creds()), store.New(t.TempDir()))
	a.PinNetwork()
	t.Cleanup(fs.RouteSlackHosts(a.C))
	t.Cleanup(a.Shutdown)
	scaleConnWaits(t, 0.01) // a second is 10 ms, a round's 5 s gap 50 ms
	evs, stop := a.Subscribe()
	t.Cleanup(stop)
	return a, fs, evs
}

// apiCalls makes n calls of method, each its own (the backoff guard
// holds none back).
func apiCalls(a *App, method string, n int) {
	for i := 0; i < n; i++ {
		_ = a.C.Call(slack.NoBackoff(context.Background()), method, url.Values{"channel": {"C0FAKE00001"}, "cursor": {"synthetic" + strconv.Itoa(i)}}, nil)
	}
}

func lastConnection(a *App) time.Time {
	a.online.mu.Lock()
	defer a.online.mu.Unlock()
	return a.online.changed
}

func TestAPIFailuresTakeClientOfflineLikeSlack(t *testing.T) {
	a, fs, evs := connApp(t)
	fs.SetFault("test_cdn_pixel", fakeslack.Fault{Status: 503})
	hangUps := func(n int) { // every try dropped (a read's resend on a fresh connection is the same try)
		fs.SetFault("conversations.history", fakeslack.Fault{HangUp: true})
		apiCalls(a, "conversations.history", n)
		fs.SetFault("conversations.history", fakeslack.Fault{})
	}
	hangUps(9)
	if !a.ClientOnline() {
		t.Fatal("offline after nine tries with no answer")
	}
	hangUps(1)
	if a.ClientOnline() {
		t.Fatal("still online after ten tries in a row with no answer (the API queue's setClientOffline)")
	}
	waitEvent(t, evs, 2*time.Second, func(ev Event) bool { return ev.Type == "client_online" && !ev.Connected })

	// each failed round of pixel loads sets it offline again, which moves
	// lastClientConnection (wasRecentlyOnline's grace starts over)
	at := lastConnection(a)
	waitFor(t, "a failed round of the CDN's test pixel", 5*time.Second, func() bool { return lastConnection(a).After(at) })
	if a.ClientOnline() {
		t.Fatal("online while the pixel doesn't load")
	}
	if fs.CallCount("test_cdn_pixel") < int64(connMaxAttempts) {
		t.Errorf("a failed round loaded the pixel %d times, want a round's %d tries", fs.CallCount("test_cdn_pixel"), connMaxAttempts)
	}
	fs.SetFault("test_cdn_pixel", fakeslack.Fault{})
	waitFor(t, "the pixel loading puts the client online", 5*time.Second, a.ClientOnline)

	// no ten in a row: nine with no answer, a success, nine more
	hangUps(9)
	apiCalls(a, "auth.test", 1)
	hangUps(9)
	// and a timeout (CLIENT_TIMEOUT) resets the count as a success does
	fs.SetFault("conversations.info", fakeslack.Fault{Delay: 300 * time.Millisecond, Times: 1})
	ctx, cancel := context.WithTimeout(slack.NoBackoff(context.Background()), 30*time.Millisecond)
	_ = a.C.Call(ctx, "conversations.info", url.Values{"channel": {"C0FAKE00001"}}, nil)
	cancel()
	hangUps(9)
	time.Sleep(50 * time.Millisecond)
	if !a.ClientOnline() {
		t.Error("offline without ten tries in a row with no answer (a success or a timeout between them)")
	}
}

func TestServiceErrorsTakeServiceOfflineLikeSlack(t *testing.T) {
	a, fs, _ := connApp(t)
	fs.SetFault("api.test", fakeslack.Fault{Status: 503})
	// the 10 s grace of wasRecentlyOnline over: only isOnline counts
	a.online.mu.Lock()
	a.online.changed = time.Now().Add(-11 * time.Second)
	a.online.mu.Unlock()
	// 500s: a 503 on the queue's own retries would wait a minute first
	fs.SetFault("conversations.history", fakeslack.Fault{Status: 500, Times: 10})
	apiCalls(a, "conversations.history", 9)
	if on, _ := a.RecentlyOnline(); !on {
		t.Fatal("not recently online after nine 500s")
	}
	apiCalls(a, "conversations.history", 1)
	if on, _ := a.RecentlyOnline(); on {
		t.Fatal("recently online after ten 500s in a row (the API queue's setServiceOffline)")
	}
	waitFor(t, "api.test asked", 5*time.Second, func() bool { return fs.CallCount("api.test") >= int64(connMaxAttempts) })
	if on, _ := a.RecentlyOnline(); on {
		t.Error("recently online while api.test fails")
	}
	if !lastConnection(a).Before(time.Now().Add(-10 * time.Second)) {
		t.Error("a failed round of api.test moved lastClientConnection (setServiceOnline doesn't)")
	}
	fs.SetFault("api.test", fakeslack.Fault{})
	waitFor(t, "api.test's answer puts the service online", 5*time.Second, func() bool { on, _ := a.RecentlyOnline(); return on })
	if !a.ClientOnline() {
		t.Error("api.test answered: the client isn't online (it sets both)")
	}
}
