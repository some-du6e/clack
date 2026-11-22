package ui

import (
	"sync"

	"github.com/some-du6e/clack/internal/ui/blockkit"
	"github.com/some-du6e/clack/internal/ui/composer"
	"github.com/some-du6e/clack/internal/ui/messages"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/views"
)

// release drops a finished App's entries in the process-wide registries
// keyed by it or by its parts: the package's maps keyed by *App, popups'
// and views' keyed by their Host (the App), its session's message hub and
// the caches keyed by its shapers. Each entry kept the App reachable, and
// through it its session, images and parsed fonts. The process's one App
// never needs it: internal/ui's tests, which make hundreds of Apps in one
// process, call it at each one's end, after its session's stop
// (perfEnv.shutdown). ci-speed-3: ui.test held 9 GB live by its end.
func (a *App) release() {
	if a.insp != nil {
		a.insp.Close() // a tool left on stays among the shooter's wanters (inspect/shooter.go)
	}
	// every package map keyed by *App (TestReleaseCoversAppMaps reads the
	// package's files for them)
	for _, m := range []*sync.Map{&wfRunStates, &wfTabs, &wfTabPending, &tabCtxTags, &tabsMoreState, &tabDrags, &canvasJumps, &canvasTabs,
		&listTabs, &agentTasksTabs, &pinsTabs, &folderTabs, &filesTabs, &tabLinkWant} {
		m.Delete(a)
	}
	// the UI goroutine's
	delete(focusModes, a)
	delete(railStatusTabs, a)
	delete(createStrips, a)
	delete(appNuggets, a)
	popups.ForgetHost(a)
	views.ForgetHost(a)
	blockkit.ForgetHost(a)
	a.closeVideos()      // its players and their goroutines (video.go)
	clear(lastClickMods) // keyed by its widgets (widgets.go; one App at a time here)
	if a.data != nil && a.data.core != nil {
		messages.ForgetCore(a.data.core)
		blockkit.ForgetCore(a.data.core)
		composer.ForgetCore(a.data.core)
	}
	if a.th != nil {
		a.th.ForgetShapers()
	}
	// the process-wide listeners it registered: i18n's locale change, the
	// OS scheme's
	a.unhooks.Lock()
	unhooks := a.unhooks.fns
	a.unhooks.fns = nil
	a.unhooks.Unlock()
	for _, f := range unhooks {
		f()
	}
	// its goroutines: the invalidator, the image cache's sweeps
	if a.invalDone != nil {
		a.invalStop.Do(func() { close(a.invalDone) })
	}
	if a.images != nil {
		a.images.Close()
	}
}

// unhooks are the stops of an App's process-wide listeners (i18n's, the OS
// scheme's): each list kept every App ever made reachable.
type unhooks struct {
	sync.Mutex
	fns []func()
}

// unhookAtRelease keeps a process-wide listener's stop for release.
func (a *App) unhookAtRelease(stop func()) {
	a.unhooks.Lock()
	a.unhooks.fns = append(a.unhooks.fns, stop)
	a.unhooks.Unlock()
}
