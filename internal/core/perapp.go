package core

import (
	"sync"

	"github.com/some-du6e/clack/internal/store"
)

// perApp is one of the package's maps keyed by *App: an App's state that
// lives beside the App rather than in it (the package keeps about thirty).
// Each is made by newPerApp, so Release can drop a finished App from all
// of them. A map's entry keeps its App reachable, and with it the store,
// the client and every subscription: a replaced session stayed in memory
// for good, and internal/ui's tests, which make hundreds of Apps in one
// process, held 9 GB live by their end (ci-speed-3).
type perApp struct{ sync.Map }

var perAppMaps []*perApp

func newPerApp() *perApp {
	m := &perApp{}
	perAppMaps = append(perAppMaps, m)
	return m
}

// Release drops a finished App's process-wide state: its entries in the
// per-App maps and appsByStore, and its store's (store.Release). Close
// ends with it. Call it only once the App is done for good: an App that
// runs on after it starts those maps' state over.
func (a *App) Release() {
	a.closeSubscribers()
	for _, m := range perAppMaps {
		m.Delete(a)
	}
	if v, ok := appsByStore.Load(a.S); ok && v.(*App) == a {
		appsByStore.Delete(a.S)
	}
	store.Release(a.S)
}

// closeSubscribers closes every channel the App handed out to
// subscribers, Subscribe's, SubscribeRaw's and its Interactive's, so the
// goroutines ranging over them end. Each such goroutine kept the App, and
// what it subscribed for, reachable for good: internal/ui's tests left
// hundreds (messages' hubs, composer's typing, blockkit, views' watchers,
// core's own). Release calls it after the pump and the background work
// stopped (Close follows StopTraffic), so no event is sent after.
func (a *App) closeSubscribers() {
	a.subMu.Lock()
	subs := make([]*sub, 0, len(a.subs))
	for s := range a.subs {
		subs = append(subs, s)
	}
	clear(a.subs)
	a.subMu.Unlock()
	for _, s := range subs {
		close(s.ch)
	}
	if p, ok := rawTapIndex.Load(a); ok {
		if t := p.(*rawTapPtr).Swap(nil); t != nil {
			t.mu.Lock() // a tap in flight on t finishes first
			for c := range t.subs {
				close(c)
			}
			t.subs = nil
			t.mu.Unlock()
		}
	}
	if v, ok := interactiveIndex.Load(a); ok {
		v.(*Interactive).closeSubs()
	}
}
