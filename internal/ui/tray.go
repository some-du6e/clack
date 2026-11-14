package ui

import (
	"log"
	"net/url"
	"os"
	"runtime"
	"strings"
	"sync"

	"gioui.org/app"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/desktop"
	"github.com/some-du6e/clack/internal/i18n"
)

// The Windows shell around the window, as Slack desktop 4.53.276's win32
// main process has it (main.bundle.cjs from the win32 nupkg's app.asar):
//
//   - TrayHandler (class vA @1175804). syncState sums the teams' unreads
//     and unreadHighlights (at @909569: getDesktopUnreads' hasUnreads and
//     app badge count, core app_badge.go), and getTrayStateFromUnreadsInfo
//     (@1177993) picks the icon: highlight while there are highlights,
//     unread while anything is unread, rest otherwise, shown as
//     resources/slack-taskbar-<icon>.ico (setTrayIcon @1178674), with
//     getTooltipText's (@1178339) "You have N notifications", "You have
//     unread messages" or "No unread messages". update() sets the icon and
//     the tooltip only when they change and remakes the menu when the
//     workspaces, the update status, the locale or the launch settings
//     change. A click is FOREGROUND_APP (handleTrayClick @1179498).
//   - createTrayMenu (@1179887): each workspace (switch to it, then
//     FOREGROUND_APP) and a separator; &Launch Control ▸ (&Launch on Login,
//     a checkbox greyed when IT enforces it; a separator; Show Slack
//     &Immediately and Keep in &Background, radio items while launching
//     on login, greyed plain items otherwise, for hideOnStartup); &Open
//     Slackbot Mini (only with the DESKTOP_COMMAND_CENTER experiment,
//     which Clack doesn't have); the update item (cD.V0 @1411965, when
//     updates are supported); &Preferences (Ctrl+,) when signed in; a
//     separator; Troubleshooting (fP @1032970); a separator; &Quit
//     (Ctrl+Q).
//   - JumpListHandler (class v_ @1174233, createJumpList @1174947): at
//     start and when the workspaces or the locale change, the custom
//     categories Workspaces (up to 10, each "slack://open?team=<id>" with
//     the exe's icon, less the ones the user removed from the list) and
//     Tasks: "Move Slack to Current Display" (resources/screen.ico,
//     "slack://app/?action=reset-window").
//   - slack://app?action=… (j @755313, the app-actions epic @1077569):
//     quit is QUIT_APP and reset-window MOVE_WINDOW_TO_PRIMARY_SCREEN (u0
//     @988237): the window fitted into the display under the cursor,
//     centred there and maximized. reveal-logs and clear-cache-and-restart
//     have nothing in Clack to run.
//
// Clack differs where it has to: Preferences and the update item bring
// the window forward first, because Clack shows them inside the window
// (Slack's web client opens its Preferences there too; its update checks
// answer in native message boxes). Troubleshooting has Clack's items (the
// app menu's, appmenu.go).

// trayShell is the part of the tray icon the state drives: desktop.Tray.
type trayShell interface {
	SetImage(s desktop.TrayState)
	SetTooltip(s string)
	SetMenu(items []desktop.TrayMenuItem)
}

// The shell calls tray.go makes (tests swap them); trayAnyOS runs the tray
// and jump list logic on every OS (tests).
var (
	trayAnyOS            bool
	setJumpList          = desktop.SetJumpList
	jumpListRemoved      = desktop.JumpListRemoved
	moveToCurrentDisplay = desktop.MoveToCurrentDisplay
)

// trayOS reports whether this OS has Slack's tray and jump list.
func trayOS() bool { return runtime.GOOS == "windows" || trayAnyOS }

// The desktop shell's string namespaces (iP.u: MENU, GENERAL).
var (
	nsDesktopMenu    = i18n.NS("desktop_menu")
	nsDesktopGeneral = i18n.NS("desktop_general")
)

// appNamed is a shell string with Slack's name replaced by Clack's.
func appNamed(s string) string { return strings.ReplaceAll(s, "Slack", AppName) }

// TrayTooltip is getTooltipText for the desktop unreads: Slack's tray
// tooltip.
func TrayTooltip(highlights int, unreads bool) string {
	switch {
	case highlights > 0:
		return nsDesktopBrowser.T("You have {unreadHighlights,plural,=1{# notification}other{# notifications}}",
			"unreadHighlights", highlights)
	case unreads:
		return nsDesktopBrowser.T("You have unread messages")
	}
	return nsDesktopBrowser.T("No unread messages")
}

// trayState is getTrayStateFromUnreadsInfo: the icon for du, and its
// tooltip.
func trayState(du core.DesktopUnreads) (desktop.TrayState, string) {
	switch {
	case du.Count > 0:
		return desktop.TrayHighlight, TrayTooltip(du.Count, true)
	case du.HasUnreads:
		return desktop.TrayUnread, TrayTooltip(0, true)
	}
	return desktop.TrayRest, TrayTooltip(0, false)
}

// trayIconState is what the tray icon was last given.
type trayIconState struct {
	state desktop.TrayState
	tip   string
	set   bool
}

// updateTrayIcon is update()'s setTrayIcon and setToolTip: the icon and
// the tooltip for the unreads, each set when it changed (force: both, for
// a new tray). UI goroutine.
func (a *App) updateTrayIcon(force bool) {
	if !trayOS() {
		return
	}
	t, ok := a.trayIcon().(trayShell)
	if !ok {
		return
	}
	s, tip := trayState(a.desktopUnreads())
	last := &a.trayLast
	if force || !last.set || last.state != s {
		t.SetImage(s)
	}
	if force || !last.set || last.tip != tip {
		t.SetTooltip(tip)
	}
	*last = trayIconState{state: s, tip: tip, set: true}
}

// trayMenuKey is what the tray menu is made from: Slack remakes it when
// one of these changes.
type trayMenuKey struct {
	signedIn         bool
	team             string
	updates          bool // isUpdateSupported
	update           string
	updateOK         bool
	launch, launchOK bool
	showOnStartup    bool
	locale           string
	set              bool // a computed key, never the zero value
}

// syncTray brings the tray's menu, the jump list and a pending
// reset-window up to date: TrayHandler's and JumpListHandler's update().
// Every window event; only comparisons when nothing changed. UI goroutine.
func (a *App) syncTray() {
	if !trayOS() {
		return
	}
	if a.resetPending {
		a.resetWindow()
	}
	a.syncJumpList()
	a.updateTrayMenu(false)
}

// updateTrayMenu is createTrayMenu when its inputs changed (force: always,
// for a new tray).
func (a *App) updateTrayMenu(force bool) {
	if !trayOS() {
		return
	}
	t, ok := a.trayIcon().(trayShell)
	if !ok {
		return
	}
	k := a.trayMenuKeyNow()
	if !force && k == a.trayMenuLast {
		return
	}
	a.trayMenuLast = k
	t.SetMenu(a.trayMenu(k))
}

func (a *App) trayMenuKeyNow() trayMenuKey {
	k := trayMenuKey{signedIn: a.login == nil && a.data != nil, locale: i18n.Locale(), showOnStartup: a.prefs.ShowOnStartup, set: true}
	if k.signedIn {
		k.team = a.teamName()
	}
	if a.opts.CheckUpdate != nil {
		k.updates = true
		k.update, k.updateOK = a.updateMenuItem()
	}
	k.launch, k.launchOK = a.launchOnLogin()
	return k
}

// trayMenu is createTrayMenu's items for k. Their actions run on the
// tray's thread, so each hands off (Post, Raise and Quit are safe from
// any goroutine).
func (a *App) trayMenu(k trayMenuKey) []desktop.TrayMenuItem {
	ns := nsDesktopMenu
	sep := desktop.TrayMenuItem{Sep: true}
	post := func(f func()) func() { return func() { a.Post(f) } }
	var items []desktop.TrayMenuItem
	if k.signedIn {
		// tc(e.id), then FOREGROUND_APP: Clack's one workspace is the
		// selected one already
		items = append(items, desktop.TrayMenuItem{Label: k.team, Do: a.Raise}, sep)
	}
	if k.launchOK {
		launch, show := k.launch, k.showOnStartup
		items = append(items, desktop.TrayMenuItem{Label: ns.T("&Launch Control"), Sub: []desktop.TrayMenuItem{
			{Label: ns.T("&Launch on Login"), Check: true, Checked: launch, Do: post(func() { a.setLaunchFromTray(!launch) })},
			sep,
			{Label: appNamed(ns.T("Show Slack &Immediately")), Radio: launch, Checked: show, Disabled: !launch,
				Do: post(func() { a.setShowOnStartup(true) })},
			{Label: ns.T("Keep in &Background"), Radio: launch, Checked: !show, Disabled: !launch,
				Do: post(func() { a.setShowOnStartup(false) })},
		}})
	}
	if k.updates {
		label := k.update
		if label == updMenuCheck {
			label = "&Check for Updates…" // V0's label on Windows
		}
		items = append(items, desktop.TrayMenuItem{Label: ns.T(label), Disabled: !k.updateOK, Do: post(func() {
			if t, ok := a.updateMenuItem(); ok && t != updMenuRestart {
				a.Raise() // the answer shows in the window
			}
			a.updateMenuClick()
		})})
	}
	if k.signedIn {
		items = append(items, desktop.TrayMenuItem{Label: ns.T("&Preferences"), Accel: trayAccel(","), Do: func() {
			a.Raise()
			a.Post(a.openPrefs) // SHOW_WEBAPP_DIALOG(PREFERENCES)
		}})
	}
	items = append(items, sep, a.trayTroubleshooting(), sep,
		desktop.TrayMenuItem{Label: ns.T("&Quit"), Accel: trayAccel("Q"), Do: a.Quit})
	return items
}

// trayAccel is CommandOrControl+key as Windows menus show it.
func trayAccel(key string) string { return "Ctrl+" + key }

// trayTroubleshooting is fP's Troubleshooting submenu with the items
// Clack has: the app menu's (troubleshootingMenu), with fP's mnemonic
// on Show Logs in Explorer.
func (a *App) trayTroubleshooting() desktop.TrayMenuItem {
	on := func(f func()) func() { return func() { a.Post(f) } }
	m := a.troubleshootingMenu(on)
	sub := make([]desktop.TrayMenuItem, 0, len(m.Sub))
	for _, it := range m.Sub {
		if it.Sep || it.Do == nil {
			continue
		}
		label := it.Title // Clack's own items, but for the logs
		if it.ID == "menuitem-logs" && runtime.GOOS == "windows" {
			label = nsDesktopMenu.T("Show &Logs in Explorer")
		}
		do := it.Do
		sub = append(sub, desktop.TrayMenuItem{Label: label, Do: func() { go do() }}) // logs and freeze reports stat folders
	}
	return desktop.TrayMenuItem{Label: nsDesktopMenu.T(m.Title), Sub: sub}
}

// ---- Launch Control ----

// launchCache is Launch app on login as Clack last read or set it (the
// Run key, the autostart entry). Launch Control shows it; a change from
// the tray bumps gen, so an open Preferences dialog reads it again.
type launchCache struct {
	known, on bool
	gen       int
}

// launchOnLogin is launchOnStartup: whether Clack launches on login, and
// whether this OS has the setting. Read once; UI goroutine.
func (a *App) launchOnLogin() (on, ok bool) {
	if a.opts.LaunchOnLogin == nil || a.opts.SetLaunchOnLogin == nil {
		return false, false
	}
	if !a.launch.known {
		a.launch.on, a.launch.known = a.opts.LaunchOnLogin(), true
	}
	return a.launch.on, true
}

// setLaunchOnLogin is UPDATE_SETTINGS {launchOnStartup}: the shell's entry
// set or removed. UI goroutine.
func (a *App) setLaunchOnLogin(on bool) error {
	if a.opts.SetLaunchOnLogin == nil {
		return nil
	}
	if err := a.opts.SetLaunchOnLogin(on); err != nil {
		return err
	}
	a.launch = launchCache{known: true, on: on, gen: a.launch.gen + 1}
	return nil
}

// setLaunchFromTray is Launch on Login's click.
func (a *App) setLaunchFromTray(on bool) {
	if err := a.setLaunchOnLogin(on); err != nil {
		log.Printf("ui: launch on login: %v", err)
		a.Toast("Couldn’t change Launch app on login: " + err.Error())
	}
	a.updateTrayMenu(false)
	a.Invalidate()
}

// setShowOnStartup is Show Clack Immediately (true) or Keep in Background
// (false): UPDATE_SETTINGS {hideOnStartup: !show}.
func (a *App) setShowOnStartup(show bool) {
	if a.prefs.ShowOnStartup != show {
		a.prefs.ShowOnStartup = show
		a.savePrefs()
	}
	a.updateTrayMenu(false)
}

// startHidden is Slack's sf (@929699): Launch on Login started Clack
// (invokedOnStartup, cmd/clack's --startup) and Launch Control says Keep
// in Background (hideOnStartup, Slack's default), so the window stays in
// the notification area until the icon brings it out. Only with an icon
// to come back from.
func (a *App) startHidden() bool {
	return a.opts.InvokedOnStartup && !a.prefs.ShowOnStartup && a.trayIcon() != nil && trayOS()
}

// ---- the jump list ----

// jumpKey is what the jump list was made from: the workspace and the
// locale (JumpListHandler's update).
type jumpKey struct {
	teamID, team, locale string
	set                  bool
}

var jumpMu sync.Mutex // one jump list transaction at a time

// syncJumpList is createJumpList when the workspace or the locale
// changed. The COM work runs off the UI goroutine.
func (a *App) syncJumpList() {
	k := jumpKey{locale: i18n.Locale(), set: true}
	if a.login == nil && a.data != nil {
		k.teamID, k.team = a.noteTeam()
	}
	if k == a.jump {
		return
	}
	a.jump = k
	exe, err := os.Executable()
	if err != nil {
		log.Printf("ui: jump list: %v", err)
		return
	}
	set, removedOf := setJumpList, jumpListRemoved // as they are now (tests swap them)
	go func() {
		jumpMu.Lock()
		defer jumpMu.Unlock()
		removed, err := removedOf("") // getJumpListSettings
		if err != nil {
			log.Printf("ui: jump list: removed items: %v", err)
		}
		if err := set("", jumpListCategories(k, exe, removed)); err != nil {
			log.Printf("ui: Tried to create the jump list, but failed: %v", err)
		}
	}()
}

// screenIconIndex is the jump list task's icon in Clack.exe: icon group
// #2, assets/icon/screen.ico (packaging/windows/winres.json), Slack's
// resources/screen.ico.
const screenIconIndex = -2

// resetWindowLink and openTeamLink are the jump list's arguments.
const resetWindowLink = "slack://app/?action=reset-window"

func openTeamLink(team string) string { return "slack://open?team=" + team }

// jumpListCategories is createJumpList's [Workspaces, Tasks] for k, with
// exe as the program (Slack's process.execPath when packaged, no
// "cmd.exe /c start" prefix), less the workspace items whose arguments
// the user removed.
func jumpListCategories(k jumpKey, exe string, removed []desktop.JumpListTask) []desktop.JumpListCategory {
	var teams []desktop.JumpListTask
	if k.teamID != "" && k.team != "" {
		t := desktop.JumpListTask{Title: k.team, Description: k.team, Program: exe, Args: openTeamLink(k.teamID),
			IconPath: exe, IconIndex: 0}
		gone := false
		for _, r := range removed {
			if r.Args == t.Args {
				gone = true
			}
		}
		if !gone {
			teams = append(teams, t)
		}
	}
	tasks := []desktop.JumpListTask{{
		Title:       appNamed(nsDesktopGeneral.T("Move Slack to Current Display")),
		Description: appNamed(nsDesktopGeneral.T("Move the Slack window to the current display")),
		Program:     exe,
		Args:        resetWindowLink,
		IconPath:    exe,
		IconIndex:   screenIconIndex,
	}}
	return []desktop.JumpListCategory{
		{Name: nsDesktopGeneral.T("Workspaces"), Items: teams},
		{Name: nsDesktopGeneral.T("Tasks"), Items: tasks},
	}
}

// ---- slack://app actions ----

// appAction is j: the action of a slack://app link, lowercased ("" for
// any other link).
func appAction(uri string) string {
	u, err := url.Parse(uri)
	if err != nil || !strings.EqualFold(u.Scheme, "slack") || !strings.EqualFold(u.Host, "app") {
		return ""
	}
	return strings.ToLower(u.Query().Get("action"))
}

// runAppAction runs a slack://app action Clack has, reporting whether it
// had one: quit (QUIT_APP) and reset-window
// (MOVE_WINDOW_TO_PRIMARY_SCREEN). Any goroutine.
func (a *App) runAppAction(action string) bool {
	switch action {
	case "quit":
		a.Quit()
	case "reset-window":
		a.Post(a.resetWindow)
	default:
		return false
	}
	return true
}

// resetWindow is MOVE_WINDOW_TO_PRIMARY_SCREEN: the main window moved to
// the display under the cursor (Windows; Gio can't place a window
// elsewhere), then maximized. Before the window exists it waits for it
// (syncTray). UI goroutine.
func (a *App) resetWindow() {
	hwnd := a.taskbarHWND()
	if a.win == nil || trayOS() && hwnd == 0 {
		a.resetPending = true
		return
	}
	a.resetPending = false
	if hwnd != 0 {
		moved := false
		a.onWindowThread(func() { moved = moveToCurrentDisplay(hwnd) })
		if !moved {
			log.Print("ui: reset-window: couldn't move the window to the cursor's display")
		}
	}
	a.win.Option(app.Maximized.Option())
}
