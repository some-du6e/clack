package ui

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"gioui.org/layout"
	"gioui.org/x/explorer"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/popups"
)

// Preferences > Advanced > Download location: Slack desktop's
// DownloadLocationPref. "Choose location for each download" (on when
// PrefSSBFileDownloadPath is cleared) opens the system's save dialog for
// every download; off, downloads go to the folder in the box, which
// Change… picks (Slack's selectDownloadDirectory). ~/Downloads until one
// is picked.

// downloadPrefs is the setting for popups' savers, which run off the UI
// goroutine.
type downloadPrefs struct {
	dir atomic.Pointer[string]
	ask atomic.Bool

	mu     sync.Mutex
	asking *dirAsk // the folder chooser open now
}

// startDownloads hands popups the setting (New).
func (a *App) startDownloads() {
	a.syncDownloadPrefs()
	popups.DownloadDir = func() string {
		if p := a.dl.dir.Load(); p != nil {
			return *p
		}
		return ""
	}
	popups.ChooseDownloadDir = a.chooseDownloadDir
}

func (a *App) syncDownloadPrefs() {
	d := a.prefs.DownloadPath
	a.dl.dir.Store(&d)
	a.dl.ask.Store(a.prefs.DownloadAsk)
}

// chooseDownloadDir is the folder a download goes to when "Choose
// location for each download" is on: the system's folder chooser, as
// Slack's startFileDownloadEpic asks with showOpenDialog (openDirectory,
// createDirectory, "Save") when PrefSSBFileDownloadPath is cleared. One
// chooser at a time: downloads that start while it's open share its
// answer (Slack's fX, shareReplay). asked is false when it's off or the
// platform has no chooser: the folder then. Off the UI goroutine.
func (a *App) chooseDownloadDir() (string, bool, error) {
	e := a.explorer
	if !a.dl.ask.Load() || e == nil {
		return "", false, nil
	}
	a.dl.mu.Lock()
	c := a.dl.asking
	if c == nil {
		c = &dirAsk{done: make(chan struct{})}
		a.dl.asking = c
		go func() {
			dir, err := e.ChooseFolder()
			switch {
			case errors.Is(err, explorer.ErrUserDecline), err == nil && dir == "":
				err = popups.ErrDownloadCanceled
			case errors.Is(err, explorer.ErrNotAvailable):
				c.none = true
			}
			c.dir, c.err = dir, err
			a.dl.mu.Lock()
			a.dl.asking = nil
			a.dl.mu.Unlock()
			close(c.done)
		}()
	}
	a.dl.mu.Unlock()
	<-c.done
	if c.none {
		return "", false, nil
	}
	return c.dir, true, c.err
}

// dirAsk is one open folder chooser and its answer.
type dirAsk struct {
	done chan struct{}
	dir  string
	err  error
	none bool // no chooser on this platform
}

// downloadDirShown is the box's text: the folder, ~ for home.
func (a *App) downloadDirShown() string {
	d := a.prefs.DownloadPath
	if d == "" {
		if home, err := os.UserHomeDir(); err == nil {
			d = filepath.Join(home, "Downloads")
		}
	}
	return homeTilde(d)
}

func (a *App) toggleDownloadAsk() {
	a.prefs.DownloadAsk = !a.prefs.DownloadAsk
	a.syncDownloadPrefs()
	a.savePrefs()
}

// changeDownloadDir is Change…: the system's folder chooser (UI
// goroutine; the chooser blocks, so it runs off it).
func (a *App) changeDownloadDir() {
	e := a.explorer
	if e == nil {
		a.Toast("This computer has no folder chooser " + AppName + " can open")
		return
	}
	go func() {
		dir, err := e.ChooseFolder()
		if err != nil || dir == "" {
			return // closed, or no chooser
		}
		a.Post(func() {
			a.prefs.DownloadPath = dir
			a.syncDownloadPrefs()
			a.savePrefs()
			a.Invalidate()
		})
	}()
}

// downloadLocationRows is the page's Download location group, as
// DownloadLocationPref lays it out: the box, the folder (a read-only
// field) with Change… beside it (greyed while the box is on), and Slack's
// note under them.
func (p *prefsModal) downloadLocationRows(f *prefsForm) {
	a := p.a // under the page's own "Download location" legend
	f.option("download_ask", "Choose location for each download", "", a.prefs.DownloadAsk, false, a.toggleDownloadAsk)
	f.fieldButton("download_dir", a.downloadDirShown(), prefsButton{"download_change", "Change…", !a.prefs.DownloadAsk, a.changeDownloadDir})
	f.muted("Note: this setting is for this computer only and affects all workspaces you are signed in to.", false)
}

// The download toast (popups.DownloadToast): drawn over everything at
// the bottom right while core's active list has files, redrawn as the
// downloads move (Downloads.Subscribe, off the UI goroutine).
type downloadToastState struct {
	toast popups.DownloadToast
	core  *core.App
	stop  func()
}

// layoutDownloadToast draws the download toast (app.go, beside the
// toasts).
func (a *App) layoutDownloadToast(gtx layout.Context) {
	c := a.Core()
	t := &a.dlToast
	if c != t.core {
		if t.stop != nil {
			t.stop()
			t.stop = nil
		}
		t.core = c
		if c != nil {
			t.stop = c.Downloads().Subscribe(a.Invalidate)
		}
	}
	if c == nil {
		return
	}
	t.toast.Layout(gtx, a)
}

// openDownloadsModal is ⌘⇧J's openDownloadsModal (View all downloads).
func (a *App) openDownloadsModal() {
	if a.Core() == nil {
		return
	}
	a.ShowOverlay(popups.NewDownloadsModal(a))
}
