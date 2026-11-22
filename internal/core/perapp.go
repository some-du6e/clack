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

// LoadOrStore is sync.Map's, but a released App's entry isn't kept: a
// lookup after Release (a goroutine finishing after Close) used to put
// the App back, and with it its session, for good (ci-speed-3: a heap
// dump found finished tests' sessions in teamPrefIndex, codeOpenStates
// and listStates). The caller still gets the state, kept nowhere.
func (m *perApp) LoadOrStore(key, value any) (actual any, loaded bool) {
	actual, loaded = m.Map.LoadOrStore(key, value)
	if a, ok := key.(*App); ok && a.released.Load() {
		m.Map.CompareAndDelete(key, actual)
	}
	return actual, loaded
}

// Release drops a finished App's process-wide state: its entries in the
// per-App maps and appsByStore, and its store's (store.Release). Close
// ends with it. Call it only once the App is done for good: an App that
// runs on after it starts those maps' state over.
func (a *App) Release() {
	// first: the maps' LoadOrStore keeps nothing of the App from here, and
	// a subscriber from here gets a closed channel
	a.released.Store(true)
	a.closeSubscribers()
	// a temporary mute's expiry: its timer kept the App until then (hours
	// on; the runtime's timer heap is a root)
	a.mu.Lock()
	if a.tempMuteTimer != nil {
		a.tempMuteTimer.Stop()
		a.tempMuteTimer = nil
	}
	a.mu.Unlock()
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
