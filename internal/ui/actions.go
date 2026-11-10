package ui

import (
	"context"
	"image"
	"strings"

	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/shortcut"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Shell actions: menus and commands reachable from the chrome.

func (a *App) openTeamMenu(anchor image.Point) {
	if mkTeamMenu != nil {
		a.ShowOverlay(mkTeamMenu(a, anchor))
		return
	}
	items := []MenuItem{
		{Label: a.teamName(), Disabled: true},
		{Divider: true},
		{Label: "Preferences", Icon: "settings", Hint: shortcut.Format("Mod+,"), Do: a.openPrefs},
	}
	if f := a.menuCheckForUpdates(); f != nil {
		label, ok := a.updateMenuItem()
		items = append(items, MenuItem{Label: label, Disabled: !ok, Do: f})
	}
	a.menu(anchor, append(items, []MenuItem{
		{Label: "Toggle dark mode", Icon: "moon", Do: a.ToggleDark},
		{Divider: true},
		{Label: "All unreads", Icon: "unreads", Hint: shortcut.Format("Mod+Shift+A"), Do: func() { a.Navigate("unreads") }},
		{Label: "Browse channels", Icon: "hash", Do: func() { a.Navigate("directories?tab=channels") }},
		{Label: "Directories", Icon: "directories", Do: func() { a.Navigate("directories") }},
		{Divider: true},
		{Label: "Sign out of " + a.teamName() + " on this device", Danger: true, Do: a.signOut},
	}...))
}

func (a *App) openSelfMenu(anchor image.Point) {
	if mkUserMenu != nil {
		a.ShowOverlay(mkUserMenu(a, anchor))
		return
	}
	m := a.model()
	away := "Set yourself as away"
	if !m.SelfActive {
		away = "Set yourself as active"
	}
	a.menu(image.Pt(anchor.X+a.px(8), anchor.Y-a.px(220)), []MenuItem{
		{Label: m.SelfName, Disabled: true},
		{Divider: true},
		{Label: "Update your status", Icon: "emoji", Do: func() { a.setStatusPrompt() }},
		{Label: away, Icon: "ring", Do: func() {
			if a.data != nil {
				a.data.togglePresence()
			}
		}},
		{Divider: true},
		{Label: "Profile", Icon: "user", Do: func() { a.OpenUserProfile(m.SelfID) }},
		{Label: "Preferences", Icon: "settings", Hint: shortcut.Format("Mod+,"), Do: a.openPrefs},
		{Divider: true},
		{Label: "Sign out on this device", Danger: true, Do: a.signOut},
	})
}

func (a *App) openMoreMenu(anchor image.Point) {
	a.menu(anchor, []MenuItem{
		{Label: "Threads", Icon: "threads", Do: func() { a.Navigate("threads") }},
		{Label: "Huddles", Icon: "huddle", Do: func() { a.Navigate("huddles") }},
		{Label: "Drafts & sent", Icon: "drafts", Do: func() { a.Navigate("drafts") }},
		{Label: "Directories", Icon: "directories", Do: func() { a.Navigate("directories") }},
		{Label: "All unreads", Icon: "unreads", Hint: shortcut.Format("Mod+Shift+A"), Do: func() { a.Navigate("unreads") }},
	})
}

func (a *App) openHistoryMenu() {
	var items []MenuItem
	for _, r := range a.router.Recent(12) {
		r := r
		items = append(items, MenuItem{Label: a.routeLabel(r), Icon: a.routeIcon(r), Do: func() { a.Navigate(string(r)) }})
	}
	if len(items) == 0 {
		items = []MenuItem{{Label: "No recent history", Disabled: true}}
	}
	a.menu(image.Pt(a.px(theme0RailW()), a.px(40)), items)
}

func theme0RailW() int { return 70 }

func (a *App) routeLabel(r Route) string {
	if c := a.model().conv(r.Channel()); c != nil {
		if c.Kind == "channel" || c.Kind == "private" {
			return "#" + c.displayName()
		}
		return c.displayName()
	}
	if t := pageTitle(r); t != "" {
		return t
	}
	return string(r)
}

func (a *App) routeIcon(r Route) string {
	if c := a.model().conv(r.Channel()); c != nil {
		switch c.Kind {
		case "private":
			return "lock"
		case "im", "mpim", "app":
			return "dms"
		}
		return "hash"
	}
	switch r.Page() {
	case "activity":
		return "activity"
	case "later":
		return "later"
	case "files":
		return "files"
	case "search":
		return "search"
	}
	return "history"
}

func (a *App) openChannelMenu(ch string, anchor image.Point) {
	if mkChannelMenu != nil {
		a.ShowOverlay(mkChannelMenu(a, anchor, ch))
		return
	}
	conv := a.model().conv(ch)
	muted := conv != nil && conv.Muted
	mute := "Mute conversation"
	if muted {
		mute = "Unmute conversation"
	}
	items := []MenuItem{
		{Label: "Open channel details", Icon: "info", Do: func() { a.OpenDetails(ch) }},
		{Label: "Copy link", Icon: "link", Do: func() { a.copyChannelLink(ch) }},
		{Divider: true},
		{Label: mute, Icon: "bell-off", Do: func() {
			if a.data != nil {
				a.data.setMuted(ch, !muted)
			}
		}},
		{Label: "Mark as read", Icon: "check", Hint: "Esc", Do: func() { a.markRead(ch) }},
		{Divider: true},
		{Label: "Search in channel", Icon: "search", Do: func() { a.searchInChannel(ch) }},
	}
	if conv != nil && (conv.Kind == "channel" || conv.Kind == "private") {
		items = append(items, MenuItem{Divider: true}, MenuItem{Label: "Leave channel", Danger: true, Do: func() {
			if a.data != nil {
				a.data.leave(ch)
			}
		}})
	}
	a.menu(image.Pt(anchor.X-a.px(300), anchor.Y+a.px(4)), items)
}

func (a *App) openNotifPrefs(ch string) {
	conv := a.model().conv(ch)
	if conv != nil && (conv.Kind == "channel" || conv.Kind == "private" || conv.Kind == "mpim") && a.Core() != nil {
		// Slack's ChannelNotificationPrefMenu, right-aligned like the ⋮
		// menu, 3px lower (y 77 vs 74)
		a.ShowOverlay(popups.NewChannelNotifyMenu(a, a.anchor(image.Pt(a.winSize.X-a.px(int(theme.PanelInset))-a.px(14)-a.px(6), a.px(int(theme.TopNavH))+a.px(37))), ch))
		return
	}
	// 1:1 DMs: the header's NotificationsControl is a plain mute toggle
	// ("Mute conversation" / "Unmute conversation", boot-shared vpVS).
	if conv != nil && a.data != nil {
		a.data.setMuted(ch, !conv.Muted)
	}
}

func (a *App) openTab(ch, key string) {
	switch {
	case key == "pins":
		a.OpenDetails(ch)
	case strings.HasPrefix(key, "bm:"):
		if a.data != nil {
			a.data.openBookmark(ch, strings.TrimPrefix(key, "bm:"))
		}
	case strings.HasPrefix(key, "tab:"):
		if a.data != nil {
			a.data.openTab(ch, key)
		}
	}
}

func (a *App) sidebarAction(key string) {
	switch key {
	case "add-channels": // Slack: a menu, Create a new channel / Browse channels
		a.openAddChannelsMenu(a.ptrWin)
	case "browse-people":
		a.Navigate("directories?tab=people")
	case "add-apps":
		a.Navigate("directories?tab=apps")
	}
}

func (a *App) compose() {
	a.openSwitcher()
}

func (a *App) markRead(ch string) {
	if a.data != nil && ch != "" {
		a.data.markRead(ch)
	}
}

func (a *App) copyChannelLink(ch string) {
	if a.data == nil {
		return
	}
	a.copyText(a.data.permalink(ch, ""))
	a.Toast("Link copied")
}

func (a *App) setStatusPrompt() {
	if mkStatusModal != nil {
		a.ShowOverlay(mkStatusModal(a, false))
		return
	}
	a.ShowOverlay(popups.NewSetStatusModal(a)) // the user menu's "Update your status"
}

// openStatusFromShortcut is Slack's mod+shift+y (PG1Q):
// openCustomStatusDialog({fromKeyboardShortcut: true}) first saves
// used_custom_status_kb_shortcut (setUserPrefByApi, which updates the
// local pref before the call, so the dialog it opens has no "Tip:" pill),
// unless it's set, then opens "Set a status".
func (a *App) openStatusFromShortcut() {
	if c := a.Core(); c != nil && !c.PrefBool("used_custom_status_kb_shortcut", false) {
		go func() { _ = a.writePref(context.Background(), "used_custom_status_kb_shortcut", true) }()
	}
	if mkStatusModal != nil {
		a.ShowOverlay(mkStatusModal(a, true))
		return
	}
	a.ShowOverlay(popups.NewSetStatusModalFromShortcut(a))
}

func (a *App) signOut() {
	if a.opts.SignOut != nil && a.data == nil {
		a.opts.SignOut() // deletes the stored credentials
	}
	if a.data != nil {
		a.data.signOut()
		a.data = nil
	}
	a.views = map[Route]View{}
	a.viewOrder = nil
	a.convos, a.convoOrder = nil, nil
	a.login = newLoginScreen()
	a.Toast("Signed out on this device. Your Slack session in other apps isn't affected.")
	a.Invalidate()
}
