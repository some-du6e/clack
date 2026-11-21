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
	for _, m := range perAppMaps {
		m.Delete(a)
	}
	if v, ok := appsByStore.Load(a.S); ok && v.(*App) == a {
		appsByStore.Delete(a.S)
	}
	store.Release(a.S)
}
