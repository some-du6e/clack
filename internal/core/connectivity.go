package core

// Slack's two waits for a connection to come back (web client build
// 889ac449a32d), and what starts them. The API queue (client-boot 7quX
// ApiQ, its counts in internal/slack/apihealth.go) pauses:
//
//   - after ten answers of 500 or more in a row: setServiceOffline, then
//     awaitServiceReconnection (3h1+), which polls api.test until it
//     answers and then sets serviceOnline and clientOnline;
//   - after ten tries in a row with no answer, or whenever clientOnline is
//     already false (its tick checks every second): setClientOffline, then
//     awaitNetworkReconnection (d55I), which loads the CDN's test pixel
//     until one does and then sets clientOnline.
//
// The socket's WAIT_FOR_CONNECTIVITY (boot-render RpHI) awaits the same
// network wait (RTM.AwaitOnline).
//
// Each wait runs rounds through a ConnectivityManager (RFm0): up to ten
// tries, each after the last one's retryAfter (at most 300 s). A round that
// fails sets the state offline again (setClientOnline(false) moves
// lastClientConnection; setServiceOnline(false) doesn't), and the next
// round starts 5 s later (retryNetworkConnection, retryServiceConnection).
// One wait of each kind runs at a time (awaitNetworkReconnection's and
// awaitServiceReconnection's shared promise).
//
// A network try (2Aob testNetworkConnectivity) reads navigator.onLine,
// Clack's interface watcher (online.go). While it says offline, a try
// fails at once and is tried again in a second, except every 10th (in the
// desktop app, until the trust check has run once) and every 100th, which
// load the pixels anyway; the count runs across rounds and resets only on
// a success. A pixel that loads on a 10th try while the watcher says
// offline runs checkNavigatorOnlineTrustworthiness: a second load, and if
// the watcher still says offline, its reading is taken as online for the
// rest of the session (Slack redefines navigator.onLine as true). A fetch
// that failed sets the CDN's full backoff: the next try waits out what the
// failed fetches asked for before it loads again.
//
// Not ported: the queue holding every call while offline (_tick returns
// while !isOnline). Calls go out as before.

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// The waits' timings (vars for tests): ConnectivityManager's maxAttempts
// and maxRetryAfter, and the 5 s between rounds.
var (
	connMaxAttempts   = 10
	connMaxRetryAfter = 300 * time.Second
	connRoundGap      = 5 * time.Second
	// connRetryScale shortens every retryAfter (tests)
	connRetryScale = 1.0
)

// The two requests (tests replace them).
var (
	cdnProbe = slack.ProbeCDN
	apiTest  = slack.APITest
)

// connState is the waits' state.
type connState struct {
	mu         sync.Mutex
	serviceOff bool          // !serviceOnline (the connectivity store's)
	netWait    chan struct{} // the network wait running: closed when it ends
	svcWait    chan struct{} // the service wait running: closed when it ends
	// testNetworkConnectivity's module state
	tries     int       // f: failed tries since the last success
	trustRan  bool      // _: checkNavigatorOnlineTrustworthiness ran
	untrusted bool      // g: the watcher's offline reading isn't believed
	cdnWait   time.Time // b + y: no pixel fetch before then
}

// serviceOnline is the connectivity store's serviceOnline.
func (a *App) serviceOnline() bool {
	a.conn.mu.Lock()
	defer a.conn.mu.Unlock()
	return !a.conn.serviceOff
}

// wireConnectivity hooks the API counts and the socket to the waits.
func (a *App) wireConnectivity() {
	a.C.SetAPIHealthHooks(func() { a.awaitNetwork(true) }, func() { a.awaitService() })
	if a.RTM != nil {
		a.RTM.AwaitOnline = func(ctx context.Context) bool {
			if a.ClientOnline() {
				return false
			}
			select {
			case <-a.awaitNetwork(false):
			case <-ctx.Done():
			case <-a.bg.Done():
			}
			return true
		}
	}
}

// awaitNetwork is awaitNetworkReconnection: the network wait, started
// unless it runs; the channel closes when it ends (the network is back).
// offline: the API queue's reconnectClient, which sets the client offline
// first (setClientOffline); the socket's wait doesn't.
func (a *App) awaitNetwork(offline bool) <-chan struct{} {
	c := &a.conn
	c.mu.Lock()
	if c.netWait != nil {
		w := c.netWait
		c.mu.Unlock()
		return w
	}
	w := make(chan struct{})
	c.netWait = w
	c.mu.Unlock()
	if offline {
		a.setClientOnline(false) // setClientOffline
	}
	go func() {
		defer func() {
			c.mu.Lock()
			c.netWait = nil
			c.mu.Unlock()
			close(w)
		}()
		log.Printf("core: the network looks gone; waiting for it (the CDN's test pixel)")
		for round := 1; ; round++ {
			if a.connRound(a.networkTry) {
				a.setClientOnline(true)
				a.C.ResetAPIErrors() // the queue's _unpause
				log.Printf("core: the network is back (round %d)", round)
				return
			}
			a.setClientOnline(false) // the round failed: lastClientConnection moves
			if !a.sleepConn(connRoundGap) {
				return
			}
		}
	}()
	return w
}

// awaitService is setServiceOffline and awaitServiceReconnection.
func (a *App) awaitService() <-chan struct{} {
	c := &a.conn
	c.mu.Lock()
	if c.svcWait != nil {
		w := c.svcWait
		c.mu.Unlock()
		return w
	}
	w := make(chan struct{})
	c.svcWait = w
	c.mu.Unlock()
	a.setServiceOnline(false)
	go func() {
		defer func() {
			c.mu.Lock()
			c.svcWait = nil
			c.mu.Unlock()
			close(w)
		}()
		log.Printf("core: Slack's API keeps failing; waiting for api.test")
		for round := 1; ; round++ {
			if a.connRound(func(ctx context.Context) (time.Duration, bool) {
				wait, err := apiTest(ctx)
				return wait, err == nil
			}) {
				a.setServiceOnline(true)
				a.setClientOnline(true)
				a.C.ResetAPIErrors()
				log.Printf("core: Slack's API answers again (round %d)", round)
				return
			}
			a.setServiceOnline(false)
			if !a.sleepConn(connRoundGap) {
				return
			}
		}
	}()
	return w
}

// connRound is a ConnectivityManager round: up to connMaxAttempts tries,
// each after the last failure's retryAfter (capped); true once one worked.
func (a *App) connRound(try func(ctx context.Context) (time.Duration, bool)) bool {
	for n := 1; ; n++ {
		if a.bg.Err() != nil {
			return false
		}
		wait, ok := try(a.bg)
		if ok {
			return true
		}
		if n >= connMaxAttempts || !a.sleepConn(min(wait, connMaxRetryAfter)) {
			return false
		}
	}
}

// scaledConn is d as the waits take it (tests shorten them).
func scaledConn(d time.Duration) time.Duration { return time.Duration(float64(d) * connRetryScale) }

// sleepConn waits d (scaled for tests); false when the App shut down.
func (a *App) sleepConn(d time.Duration) bool {
	t := time.NewTimer(scaledConn(d))
	defer t.Stop()
	select {
	case <-a.bg.Done():
		return false
	case <-t.C:
		return true
	}
}

// networkTry is testNetworkConnectivity: one try, its retryAfter when it
// failed.
func (a *App) networkTry(ctx context.Context) (time.Duration, bool) {
	c := &a.conn
	c.mu.Lock()
	tenth := !c.untrusted && !c.trustRan && c.tries > 0 && c.tries%10 == 0 // and isDesktopApp
	navOffline := !c.untrusted && !a.watcherReading()
	if navOffline && !tenth && c.tries%100 != 99 {
		c.tries++
		c.mu.Unlock()
		return time.Second, false // "Browser says we're offline"
	}
	if left := time.Until(c.cdnWait); left > 0 {
		c.mu.Unlock()
		return time.Duration(float64(left) / connRetryScale), false // waiting on the CDN's full backoff (not counted)
	}
	c.mu.Unlock()
	start := time.Now()
	wait, err := cdnProbe(ctx)
	if err != nil {
		c.mu.Lock()
		c.cdnWait = start.Add(scaledConn(wait))
		c.tries++
		c.mu.Unlock()
		return time.Second, false
	}
	if navOffline && tenth {
		a.checkWatcherTrust(ctx)
	}
	c.mu.Lock()
	c.cdnWait, c.tries = time.Time{}, 0
	c.mu.Unlock()
	return 0, true
}

// checkWatcherTrust is checkNavigatorOnlineTrustworthiness: the pixels
// loaded although the watcher says offline; a second load, and if the
// watcher still says so, its offline reading isn't believed again.
func (a *App) checkWatcherTrust(ctx context.Context) {
	c := &a.conn
	c.mu.Lock()
	c.trustRan = true
	c.mu.Unlock()
	if _, err := cdnProbe(ctx); err != nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !a.watcherReading() {
		log.Printf("core: the interface watcher says offline while the CDN loads; not believing it again")
		c.untrusted = true
	}
}

// setServiceOnline is the connectivity store's setServiceOnline: it
// doesn't move lastClientConnection; isOnline (and so wasRecentlyOnline)
// reads it, so the views hear of it as of clientOnline.
func (a *App) setServiceOnline(on bool) {
	a.conn.mu.Lock()
	a.conn.serviceOff = !on
	a.conn.mu.Unlock()
	a.emit(Event{Type: "client_online", Connected: a.ClientOnline()})
}
