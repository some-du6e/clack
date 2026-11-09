package ui

import (
	"context"
	"errors"
	"log"

	"gioui.org/io/system"
	"gioui.org/widget"
)

// Desktop integration the shell exposes to cmd/clack: OS deep links, raise
// and quit (single instance, tray), and the update phases (update_ui.go
// draws them the way Slack desktop does).

// OpenURL opens a slack:// link the OS handed Clack (its URL handler, or a
// second launch forwarding its argument). It goes through Slack's
// handleDeepLink thunk, so plugins see it like an in-app link, and lands
// in the shell's deep-link handler (UIHooks.DeepLink). Safe from any
// goroutine, also before the window or the workspace is up.
func (a *App) OpenURL(uri string) {
	a.Post(func() {
		a.dispatch("handleDeepLink", map[string]any{"uri": uri},
			func(ctx context.Context, args map[string]any) (any, error) {
				u := argStr(args, "uri")
				ok := a.openDeepLink(u)
				if !ok {
					log.Printf("ui: deep link not handled: %.80s", noQuery(u)) // the query can name a search or a message
				}
				return ok, nil
			})
	})
}

// openDeepLink is the shell's slack:// handler: it navigates (or opens a
// profile) on the UI goroutine and reports whether it understood uri.
func (a *App) openDeepLink(uri string) bool {
	route, user, ok := deepLinkRoute(uri)
	if ok {
		a.Post(func() {
			if user != "" {
				a.OpenUserProfile(user)
			} else {
				a.Navigate(route)
			}
		})
	}
	return ok
}

// Raise brings the window to the front (restoring it if minimized). Safe
// from any goroutine; it doesn't wait for a frame, which a minimized window
// may never get.
func (a *App) Raise() { a.perform(system.ActionRaise) }

// Quit closes the window; Run then returns. Safe from any goroutine.
func (a *App) Quit() { a.perform(system.ActionClose) }

func (a *App) perform(act system.Action) {
	if w := a.winLive.Load(); w != nil {
		w.Perform(act) // runs on the window's event loop
		return
	}
	a.Post(func() { a.win.Perform(act) }) // before Run: on the first frame
}

// ---- updates ----

// UpdateInfo describes a newer Clack release.
type UpdateInfo struct {
	Tag string // e.g. v0.3.1
	URL string // its GitHub release page
	// Release is the shell's own description of it (cmd/clack: the
	// desktop.Release), handed back to DownloadUpdate.
	Release any
}

// ErrUpdateManual (from Options.InstallUpdate): the package opened in the
// system's installer for the user to finish; Clack keeps running.
var ErrUpdateManual = errors.New("update opened in the system installer")

// updatePhase is where an update is. Slack's updateStatus (app.asar,
// dist/main.bundle.cjs: nt) has the same steps: update-available,
// downloading-update, update-downloaded, restart-to-apply.
type updatePhase int

const (
	updNone        updatePhase = iota
	updAvailable               // found, not downloaded (downloads off, or the download failed)
	updDownloading             // in the background: the menus say "Downloading Update"
	updReady                   // verified: the Help dot, "Restart to Apply Update"
	updInstalling              // after the user chose to restart
	updManual                  // the system installer is open
	updFailed                  // the install didn't work
)

type updateState struct {
	info  UpdateInfo
	phase updatePhase
	err   string
	// manual: a check the user asked for (Check now, Check for
	// Updates…) found it, so it downloads and shows even with "Check
	// for updates" off.
	manual bool
	dlErr  string             // the last download failed: why
	cancel context.CancelFunc // the running download
	check  updateCheckState   // update_check.go
	// card is Help's "Take action" card; keys its header's Keyboard
	// shortcuts button (update_ui.go).
	card, keys, close widget.Clickable
	// dialog is the "Restart and update Clack?" dialog, box the last
	// updater message box ("You’re all good") and boxTitle its title.
	dialog   *updateDialog
	box      Overlay
	boxTitle string
	// quit closes Clack after a successful InstallUpdate (tests replace it).
	quit func()
}

// updateFound takes a check's newer release (UI goroutine). manual: the
// user asked for the check, so the banner shows whatever the settings.
func (a *App) updateFound(info UpdateInfo, manual bool) {
	u := &a.update
	if info.Tag == u.info.Tag && u.phase != updAvailable && u.phase != updNone {
		if manual {
			u.manual = true
			a.Invalidate()
		}
		return // already downloading, ready or installing this one
	}
	if u.cancel != nil {
		u.cancel()
		u.cancel = nil
	}
	same := info.Tag == u.info.Tag
	if !same {
		u.dlErr = ""
	}
	u.manual = manual || u.manual && same
	u.info, u.phase, u.err = info, updAvailable, ""
	a.startUpdateDownload(false)
	a.Invalidate()
}

// startUpdateDownload fetches the available release in the background
// (UI goroutine): when downloads are on, or force (Preferences'
// Download button).
func (a *App) startUpdateDownload(force bool) {
	u := &a.update
	if u.phase != updAvailable || a.opts.DownloadUpdate == nil {
		return
	}
	if !force && (!a.updateDownloadsOn() || a.prefs.NoUpdateCheck && !u.manual) {
		return
	}
	info := u.info
	ctx, cancel := context.WithCancel(context.Background())
	u.phase, u.cancel = updDownloading, cancel
	go func() {
		err := a.opts.DownloadUpdate(ctx, info)
		a.Post(func() {
			if u.info.Tag != info.Tag || u.phase != updDownloading {
				return
			}
			u.cancel = nil
			cancel()
			if err != nil {
				log.Printf("ui: update download: %v", err)
				u.phase = updAvailable // the release page, as without downloads
				u.dlErr = err.Error()
				a.Invalidate()
				return
			}
			u.phase, u.dlErr = updReady, ""
			a.Invalidate()
		})
	}()
}

// installUpdate is the user's choice to restart into the update ("Restart
// Clack" in the update dialog, "Restart to Apply Update" in the menus,
// Preferences' "Install and restart"): the only way an update is
// installed.
func (a *App) installUpdate() {
	u := &a.update
	if u.phase != updReady && u.phase != updFailed || a.opts.InstallUpdate == nil {
		return
	}
	info := u.info
	u.phase, u.err = updInstalling, ""
	go func() {
		err := a.opts.InstallUpdate(info)
		a.Post(func() {
			switch {
			case err == nil:
				if u.quit != nil {
					u.quit()
				} else {
					a.Quit() // the helper starts the new version once Clack exits
				}
			case errors.Is(err, ErrUpdateManual):
				u.phase = updManual
				a.Toast("Finish installing " + AppName + " " + info.Tag + " in the window that opened, then restart " + AppName + ".")
			default:
				log.Printf("ui: update install: %v", err)
				u.phase, u.err = updFailed, err.Error()
				a.updateMessageBox("Couldn’t update "+AppName, u.err)
			}
			a.Invalidate()
		})
	}()
}

// updateChecksOn is Preferences > Advanced > "Check for updates"
// (automatic checks; Check now works either way).
func (a *App) updateChecksOn() bool { return !a.prefs.NoUpdateCheck }

func (a *App) toggleUpdateChecks() {
	a.prefs.NoUpdateCheck = !a.prefs.NoUpdateCheck
	a.updatesOff.Store(a.prefs.NoUpdateCheck)
	if a.prefs.NoUpdateCheck {
		a.stopUpdateDownload()
		a.update.info, a.update.phase, a.update.manual = UpdateInfo{}, updNone, false
	}
	a.savePrefs()
}

// updateDownloadsOn is Preferences > Advanced > "Download updates
// automatically" (on unless turned off).
func (a *App) updateDownloadsOn() bool { return !a.prefs.NoUpdateDownload }

func (a *App) toggleUpdateDownloads() {
	a.prefs.NoUpdateDownload = !a.prefs.NoUpdateDownload
	if a.prefs.NoUpdateDownload {
		a.stopUpdateDownload()
	} else {
		a.startUpdateDownload(false)
	}
	a.savePrefs()
}

// stopUpdateDownload cancels a running download (a finished one stays
// ready to install).
func (a *App) stopUpdateDownload() {
	u := &a.update
	if u.phase == updDownloading {
		if u.cancel != nil {
			u.cancel()
			u.cancel = nil
		}
		u.phase = updAvailable
	}
}
