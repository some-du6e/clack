package ui

import (
	"image"
	"strings"
)

// Shell actions: menus and commands reachable from the chrome.

func (a *App) openTeamMenu(anchor image.Point) {
	a.menu(anchor, []MenuItem{
		{Label: a.teamName(), Disabled: true},
		{Divider: true},
		{Label: "Preferences", Icon: "settings", Hint: "Ctrl+,", Do: a.openPrefs},
		{Label: "Toggle dark mode", Icon: "moon", Hint: "Ctrl+Shift+Y", Do: a.ToggleDark},
		{Divider: true},
		{Label: "All unreads", Icon: "unreads", Hint: "Ctrl+Shift+A", Do: func() { a.Navigate("unreads") }},
		{Label: "Browse channels", Icon: "hash", Do: func() { a.sidebarAction("add-channels") }},
		{Label: "Directories", Icon: "directories", Do: func() { a.Navigate("directories") }},
		{Divider: true},
		{Label: "Sign out of " + a.teamName(), Danger: true, Do: a.signOut},
	})
}

func (a *App) openSelfMenu(anchor image.Point) {
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
		{Label: "Preferences", Icon: "settings", Hint: "Ctrl+,", Do: a.openPrefs},
		{Divider: true},
		{Label: "Sign out", Danger: true, Do: a.signOut},
	})
}

func (a *App) openMoreMenu(anchor image.Point) {
	a.menu(anchor, []MenuItem{
		{Label: "Threads", Icon: "threads", Do: func() { a.Navigate("threads") }},
		{Label: "Huddles", Icon: "huddle", Do: func() { a.Navigate("huddles") }},
		{Label: "Drafts & sent", Icon: "drafts", Do: func() { a.Navigate("drafts") }},
		{Label: "Directories", Icon: "directories", Do: func() { a.Navigate("directories") }},
		{Label: "All unreads", Icon: "unreads", Hint: "Ctrl+Shift+A", Do: func() { a.Navigate("unreads") }},
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
		{Label: "Search in channel", Icon: "search", Do: func() { a.openSearch("in:<#" + ch + ">") }},
	}
	if conv != nil && (conv.Kind == "channel" || conv.Kind == "private") {
		items = append(items, MenuItem{Divider: true}, MenuItem{Label: "Leave channel", Danger: true, Do: func() {
			if a.data != nil {
				a.data.leave(ch)
			}
		}})
	}
	a.menu(image.Pt(anchor.X-a.px(260), anchor.Y+a.px(40)), items)
}

func (a *App) openNotifPrefs(ch string) {
	conv := a.model().conv(ch)
	muted := conv != nil && conv.Muted
	a.menu(image.Pt(a.px(600), a.px(90)), []MenuItem{
		{Label: "Notifications", Disabled: true},
		{Divider: true},
		{Label: map[bool]string{true: "✓ Muted", false: "Mute channel"}[muted], Icon: "bell-off", Do: func() {
			if a.data != nil {
				a.data.setMuted(ch, !muted)
			}
		}},
	})
}

func (a *App) openTab(ch, key string) {
	switch {
	case key == "pins":
		a.OpenDetails(ch)
	case strings.HasPrefix(key, "bm:"):
		if a.data != nil {
			a.data.openBookmark(ch, strings.TrimPrefix(key, "bm:"))
		}
	}
}

func (a *App) sidebarAction(key string) {
	switch key {
	case "add-channels":
		a.Navigate("directories?tab=channels")
	case "browse-people":
		a.Navigate("directories?tab=people")
	case "add-apps":
		a.Navigate("directories?tab=apps")
	}
}

func (a *App) compose() {
	a.openSwitcher()
}

func (a *App) openPrefs() {
	a.menu(image.Pt(a.px(300), a.px(90)), []MenuItem{
		{Label: "Preferences", Disabled: true},
		{Divider: true},
		{Label: map[bool]string{true: "✓ Dark mode", false: "Dark mode"}[a.th.Dark], Icon: "moon", Do: a.ToggleDark},
		{Label: map[bool]string{true: "✓ Desktop notifications", false: "Desktop notifications"}[a.notifyOn()], Icon: "bell", Do: a.toggleNotify},
		{Label: map[bool]string{true: "Show sidebar", false: "Hide sidebar"}[a.sidebar.hidden], Icon: "sidebar", Hint: "Ctrl+Shift+D", Do: func() { a.sidebar.hidden = !a.sidebar.hidden }},
	})
}

func (a *App) toggleStar(ch string) {
	if a.data != nil {
		a.data.toggleStar(ch)
	}
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
	a.Toast("Status editing arrives with ui-popups")
}

func (a *App) signOut() {
	if a.data != nil {
		a.data.signOut()
		a.data = nil
	}
	a.views = map[Route]View{}
	a.viewOrder = nil
	a.login = newLoginScreen()
	a.Invalidate()
}
