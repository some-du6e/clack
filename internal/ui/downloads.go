package ui

import (
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"

	"gioui.org/x/explorer"

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
	popups.AskDownload = a.askDownload
}

func (a *App) syncDownloadPrefs() {
	d := a.prefs.DownloadPath
	a.dl.dir.Store(&d)
	a.dl.ask.Store(a.prefs.DownloadAsk)
}

// askDownload saves data where the save dialog says, when "Choose
// location for each download" is on. asked is false when it's off or the
// platform has no dialog: the folder then. Off the UI goroutine.
func (a *App) askDownload(name string, data []byte) (string, bool, error) {
	e := a.explorer
	if !a.dl.ask.Load() || e == nil {
		return "", false, nil
	}
	w, err := e.CreateFile(name)
	switch {
	case errors.Is(err, explorer.ErrUserDecline):
		return "", true, popups.ErrDownloadCanceled
	case errors.Is(err, explorer.ErrNotAvailable):
		return "", false, nil
	case err != nil:
		return "", true, err
	}
	if _, err := w.Write(data); err != nil {
		w.Close()
		return "", true, err
	}
	if err := w.Close(); err != nil {
		return "", true, err
	}
	if f, ok := w.(*os.File); ok {
		return f.Name(), true, nil
	}
	return name, true, nil
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
