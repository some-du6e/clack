package ui

import (
	"image"
	"net/url"
	"strings"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/shortcut"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

// ---- shared Host methods (docs/NATIVE.md) ----

// Theme returns the shared theme.
func (a *App) Theme() *theme.Theme { return a.th }

// PrimaryChannel is the channel the primary view shows ("" when it isn't a
// channel): the thread header hides its channel name for it
// (messages.PrimaryHost).
func (a *App) PrimaryChannel() string {
	if r := a.router.Current(); r.Page() == "c" {
		return r.Channel()
	}
	return ""
}

// UserAttention is messages.AttentionHost: Slack's
// isUserAttentionOnThread (the window has the focus and no overlay is
// open) gates marking a thread read.
func (a *App) UserAttention() bool {
	if !a.focused {
		return false
	}
	for _, o := range a.overlays {
		if !o.Dismissed() {
			return false
		}
	}
	return true
}

// Images returns the shared image cache.
func (a *App) Images() *imgcache.Cache { return a.images }

// Navigate switches the primary view and pushes history.
func (a *App) Navigate(route string) {
	r := Route(route)
	if r == "" {
		return
	}
	if r.Page() == "search" {
		a.searchQuery = r.Query() // routes carry it URL-escaped (popups' search)
		if q, err := url.QueryUnescape(a.searchQuery); err == nil {
			a.searchQuery = q
		}
	}
	prev := a.router.Current()
	a.router.Push(r)
	a.switchAt, a.switchTo = time.Now(), r
	a.afterRoute(prev, r)
}

// openDefault picks the first route after boot: the last channel, else the
// first sidebar channel.
func (a *App) openDefault() {
	m := a.model()
	if a.lastChannel != "" && m.conv(a.lastChannel) != nil {
		a.Navigate("c/" + a.lastChannel)
		return
	}
	for _, s := range m.Sections {
		for _, id := range s.Rows {
			if c := m.conv(id); c != nil && c.Kind == "channel" {
				a.Navigate("c/" + id)
				return
			}
		}
	}
	a.Navigate("threads")
}

// Back goes back in history.
func (a *App) Back() {
	prev := a.router.Current()
	for {
		r, ok := a.router.Back()
		if !ok {
			return
		}
		// outside Simplified layout mode the history skips the BBM tab's
		// entries (B69+ getStackWithoutBBMEntries)
		if isBBMRoute(r) && !a.bbmOn() && a.router.CanBack() {
			continue
		}
		a.afterRoute(prev, r)
		return
	}
}

// Forward goes forward in history.
func (a *App) Forward() {
	prev := a.router.Current()
	for {
		r, ok := a.router.Forward()
		if !ok {
			return
		}
		if isBBMRoute(r) && !a.bbmOn() && a.router.CanForward() {
			continue
		}
		a.afterRoute(prev, r)
		return
	}
}

// onPlace, set by mount_features.go, feeds the search popover's Recent
// places (Slack's navigation history: conversations and pages).
var onPlace func(id, title, icon string)

func (a *App) afterRoute(prev, r Route) {
	a.slickRoute(r)
	a.bbmAfterRoute(prev, r) // Simplified layout mode's container (bbm.go)
	if onPlace != nil {
		switch p := r.Page(); {
		case r.Channel() != "":
			onPlace(r.Channel(), "", "")
		case p != "" && p != "search" && p != "c" && pageTitle(r) != "":
			onPlace("view:"+p, pageTitle(r), pageIcon(p))
		}
	}
	ch := r.Channel()
	a.trackPreviewRoute(prev, r)
	if a.data != nil && a.data.core != nil {
		a.data.core.SetSelected(ch) // Slack's currentlySelectedId: a previewed channel gets a sidebar row
		// maybeRecordRecentNavigationEvent (desktop_search_recents_write)
		if id := ch; id != "" || core.SlackPageIDs[r.Page()] != "" {
			if id == "" {
				id = core.SlackPageIDs[r.Page()]
			}
			a.data.core.RecordRecentNavigation(id, "")
		}
	}
	if prev.Channel() != ch {
		// leaving a channel closes its thread/details pane (Slack keeps the
		// pane only within the same conversation).
		if a.secondary.kind != secNone && a.secondary.channel != ch {
			w := a.secondary.width
			a.secondary = secondaryState{width: w}
		}
	}
	if ts := r.Thread(); ts != "" {
		a.OpenThread(ch, ts)
	}
	if ts := r.Message(); ts != "" {
		// a permalink: the channel scrolled to the message, flashed
		// (ChannelView.Highlight; a thread pane on ch stays open)
		if h, ok := a.convoFor(ch, "").list.(interface{ Highlight(string) }); ok {
			h.Highlight(ts)
		}
	}
	if ch != "" {
		a.lastChannel = ch
		if a.data != nil {
			a.data.opened(ch)
		}
	}
	if r.Page() != "search" {
		a.searchQuery = ""
	}
	a.savePrefsSoon()
	a.Invalidate()
}

// activeRail maps the current route to a rail tab.
func (a *App) activeRail() string {
	switch p := a.router.Current().Page(); p {
	case "dms":
		return "dms"
	case "activity":
		return "activity"
	case "files":
		return "files"
	case "later":
		return "later"
	case "more", "directories", "huddles", "workflows":
		if p == "more" {
			return "platform"
		}
	}
	return "home"
}

// railClick opens a rail tab; org is the button's window position.
func (a *App) railClick(key string, org image.Point) {
	switch key {
	case "home":
		if a.lastChannel != "" {
			a.Navigate("c/" + a.lastChannel)
		} else {
			a.Navigate("threads")
		}
	case "platform": // Agents & tools: Clack's tools menu stands in for Slack's page
		a.openMoreMenu(image.Pt(a.px(70), org.Y))
	case "browse":
		a.openRailMore(image.Pt(a.px(70), org.Y))
	default:
		a.Navigate(key)
	}
}

// openRailMore is Slack's More menu (BrowsePeekMenu): the tabs that are not
// in the rail, with their subtitles and tab-number shortcuts, then
// "Customize navigation bar".
func (a *App) openRailMore(anchor image.Point) {
	r := a.model().Rail
	items := []MenuItem{{Label: "More", Disabled: true}}
	for _, id := range a.railOverflow {
		t := a.railTabFor(id)
		hint := ""
		if i := indexOf(r.Rail, id); i >= 0 && i < 9 { // shortcut: position among the rail tabs
			// Slack: ^1 on macOS (Control), Ctrl Shift 1 elsewhere
			if shortcut.Mac() {
				hint = shortcut.Format("Ctrl+" + itoa(i+1))
			} else {
				hint = shortcut.Format("Ctrl+Shift+" + itoa(i+1))
			}
		}
		key := t.key
		items = append(items, MenuItem{Label: t.label, Subtitle: t.sub, Icon: t.icon, Hint: hint, Do: func() {
			a.railClick(key, anchor)
		}})
	}
	items = append(items, MenuItem{Divider: true},
		MenuItem{Label: "Customize navigation bar", Do: func() { a.openPrefsSection("navigation") }})
	a.menu(anchor, items)
}

func indexOf(list []string, s string) int {
	for i, v := range list {
		if v == s {
			return i
		}
	}
	return -1
}

func (a *App) railBadge(key string) (int, bool) {
	m := a.model()
	switch key {
	case "home":
		if uipref.Get().NoHomeDot { // home_unread_dot off: +oIJ hides Home's dot and badge
			return 0, false
		}
		return m.Rail.HomeBadge(m.HomeUnread, m.DMCount)
	case "dms":
		return m.DMCount, m.DMUnread
	case "activity":
		return m.ActivityCount, false
	case "browse": // yEMG: badges of what is under More
		n := 0
		for _, id := range a.railOverflow {
			switch railKey(id) {
			case "dms":
				if !(m.Rail.ActivityInbox && !m.Rail.DMsOptOut) && m.Rail.DMsInRail {
					n += m.DMCount
				}
			case "activity":
				n += m.ActivityCount
			}
		}
		return n, false
	}
	return 0, false
}

func (a *App) teamName() string {
	if n := a.model().TeamName; n != "" {
		return n
	}
	return AppName
}

func (a *App) teamIconURL() string { return a.model().TeamIcon }
func (a *App) selfAvatar() string  { return a.model().SelfAvatar }
func (a *App) selfName() string    { return a.model().SelfName }
func (a *App) selfActive() bool    { return a.model().SelfActive }

func (a *App) userAvatar(id string) string {
	if a.data == nil || id == "" { // "" = facepile placeholder
		return ""
	}
	return a.data.avatar(id)
}

// windowTitle mirrors Slack: "<conv> (Channel) - <Team> - N new items - Slack".
func (a *App) windowTitle() string {
	m := a.model()
	var b strings.Builder
	r := a.router.Current()
	if c := m.conv(r.Channel()); c != nil {
		b.WriteString(c.displayName())
		switch c.Kind {
		case "im", "app":
			b.WriteString(" (DM)")
		case "mpim":
			b.WriteString(" (Group DM)")
		default:
			b.WriteString(" (Channel)")
		}
		b.WriteString(" - ")
	} else if p := pageTitle(r); p != "" {
		b.WriteString(p)
		b.WriteString(" - ")
	}
	b.WriteString(a.teamName())
	if n := a.unreadItems(); n > 0 {
		b.WriteString(" - ")
		b.WriteString(itoa(n))
		if n == 1 {
			b.WriteString(" new item")
		} else {
			b.WriteString(" new items")
		}
	}
	if a.opts.Demo != "" {
		b.WriteString(" (Demo)")
	}
	b.WriteString(" - " + AppName)
	return b.String()
}

// unreadItems counts mentions + unread DMs (what Slack calls new items).
func (a *App) unreadItems() int {
	m := a.model()
	n := 0
	for _, c := range m.Convs {
		if c.Muted {
			continue
		}
		if c.Mentions > 0 {
			n += c.Mentions
		} else if c.Unread && (c.Kind == "im" || c.Kind == "mpim") {
			n++
		}
	}
	return n
}

// pageIcon is a page's Recent places icon.
func pageIcon(page string) string {
	switch page {
	case "later":
		return "todos"
	case "dms":
		return "dms"
	case "files":
		return "files"
	case "drafts":
		return "send"
	case "directories":
		return "directories"
	case "unreads":
		return "unreads"
	}
	return page
}

func pageTitle(r Route) string {
	switch r.Page() {
	case "activity":
		return "Activity"
	case "dms":
		return "DMs"
	case "later":
		return "To-dos"
	case "files":
		return "Files"
	case "threads":
		return "Threads"
	case "huddles":
		return "Huddles"
	case "drafts":
		return "Drafts & sent"
	case "directories":
		return "Directories"
	case "search":
		return "Search"
	case "saved":
		return "Saved"
	case "unreads":
		return "All unreads"
	}
	return ""
}

func (a *App) px(dp int) int {
	if a.win == nil {
		return dp
	}
	return int(float32(dp)*a.pxPerDp + .5)
}

// ---- popup entry points (Host contract); ui-popups provides the widgets ----

var (
	mkProfileCard func(a *App, anchor image.Point, userID string) Overlay
	mkMessageMenu func(a *App, anchor image.Point, m core.Message) Overlay
	mkLightbox    func(a *App, img richtext.Image, m core.Message) Overlay
)

// OpenEmojiPicker opens the emoji picker anchored at anchor.
func (a *App) OpenEmojiPicker(anchor image.Point, onPick func(name string)) {
	if mkEmojiPicker != nil {
		a.ShowOverlay(mkEmojiPicker(a, anchor, onPick))
		return
	}
	a.Toast("Emoji picker arrives with ui-popups")
}

// OpenProfile opens a user's profile card.
func (a *App) OpenProfile(anchor image.Point, userID string) {
	if mkProfileCard != nil {
		a.ShowOverlay(mkProfileCard(a, anchor, userID))
		return
	}
	a.OpenUserProfile(userID)
}

// OpenMessageMenu opens the "more actions" menu for a message.
func (a *App) OpenMessageMenu(anchor image.Point, m core.Message) {
	if mkMessageMenu != nil {
		a.ShowOverlay(mkMessageMenu(a, anchor, m))
		return
	}
	a.menu(anchor, []MenuItem{
		{Label: "Reply in thread", Icon: "threads", Do: func() { a.OpenThread(m.Channel, threadRoot(m)) }},
		{Label: "Copy link", Icon: "link", Do: func() {
			if a.data != nil {
				a.copyText(a.data.permalink(m.Channel, m.TS))
				a.Toast("Link copied")
			}
		}},
		{Label: "Copy text", Icon: "copy", Do: func() { a.copyText(m.Text) }},
	})
}

// OpenLightbox shows an image full-window.
func (a *App) OpenLightbox(img richtext.Image, m core.Message) {
	if mkLightbox != nil {
		a.ShowOverlay(mkLightbox(a, img, m))
		return
	}
	openURL(img.URL)
}

func threadRoot(m core.Message) string {
	if m.ThreadTS != "" {
		return m.ThreadTS
	}
	return m.TS
}
