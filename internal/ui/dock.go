package ui

import (
	"log"
	"runtime"
	"strconv"

	"github.com/some-du6e/clack/internal/desktop"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

// The macOS Dock icon, as Slack's desktop app drives it from the web
// client's badge state (boot-render eow, the shell's app.dock):
//
//   - the badge is the app badge count (mentions and DMs) when there is
//     one; otherwise, with mac_ssb_bullet (default on), "•" while
//     anything is unread; otherwise nothing;
//   - each notification bounces the icon when mac_ssb_bounce is set:
//     "short" once, "long" until Clack is brought forward.
//
// The Windows taskbar button, as Slack desktop 4.53.276's win32 main
// process drives it (main.bundle.cjs from the win32 nupkg's app.asar):
//
//   - SET_BADGE_EPIC (@1171101): the overlay icon is the teams' summed
//     unreadHighlights (the same count as the Dock's badge, Clack's
//     HomeCount; 0 for slackbotStandalone, which Clack never is), set
//     when it changes and again on FOREGROUND_APP; vc puts Slack's
//     unread-badge image for it on the main window with the tooltip "You
//     have unread messages" (desktop_browser), and clears it at 0
//     (desktop.SetTaskbarOverlay);
//   - FLASH_WINDOW_ON_NOTIFICATION (@1168880): each NEW_NOTIFICATION, which
//     NOTIFY dispatches only when Windows shows the app's toasts
//     (checkNotificationState "notification display"), flashes the
//     frame per win32.windowFlashBehavior: "always" flashFrame(true);
//     "idle" (the default) only while systemIdleStatus is idle, first
//     showing a hidden window inactive and minimized so it has a taskbar
//     button to flash; "never" nothing;
//   - focusing the window (rj @806328) stops the flash, flashFrame(false).

// dockBadge and dockBounce are desktop's (tests swap them), and so are
// the taskbar's calls below.
var (
	dockBadge  = desktop.SetDockBadge
	dockBounce = desktop.BounceDock

	taskbarOverlay  = desktop.SetTaskbarOverlay
	frameFlash      = desktop.FlashFrame
	winVisible      = desktop.WindowVisible
	winShowInactive = desktop.ShowInactive
	winMinimize     = desktop.Minimize
	systemIdleTime  = desktop.SystemIdleTime
	toastsEnabled   = desktop.ToastsEnabled
)

// dockBadgeLabel is the badge for count highlights and unread state.
func dockBadgeLabel(count int, unread, bullet bool) string {
	switch {
	case count > 0:
		return strconv.Itoa(count)
	case unread && bullet:
		return "•"
	}
	return ""
}

// updateDock sets the badge from the model when it changed: the Dock's
// on macOS, the taskbar overlay on Windows.
func (a *App) updateDock() {
	if runtime.GOOS == "windows" || taskbarAnyOS {
		a.updateTaskbar(false)
	}
	if runtime.GOOS != "darwin" && !dockAnyOS {
		return
	}
	m := a.model()
	l := dockBadgeLabel(m.HomeCount, m.HomeUnread || m.DMUnread, !uipref.Get().NoMacBullet)
	if l == a.dock.label && a.dock.set {
		return
	}
	a.dock.label, a.dock.set = l, true
	dockBadge(l)
}

// bounceDock is a notification's bounce (MacAppearancePref).
func (a *App) bounceDock() {
	if runtime.GOOS != "darwin" && !dockAnyOS {
		return
	}
	switch uipref.Get().MacBounce {
	case "short":
		dockBounce(false)
	case "long":
		dockBounce(true)
	}
}

type dockState struct {
	label string
	set   bool

	// the overlay last set (Windows): its count and window
	tbCount int
	tbHWND  uintptr
	tbSet   bool
}

// dockAnyOS runs the Dock logic on every OS (tests); taskbarAnyOS the
// taskbar's, with taskbarHWNDHook as the window.
var (
	dockAnyOS       bool
	taskbarAnyOS    bool
	taskbarHWNDHook func() uintptr
)

// nsDesktopBrowser is the desktop shell's own strings (sn.BROWSER).
var nsDesktopBrowser = i18n.NS("desktop_browser")

// taskbarHWND is the main window's HWND (0 before Gio made it, and off
// Windows).
func (a *App) taskbarHWND() uintptr {
	if taskbarHWNDHook != nil {
		return taskbarHWNDHook()
	}
	return a.mainHWND()
}

// onWindowThread runs f on the window's thread (Window.Run), where the
// taskbar's COM object and the window calls belong; inline without a
// window (tests). UI goroutine, inside the event loop.
func (a *App) onWindowThread(f func()) {
	if a.win != nil && a.winLive.Load() != nil {
		a.win.Run(f)
		return
	}
	f()
}

// updateTaskbar is SET_BADGE_EPIC: the overlay for the count when it
// or the window changed (force: FOREGROUND_APP's unconditional set).
func (a *App) updateTaskbar(force bool) {
	hwnd := a.taskbarHWND()
	if hwnd == 0 {
		return
	}
	n := a.model().HomeCount
	d := &a.dock
	if !force && d.tbSet && d.tbHWND == hwnd && d.tbCount == n {
		return
	}
	d.tbSet, d.tbHWND, d.tbCount = true, hwnd, n
	desc := nsDesktopBrowser.T("You have unread messages")
	a.onWindowThread(func() {
		if err := taskbarOverlay(hwnd, n, desc); err != nil {
			log.Printf("ui: setWindowsOverlayIconFromBuffer: unexpected error occurred: %v", err)
		}
	})
}

// syncTaskbar puts the overlay on a new window (each window event: Gio's
// Win32ViewEvent gives the HWND after the badge may have been counted).
func (a *App) syncTaskbar() {
	if runtime.GOOS != "windows" && !taskbarAnyOS {
		return
	}
	if h := a.taskbarHWND(); h != 0 && h != a.dock.tbHWND {
		a.updateTaskbar(false)
	}
}

// foregrounded is what bringing Clack forward (Raise: the tray icon, a
// toast's click, another launch) does to the taskbar button:
// FOREGROUND_APP sets the badge again, and focusing the window (rj)
// stops a flash. UI goroutine.
func (a *App) foregrounded() {
	if runtime.GOOS != "windows" && !taskbarAnyOS {
		return
	}
	hwnd := a.taskbarHWND()
	if hwnd == 0 {
		return
	}
	a.onWindowThread(func() { frameFlash(hwnd, false) })
	a.updateTaskbar(true)
}

// The values of win32.windowFlashBehavior.
const (
	flashNever  = "never"
	flashIdle   = "idle"
	flashAlways = "always"
)

// windowFlashBehavior is win32.windowFlashBehavior ("idle" unset).
func (a *App) windowFlashBehavior() string {
	if v := a.prefs.WindowFlashBehavior; v != "" {
		return v
	}
	return flashIdle
}

// keepAppInTaskbar is MAIN_WINDOW_CLOSE's keepAppInTaskbar: on Windows
// (the only shell with win32 settings), "always" flashing makes a close
// minimize the window, so its taskbar button stays to flash.
func (a *App) keepAppInTaskbar() bool {
	return (runtime.GOOS == "windows" || taskbarAnyOS) && a.windowFlashBehavior() == flashAlways
}

// systemIdleSeconds is systemIdleStatusEpic's threshold: the system is
// idle once it has had no input for more than 20 seconds.
const systemIdleSeconds = 20

// notificationShown is NEW_NOTIFICATION for the shell's side of a
// notification Clack just showed: on Windows, once Windows says it shows
// Clack's toasts, the frame flash. Any goroutine.
func (a *App) notificationShown() {
	if runtime.GOOS != "windows" && !taskbarAnyOS {
		return
	}
	go func() {
		if toastsEnabled() {
			a.Post(a.flashOnNotification)
		}
	}()
}

// flashOnNotification is flashMainWindowFrame. Slack samples the idle
// state every 10 s (getSystemIdleTime() > 20 is idle); Clack reads it as
// the notification comes. UI goroutine.
func (a *App) flashOnNotification() {
	hwnd := a.taskbarHWND()
	if hwnd == 0 {
		return
	}
	b := a.windowFlashBehavior()
	idle := systemIdleTime() > systemIdleSeconds
	status := map[bool]string{false: "active", true: "idle"}[idle]
	flash := func(revived bool) {
		frameFlash(hwnd, true)
		log.Printf("ui: flashMainWindowFrame: flashed main window frame (windowFlashBehavior %s, systemIdleStatus %s, revivedHiddenWindow %v)", b, status, revived)
	}
	switch b {
	case flashAlways:
		a.onWindowThread(func() { flash(false) })
	case flashIdle:
		if !idle {
			return
		}
		a.onWindowThread(func() {
			revived := false
			if !winVisible(hwnd) { // hidden to the tray: give it a taskbar button
				winShowInactive(hwnd)
				winMinimize(hwnd)
				revived = true
			}
			flash(revived)
		})
	}
}

// macAppearance is Slack's MacAppearancePref (Sounds & appearance, macOS).
func (p *prefsModal) macAppearance(f *prefsForm) {
	bullet := p.prefBool("mac_ssb_bullet", true)
	f.option("pref.mac_ssb_bullet", "Show a badge on Slack’s icon to indicate new activity", "", bullet, false,
		func() { p.setPref("mac_ssb_bullet", !bullet) })
	r := p.prefStr("mac_ssb_bounce")
	if p.ra.bounce == "" { // useState(""===r?"short":r)
		p.ra.bounce = r
		if r == "" {
			p.ra.bounce = "short"
		}
	}
	l := p.ra.bounce
	f.option("pref.mac_ssb_bounce", "Bounce Slack’s icon when receiving a notification", "", r != "", false, func() {
		v := ""
		if r == "" {
			v = l
		}
		p.setPref("mac_ssb_bounce", v)
	})
	f.optionAt("pref.mac_ssb_bounce_short", "Only bounce it once", "", l == "short", false, r != "", func() {
		v := "long"
		if l != "short" {
			v = "short"
		}
		p.ra.bounce = v
		p.setPref("mac_ssb_bounce", v)
	})
}
