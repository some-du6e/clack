package ui

import (
	"runtime"
	"time"

	"gioui.org/io/event"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/menubar"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/shortcut"
)

// The macOS menu bar: Slack desktop's (4.53.276), whose menus come from two
// places. The Electron shell's AppMenu (app.asar, dist/main.bundle.cjs:
// buildSlackMenu, buildFileMenu, buildEditMenu, buildViewMenu,
// buildMacWindowItems, buildHelpMenu) and the web client's
// desktop-app-menu items (getStaticMenuItems, getWorkspaceMenuItems:
// Preferences, New Message, the Go menu, Hide Sidebar, Find, Search, Sign
// Out, the recent views in History). Slack sorts each menu by the items'
// order and puts a separator before every order that is a multiple of 10;
// the separators below are those. Items Clack has no feature for are left
// out: Check for Updates' auto-update states, Settings & Administration,
// Workspace, Reload, Force Reload, Toggle Privacy Mode, New Channel, New
// Canvas, Invite People, Workflow Builder, Downloads, Keyboard Navigation,
// What's New, Troubleshooting, Contact Us, Give Feedback, Developer.
//
// Shortcuts Clack's window already binds (keys.go) are Forward: a key
// press goes to Gio as before, so modals and focused widgets still decide;
// choosing the item with the mouse runs the same action.

// releasesPage is Clack's release notes (Help > Release Notes).
const releasesPage = "https://github.com/some-du6e/yoloslackgoclient/releases"

// helpCenter is Slack's Help Center (Help > Open Help Center).
const helpCenter = "https://slack.com/help"

const menuRecentMax = 6 // Slack lists six recent views

// macMenus is the menu bar model. Its actions run on the UI goroutine
// (Post) except Quit, which may not wait for a frame.
func (a *App) macMenus() []menubar.Menu {
	const C, S, O, Ct = menubar.Cmd, menubar.Shift, menubar.Option, menubar.Ctrl
	sep := menubar.Sep
	// on posts f to the UI goroutine; menubar runs it on a goroutine of
	// its own, never AppKit's main thread.
	on := func(f func()) func() { return func() { a.Post(f) } }
	nav := func(r string) func() { return on(func() { a.Navigate(r) }) }
	app := []menubar.Item{
		{ID: "menuitem-about-slack", Title: "About " + AppName, Role: "orderFrontStandardAboutPanel:"},
	}
	if a.opts.CheckUpdate != nil {
		// update_check.go: the banner if there's one, else a toast
		app = append(app, menubar.Item{ID: "menuitem-update-status", Title: "Check for Updates…", Do: on(a.checkUpdatesNotify)})
	}
	app = append(app,
		sep,
		menubar.Item{ID: "menuitem-preferences", Title: "Preferences…", Key: ",", Mods: C, Forward: true, Do: on(a.openPrefs)},
		sep,
		menubar.Item{ID: "menuitem-services", Title: "Services", Role: "services"},
		sep,
		menubar.Item{ID: "menuitem-hide-slack", Title: "Hide " + AppName, Key: "h", Mods: C, Role: "hide:"},
		menubar.Item{ID: "menuitem-hide-others", Title: "Hide Others", Key: "h", Mods: O | C, Role: "hideOtherApplications:"},
		menubar.Item{ID: "menuitem-show-all", Title: "Show All", Role: "unhideAllApplications:"},
		sep,
		menubar.Item{ID: "menuitem-sign-out", Title: "Sign Out", Sub: []menubar.Item{
			{ID: menuSignOutTeam, Title: "Sign Out", Do: on(a.signOut)}, // titled with the workspace (menuState)
		}},
		sep,
		menubar.Item{ID: "menuitem-quit", Title: "Quit " + AppName, Key: "q", Mods: C, Do: a.Quit},
	)
	return []menubar.Menu{
		{Title: AppName, Kind: "app", Items: app},
		{Title: "File", Items: []menubar.Item{
			{ID: "menuitem-new-message", Title: "New Message", Key: "n", Mods: C, Forward: true, Do: on(a.compose)},
			sep,
			{ID: "menuitem-file-close", Title: "Close", Key: "w", Mods: C, Role: "clackClose:"},
		}},
		{Title: "Edit", Items: []menubar.Item{
			{ID: "menuitem-undo", Title: "Undo", Key: "z", Mods: C, Role: "undo:"},
			{ID: "menuitem-redo", Title: "Redo", Key: "z", Mods: S | C, Role: "redo:"},
			sep,
			{ID: "menuitem-cut", Title: "Cut", Key: "x", Mods: C, Role: "cut:"},
			{ID: "menuitem-copy", Title: "Copy", Key: "c", Mods: C, Role: "copy:"},
			{ID: "menuitem-paste", Title: "Paste", Key: "v", Mods: C, Role: "paste:"},
			{ID: "menuitem-paste-as", Title: "Paste and Match Style", Key: "v", Mods: S | C, Role: "pasteAsPlainText:"},
			{ID: "menuitem-delete", Title: "Delete", Role: "delete:"},
			{ID: "menuitem-select-all", Title: "Select All", Key: "a", Mods: C, Role: "selectAll:"},
			sep,
			{ID: "menuitem-search", Title: "Search", Key: "g", Mods: C, Forward: true, Do: on(func() { a.openSearch("") })},
			sep,
			{ID: "menuitem-find", Title: "Find…", Key: "f", Mods: C, Forward: true, Do: on(a.findInConversation)},
		}},
		{Title: "View", Items: []menubar.Item{
			{ID: "menuitem-toggle-fullscreen", Title: "Toggle Full Screen", Key: "f", Mods: Ct | C, Role: "toggleFullScreen:"},
			sep,
			{ID: "menuitem-toggle-sidebar", Title: "Hide Sidebar", Key: "d", Mods: S | C, Forward: true, Do: on(a.toggleSidebar)},
			sep,
			{ID: "menuitem-zoom-reset", Title: "Actual Size", Key: "0", Mods: C, Forward: true, Do: on(func() { a.zoom(0) })},
			{ID: "menuitem-zoom-in", Title: "Zoom In", Key: "+", Mods: C, Forward: true, Do: on(func() { a.zoom(1) })},
			{ID: "menuitem-zoom-out", Title: "Zoom Out", Key: "-", Mods: C, Forward: true, Do: on(func() { a.zoom(-1) })},
		}},
		{Title: "Go", Items: []menubar.Item{
			{ID: "menuitem-switch-to-channel", Title: "Switch to Channel", Key: "k", Mods: C, Forward: true, Do: on(a.openSwitcher)},
			sep,
			{ID: "menuitem-all-unreads", Title: "All Unreads", Key: "a", Mods: S | C, Forward: true, Do: nav("unreads")},
			{ID: "menuitem-threads", Title: "Threads", Key: "t", Mods: S | C, Forward: true, Do: nav("threads")},
			{ID: "menuitem-all-dms", Title: "All DMs", Key: "k", Mods: S | C, Forward: true, Do: nav("dms")},
			{ID: "menuitem-drafts", Title: "Drafts & sent", Do: nav("drafts")},
			{ID: "menuitems-mentions-reactions", Title: "Activity", Key: "m", Mods: S | C, Forward: true, Do: nav("activity")},
			{ID: "menuitem-later", Title: "Later", Do: nav("later")},
			sep,
			{ID: "menuitems-channel-browser", Title: "Channel Browser", Key: "l", Mods: S | C, Forward: true, Do: nav(channelsRoute)},
			{ID: "menuitem-file-browser", Title: "File Browser", Do: nav("files")},
			{ID: "menuitem-people-user-groups", Title: "People & User Groups", Key: "e", Mods: S | C, Forward: true, Do: nav(peopleRoute)},
			{ID: "menuitem-apps", Title: "Workflows & Apps", Do: nav("directories?tab=apps")},
			sep,
			{ID: "menuitem-profile", Title: "Profile", Do: on(func() { a.OpenUserProfile(a.model().SelfID) })},
		}},
		{Title: "History", Items: []menubar.Item{
			{ID: "menuitem-navigate-back", Title: "Back", Key: "[", Mods: C, Forward: true, Do: on(a.Back)},
			{ID: "menuitem-navigate-forward", Title: "Forward", Key: "]", Mods: C, Forward: true, Do: on(a.Forward)},
			{Sep: true, Recent: true}, // then the recent views (menuState)
		}},
		{Title: "Window", Kind: "window", Items: []menubar.Item{
			{ID: "menuitem-window-minimize", Title: "Minimize", Key: "m", Mods: C, Role: "performMiniaturize:"},
			{ID: "menuitem-window-perform-zoom", Title: "Zoom", Role: "performZoom:"},
			sep,
			{ID: "menuitem-window-bring-front", Title: "Bring All to Front", Role: "arrangeInFront:"},
		}},
		{Title: "Help", Kind: "help", Items: []menubar.Item{
			{ID: "menuitem-keyboard-shortcut", Title: "Keyboard Shortcuts", Do: on(a.showShortcutsHelp)},
			{ID: "menuitem-open-help-center", Title: "Open Help Center", Do: func() { openURL(helpCenter) }},
			sep,
			{ID: "menuitem-release-notes", Title: "Release Notes", Do: func() { openURL(releasesPage) }},
			{ID: "menuitem-freeze-reports", Title: "Show Freeze Reports", Do: a.showFreezeReports},
			{ID: "menuitem-about-slack", Title: "About " + AppName, Role: "orderFrontStandardAboutPanel:"},
		}},
	}
}

const menuSignOutTeam = "menuitem-sign-out-team"

// menuWorkspaceOnly are the web client's items: Slack shows them only with
// a workspace open, Clack greys them out on the sign-in screen.
var menuWorkspaceOnly = map[string]bool{
	"menuitem-preferences": true, menuSignOutTeam: true, "menuitem-new-message": true,
	"menuitem-search": true, "menuitem-find": true, "menuitem-toggle-sidebar": true,
	"menuitem-switch-to-channel": true, "menuitem-all-unreads": true, "menuitem-threads": true,
	"menuitem-all-dms": true, "menuitem-drafts": true, "menuitems-mentions-reactions": true,
	"menuitem-later": true, "menuitems-channel-browser": true, "menuitem-file-browser": true,
	"menuitem-people-user-groups": true, "menuitem-apps": true, "menuitem-profile": true,
	"menuitem-navigate-back": true, "menuitem-navigate-forward": true,
}

// menuKey is what menuState shows; the state is rebuilt when it changes.
type menuKey struct {
	signedIn, back, fwd, sidebar bool
	team                         string
	cur                          Route
	depth, pos                   int
	tick                         int64 // conversation names load later: refresh every 2 s
}

// menuBar is the installed menu bar's state (macOS only).
type menuBar struct {
	on   bool
	last menuKey
}

// startMenuBar replaces Gio's app menu with macMenus (macOS).
func (a *App) startMenuBar() {
	if runtime.GOOS != "darwin" {
		return
	}
	a.mbar.on = true
	menubar.Install(a.macMenus(), func(route string) { a.Post(func() { a.Navigate(route) }) })
	a.syncMenuBar()
}

// syncMenuBar hands the menus their state: what is greyed out, the
// sidebar item's and Sign Out's titles, History's recent views. Once per
// frame, cheap unless something changed.
func (a *App) syncMenuBar() {
	if !a.mbar.on {
		return
	}
	k := menuKey{
		signedIn: a.login == nil && a.data != nil,
		back:     a.router.CanBack(), fwd: a.router.CanForward(),
		sidebar: a.sidebar.hidden, team: a.teamName(),
		cur: a.router.Current(), depth: len(a.router.stack), pos: a.router.pos,
		tick: time.Now().Unix() / 2,
	}
	if k == a.mbar.last {
		return
	}
	a.mbar.last = k
	menubar.SetState(a.menuState(k))
}

func (a *App) menuState(k menuKey) *menubar.State {
	s := &menubar.State{Disabled: map[string]bool{}, Titles: map[string]string{}}
	if !k.signedIn {
		for id := range menuWorkspaceOnly {
			s.Disabled[id] = true
		}
		return s
	}
	s.Disabled["menuitem-navigate-back"] = !k.back
	s.Disabled["menuitem-navigate-forward"] = !k.fwd
	if k.sidebar {
		s.Titles["menuitem-toggle-sidebar"] = "Show Sidebar"
	}
	if k.team != "" {
		s.Titles[menuSignOutTeam] = k.team
	}
	for _, r := range a.router.Recent(menuRecentMax + 1) {
		if r == k.cur || len(s.Recent) == menuRecentMax {
			continue
		}
		s.Recent = append(s.Recent, menubar.Recent{Title: a.routeLabel(r), Route: string(r)})
	}
	return s
}

// menuViewEvent tells the menus which view is the main window (macOS:
// menubar_view_darwin.go).
var menuViewEvent func(event.Event)

// Slack's People & User Groups (⇧⌘E) and Channel Browser (⇧⌘L).
const (
	peopleRoute   = "directories?tab=people"
	channelsRoute = "directories?tab=channels"
)

func (a *App) toggleSidebar() { a.sidebar.hidden = !a.sidebar.hidden }

// findInConversation is ⌘F: search in the open conversation, else search.
func (a *App) findInConversation() {
	if ch := a.router.Current().Channel(); ch != "" {
		a.openSearch("in:<#" + ch + ">")
	} else {
		a.openSearch("")
	}
}

// showShortcutsHelp is the top bar's help button and Help > Keyboard
// Shortcuts.
func (a *App) showShortcutsHelp() {
	f := shortcut.Format
	a.Toast("Keyboard shortcuts: " + f("Mod+K") + " switch, " + f("Mod+Shift+A") + " unreads, " +
		f("Alt+Up") + "/" + f("Alt+Down") + " channels, " + f("Mod+.") + " side pane")
}

// checkUpdatesNotify is Slack's "Check for Updates…" (Clack > on macOS,
// the workspace menu elsewhere): a check now, its answer in the banner
// or a toast. UI goroutine.
func (a *App) checkUpdatesNotify() { a.checkForUpdates(true) }

// zoomFactors are Slack's zoom levels -3…5 (the main process's fN: Actual
// Size is level 0).
var zoomFactors = [...]float32{0.7, 0.8, 0.9, 1, 1.1, 1.25, 1.5, 1.75, 2}

const zoomMin, zoomMax = -3, 5

// zoom steps the zoom level by d (Zoom In 1, Zoom Out -1); 0 resets it.
func (a *App) zoom(d int) {
	z := 0
	if d != 0 {
		z = min(max(a.prefs.Zoom+d, zoomMin), zoomMax)
	}
	if z == a.prefs.Zoom {
		return
	}
	a.prefs.Zoom = z
	a.savePrefs()
	a.Invalidate()
}

// zoomFactor is the current level's scale.
func (a *App) zoomFactor() float32 {
	return zoomFactors[min(max(a.prefs.Zoom, zoomMin), zoomMax)-zoomMin]
}
