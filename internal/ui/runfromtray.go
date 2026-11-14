package ui

import (
	"log"
	"strings"

	"gioui.org/app"

	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/slackc"
)

// Slack desktop's runFromTray (main.bundle.cjs 4.53.276): the setting
// defaults to true (@1104712), and MAIN_WINDOW_CLOSE (@1137077) answers
// the window's close event, unless the app is quitting, with
//
//	if (!runFromTray || no signed-in workspace) → quit
//	else preventDefault, EH (@1139607): leave full screen first, then hide()
//	then, the first time ever (hasRunFromTray), the tray balloon
//	RunFromTray (TrayHandler _X @1174664, @1180282): "Why didn’t Slack quit?"
//
// The tray icon's click (handleTrayClick @1178197 → FOREGROUND_APP →
// FOCUS_WINDOW's i6 @796332: show, focus) brings the window back:
// cmd/clack's tray calls Raise, which shows a hidden window (Gio patch 13).

// TrayIcon is the notification-area icon cmd/clack runs (desktop.Tray on
// Windows).
type TrayIcon interface {
	// Balloon shows a notification from the icon: Electron's
	// Tray.displayBalloon (NIF_INFO). Any goroutine.
	Balloon(title, content string) error
}

type trayRef struct{ t TrayIcon }

// SetTray tells the App a notification-area icon is running (nil: none),
// so closing the window keeps Clack running there (Preferences > Advanced
// > "Leave app running in notification area when the window is closed").
// A desktop.Tray also gets Slack's icon states, tooltip and menu
// (tray.go). Any goroutine.
func (a *App) SetTray(t TrayIcon) {
	if t == nil {
		a.tray.Store(nil)
		return
	}
	a.tray.Store(&trayRef{t})
	if _, ok := t.(trayShell); ok { // its icon, tooltip and menu (tray.go)
		a.Post(func() {
			a.updateTrayIcon(true)
			a.updateTrayMenu(true)
		})
	}
}

func (a *App) trayIcon() TrayIcon {
	if r := a.tray.Load(); r != nil {
		return r.t
	}
	return nil
}

// runFromTrayOn is the runFromTray setting (default on).
func (a *App) runFromTrayOn() bool { return !a.prefs.NoRunFromTray }

// setRunFromTray is DesktopPref's checkbox (prefs-dialog GpYL:
// cGU({name: "runFromTray", value})).
func (a *App) setRunFromTray(on bool) {
	a.prefs.NoRunFromTray = !on
	a.savePrefs()
	a.Invalidate()
}

// closeToTray handles a close request (ClosingEvent) the way
// MAIN_WINDOW_CLOSE does, reporting whether the window was hidden or
// minimized instead (the caller then aborts the close). With no tray icon
// there is nowhere to come back from, so the close quits, as it does on
// macOS and Linux (Clack has no icon there), unless the window only
// minimizes. UI goroutine.
//
// The win32 build's handler (@1149121) also reads
// win32.windowFlashBehavior: "always" is keepAppInTaskbar, which keeps
// Clack running even with runFromTray off and minimizes the window
// instead of hiding it ("minimizing to preserve taskbar flash", gP
// @1151399), then shows the WindowFlash balloon once
// (hasExplainedWindowFlash) in place of RunFromTray's.
func (a *App) closeToTray() bool {
	if a.quitting.Load() { // app.isQuitting: Quit Clack, File > Quit, an update's restart
		return false
	}
	t := a.trayIcon()
	keep := a.keepAppInTaskbar()
	if t == nil && !keep {
		return false
	}
	on := a.runFromTrayOn()
	if !on && !keep || a.data == nil || a.login != nil { // 0 === zT(workspaces)
		log.Printf("ui: main window closed, consider it a quit action (runFromTray %v, keepAppInTaskbar %v)", on, keep)
		return false
	}
	if keep {
		if !a.minimizeWindow() {
			return false
		}
		log.Print("ui: attempted to close the window, minimizing to preserve taskbar flash")
	} else {
		if a.winMode == app.Fullscreen && a.win != nil {
			log.Print("ui: attempted to close the window while fullscreen, exiting fullscreen")
			a.win.Option(app.Windowed.Option())
		}
		if !a.hideWindow() {
			return false
		}
		log.Print("ui: attempted to close the window, hiding due to run from tray")
	}
	a.focused = false // no ConfigEvent may come before the next notification
	var title, content string
	switch {
	case keep && !a.prefs.HasExplainedWindowFlash:
		a.prefs.HasExplainedWindowFlash = true
		a.savePrefs()
		title, content = windowFlashBalloon()
	case !keep && !a.prefs.HasRunFromTray:
		a.prefs.HasRunFromTray = true
		a.savePrefs()
		title, content = runFromTrayBalloon()
	}
	if title != "" && t != nil {
		go func() {
			if err := t.Balloon(title, content); err != nil {
				log.Printf("ui: tray balloon: %v", err)
			}
		}()
	}
	return true
}

// minimizeWindow is gP's minimize (BrowserWindow.minimize: SC_MINIMIZE).
func (a *App) minimizeWindow() bool {
	hwnd := a.taskbarHWND()
	if hwnd == 0 {
		return false
	}
	a.onWindowThread(func() { winMinimize(hwnd) })
	return true
}

// windowFlashBalloon is getBalloonText(WindowFlash) (@1181747), with
// Clack's name for Slack's.
func windowFlashBalloon() (title, content string) {
	ns := i18n.NS("desktop_browser")
	title = ns.T("Why didn’t Slack quit?")
	content = ns.T("Slack’s set to always flash when you get notifications. To change this, visit Preferences > Notifications.")
	return strings.ReplaceAll(title, "Slack", AppName), strings.ReplaceAll(content, "Slack", AppName)
}

// runFromTrayBalloon is TrayHandler.getBalloonText(RunFromTray), in the
// desktop's "desktop_browser" namespace (sn.BROWSER). Slack names itself
// in it; Clack says its own name.
func runFromTrayBalloon() (title, content string) {
	ns := i18n.NS("desktop_browser")
	title = ns.T("Why didn’t Slack quit?")
	content = ns.T("Slack’s set to stay running in the notification area. To change this, visit Preferences > Advanced.")
	return strings.ReplaceAll(title, "Slack", AppName), strings.ReplaceAll(content, "Slack", AppName)
}

func (a *App) hideWindow() bool {
	if a.hideHook != nil {
		return a.hideHook()
	}
	if a.win == nil {
		return false
	}
	return a.win.Hide()
}

// drainIdle runs what was posted on a window event that isn't a frame:
// Gio's wake-up after an Invalidate. A hidden or minimized window draws
// no frames, and posted work (the backend's events, notifications) would
// otherwise wait for the window to come back. UI goroutine.
func (a *App) drainIdle() {
	// Fonts still loading: the first frames drain. The loader's
	// Invalidate brings a wake-up when they're ready, and a window
	// already hidden or minimized draws no frame to take them, so take
	// them here (without, closing to the tray before they loaded left
	// posted work waiting until the window came back).
	if a.th.Shaper == nil && !a.takeShaper() {
		return
	}
	a.mu.Lock()
	n := len(a.pending)
	a.mu.Unlock()
	if n == 0 {
		return
	}
	slackc.LockFrame()
	a.drain()
	slackc.UnlockFrame()
}
