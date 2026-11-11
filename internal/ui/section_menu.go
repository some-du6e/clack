package ui

import (
	"context"
	"image"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/popups"
)

// The sidebar section heading menus, ported from Slack's web client (build
// a84662d2d135, 2026-11-08; gantry-v2-async-client-boot-render, the
// helpers between getBrowsePageMenuItem and
// getSectionHeadingEllipsisMenuTemplate):
//
// The heading's hover ⋯ (o7 getSectionHeadingEllipsisMenuTemplate) is five
// groups, separators between the ones that have something to click
// (assembleMenuGroups):
//
//  1. the section's name (a heading); "Search in this section" (custom
//     and Starred, sidebar_section_search); "Mark all as read" (when
//     something in the section is unread); "Start direct message" (Direct
//     messages) or "Create a channel" (Channels, custom, Starred); "Share"
//     ▸ "Share directly" / "Copy section link" (custom sections with
//     conversations); the browse page: "Browse channels", "Recent direct
//     messages", "Browse apps".
//  2. "Manage section": "Rename" (custom), "Create a new section", "Manage
//     conversations" (custom and Starred,
//     add_channels_channel_sections_slackathon).
//  3. "Section settings": "Filter" ▸ and "Sort" ▸, each showing its value.
//     Filter under filter_dormant_channels: "Active only" ("New activity
//     within the last 30 days" with mute_channel_edu), "Unreads",
//     "Mentions" (not for Direct messages), "All"; without it "All",
//     "Unreads only", "Mentions only". Sort: "A–Z" ("Alphabetically"
//     without filter_dormant_channels), "Recency" ("By most recent"),
//     "Priority"; the value reads "A-Z", "Recency" or "Priority".
//  4. "Notifications": "Mute all conversations" / "Unmute all
//     conversations" (custom and Temporary channels).
//  5. "Delete section" (custom and Starred), "Leave all temporary
//     channels".
//
// Right-click (o2 getSectionHeadingContextMenuTemplate) keeps group 1
// (browse page only for Channels and Apps) and folds groups 2–5 into a
// "Manage section" ▸ submenu (with "Manage VIPs" under Section settings for
// Direct messages), then lists what the filter hides: "Unread
// conversations", "Read conversations", "Muted conversations" and
// "Inactive channels" ▸ ("No messages in at least 30 days").
//
// What each item calls is in core/section_menu.go. Toasts (Slack's
// strings): "Marked N channels as read.", "Muted N conversations.",
// "Deleted <section>.", "Renamed to <section>.", each with Undo; "Section
// link copied".

// mi makes a menu item pointer (nil = not offered).
func mi(it MenuItem) *MenuItem { return &it }

// assembleMenuGroups drops missing items and groups with nothing to click,
// and puts separators between the rest.
func assembleMenuGroups(groups ...[]*MenuItem) []MenuItem {
	var out []MenuItem
	for _, g := range groups {
		var keep []MenuItem
		actionable := false
		for _, it := range g {
			if it == nil {
				continue
			}
			keep = append(keep, *it)
			actionable = actionable || !it.Header
		}
		if !actionable {
			continue
		}
		if len(out) > 0 {
			out = append(out, MenuItem{Divider: true})
		}
		out = append(out, keep...)
	}
	return out
}

func (a *App) exp(name string) string {
	if c := a.Core(); c != nil {
		return c.Experiment(name)
	}
	return ""
}

func heading(s string) *MenuItem { return mi(MenuItem{Header: true, Label: s}) }

func custom(sec *sbSection) bool { return sec.Type == "standard" }

// online is Slack's isClientOnline gate (Rename, Create a new section and
// Delete section are disabled offline). Clack has no network-offline
// signal of its own (a session without the socket still polls), so signed
// in counts as online.
func (a *App) online() bool { return a.Core() != nil }

// ---- group 1 ----

func (a *App) searchInSectionItem(sec *sbSection) *MenuItem {
	if (sec.Type != "standard" && sec.Type != "stars") || a.exp("sidebar_section_search") != "on" {
		return nil
	}
	ids := append([]string(nil), sec.All...)
	return mi(MenuItem{Key: "channel_section_menu__search_in_section", Label: "Search in this section", Icon: "search", Do: func() {
		var q []string
		for _, id := range ids {
			q = append(q, "in:<#"+id+">")
		}
		a.openSearch(strings.Join(q, " ") + " ")
	}})
}

func (a *App) sectionUnread(sec *sbSection) []string {
	m := a.model()
	var out []string
	for _, id := range sec.All {
		if c := m.conv(id); c != nil && (c.Unread || c.Mentions > 0) {
			out = append(out, id)
		}
	}
	return out
}

func (a *App) markAllReadItem(sec *sbSection) *MenuItem {
	if len(a.sectionUnread(sec)) == 0 {
		return nil
	}
	id := sec.ID
	return mi(MenuItem{Key: "channel_section_menu_mark_as_read", Label: "Mark all as read", Icon: "mark-as-read", Do: func() { a.markSectionRead(id) }})
}

func (a *App) createConversationItem(sec *sbSection) *MenuItem {
	switch sec.Type {
	case "direct_messages":
		return mi(MenuItem{Label: "Start direct message", Icon: "direct-messages", Do: a.compose})
	case "channels", "standard", "stars", "user_group":
		id := sec.ID
		if sec.Type == "channels" {
			id = ""
		}
		return mi(MenuItem{Key: "channel_section_menu__create_channel", Label: "Create a channel", Icon: "channel", Do: func() { a.openCreateChannel(id) }})
	}
	return nil
}

func (a *App) shareItem(sec *sbSection) *MenuItem {
	if !custom(sec) || len(sec.All) == 0 {
		return nil
	}
	name, emoji, ids := sec.Name, sec.Emoji, append([]string(nil), sec.All...)
	return mi(MenuItem{Key: "section_menu_visual_updates_share", Label: "Share", Icon: "share-message", Sub: []MenuItem{
		{Label: "Share directly", Icon: "share-message", Subtitle: "Invite to public or private channels",
			Do: func() {
				a.ShowOverlay(popups.NewShareSectionModal(a, popups.ShareSectionOpts{Name: name, Emoji: emoji, Channels: ids}))
			}},
		{Label: "Copy section link", Icon: "link", Subtitle: "Private channels won’t be visible to people who aren’t already in them",
			Do: func() { a.copySectionLink(name, emoji, ids) }},
	}})
}

func (a *App) browsePageItem(sec *sbSection) *MenuItem {
	switch sec.Type {
	case "channels":
		return mi(MenuItem{Key: "channel_section_menu__channels__browse", Label: "Browse channels", Icon: "channel-search",
			Do: func() { a.Navigate("directories?tab=channels") }})
	case "direct_messages":
		return mi(MenuItem{Key: "channel_sidebar__recent_dms", Label: "Recent direct messages", Do: func() { a.Navigate("dms") }})
	case "recent_apps", "apps":
		return mi(MenuItem{Label: "Browse apps", Icon: "apps", Do: func() { a.Navigate("directories?tab=apps") }})
	}
	return nil
}

// ---- group 2 ----

func (a *App) renameItem(sec *sbSection) *MenuItem {
	if !custom(sec) {
		return nil
	}
	id, name, emoji := sec.ID, sec.Name, sec.Emoji
	return mi(MenuItem{Key: "channel_section_menu_rename_channel_section", Label: "Rename", Disabled: !a.online(),
		Do: func() { a.openRenameSection(id, name, emoji) }})
}

func (a *App) createSectionItem(sec *sbSection) *MenuItem {
	id := sec.ID
	return mi(MenuItem{Key: "channel_section_menu_create_new_section", Label: "Create a new section", Disabled: !a.online(),
		Do: func() { a.openCreateSection(id, "") }})
}

func (a *App) manageConversationsItem(sec *sbSection) *MenuItem {
	if (sec.Type != "standard" && sec.Type != "stars") || a.exp("add_channels_channel_sections_slackathon") != "on" {
		return nil
	}
	id := sec.ID
	return mi(MenuItem{Key: "channel_section_menu_manage_conversations", Label: "Manage conversations", Do: func() { a.openCreateSection("", id) }})
}

// ---- group 3: Filter and Sort ----

func windowLabel(f string) string {
	return map[string]string{"today": "1 day", "d7": "Last 7 days", "d30": "Last 30 days"}[f]
}

// filterStateLabel is getShowStateLabel (the Filter row's value).
func filterStateLabel(f string, dormant bool) string {
	if l := windowLabel(f); l != "" {
		return l
	}
	if !dormant && f == "active" {
		return "All"
	}
	switch f {
	case "hid":
		if dormant {
			return "Unreads"
		}
		return "Unreads only"
	case "mentions":
		if dormant {
			return "Mentions"
		}
		return "Mentions only"
	case "active":
		return "Active only"
	}
	return "All"
}

// filterLabel is getShowLabel (an item's label).
func filterLabel(f string, dormant bool) string {
	if l := windowLabel(f); l != "" {
		return l
	}
	switch f {
	case "all":
		return "All"
	case "active":
		return "Active only"
	}
	return filterStateLabel(f, dormant)
}

func (a *App) filterItem(sec *sbSection) *MenuItem {
	if sec.Type == "priority" {
		return nil
	}
	dormant := a.exp("filter_dormant_channels") == "on"
	muteEdu := a.exp("mute_channel_edu") == "on"
	dmHist := a.exp("programmable_sidebar_dm_history") == "on"
	isDM := sec.Type == "direct_messages"
	cur := sec.Filter
	id := sec.ID
	item := func(k string) MenuItem {
		it := MenuItem{Key: "channel_section_menu_sidebar_behavior_" + k, Label: filterLabel(k, dormant),
			Checked: cur == k || !dormant && cur == "active" && k == "all", Do: func() { a.setSectionPref(id, "sidebar", k) }}
		if k == "active" && dormant && muteEdu {
			it.Subtitle = "New activity within the last 30 days"
		}
		return it
	}
	var keys []string
	switch {
	case dmHist && isDM:
		keys = []string{"today", "d7", "d30", "hid"}
	case dormant:
		keys = []string{"active", "hid"}
		if !isDM {
			keys = append(keys, "mentions")
		}
		keys = append(keys, "all")
	default:
		keys = []string{"all", "hid"}
		if !isDM {
			keys = append(keys, "mentions")
		}
	}
	var sub []MenuItem
	for _, k := range keys {
		sub = append(sub, item(k))
	}
	return mi(MenuItem{Key: "channel_section_filter_submenu", Label: "Filter", Right: filterStateLabel(cur, dormant), Sub: sub})
}

func sortStateLabel(s string) string {
	switch s {
	case "priority":
		return "Priority"
	case "recent":
		return "Recency"
	}
	return "A-Z"
}

func (a *App) sortItem(sec *sbSection) *MenuItem {
	dormant := a.exp("filter_dormant_channels") == "on"
	g := a.exp("dm_recency_sort_rollout")
	dmRecency := g == "without_edu" || g == "with_edu"
	cur := sec.Sort
	id := sec.ID
	alpha := "Alphabetically"
	if dormant {
		alpha = "A–Z"
	}
	recent := "By most recent"
	if dmRecency && sec.Type == "direct_messages" {
		recent = "By most recent (Default)"
	}
	if dormant {
		recent = "Recency"
	}
	set := func(v string) func() { return func() { a.setSectionPref(id, "sort", v) } }
	return mi(MenuItem{Key: "channel_section_sort_submenu", Label: "Sort", Right: sortStateLabel(cur), Sub: []MenuItem{
		{Key: "channel_section_menu_sort_default", Label: alpha, Checked: cur == "" || cur == "default", Do: set("default")},
		{Key: "channel_section_menu_sort_recent", Label: recent, Checked: cur == "recent", Do: set("recent")},
		{Key: "channel_section_menu_sort_priority", Label: "Priority", Checked: cur == "priority", Do: set("priority")},
	}})
}

// ---- groups 4 and 5 ----

func (a *App) muteAllItem(sec *sbSection) *MenuItem {
	if sec.Type != "standard" && sec.Type != "user_group" && sec.Type != "temporary_channels" {
		return nil
	}
	m := a.model()
	ids := append([]string(nil), sec.All...)
	var live []*convInfo
	for _, id := range ids {
		if c := m.conv(id); c != nil && !c.Archived {
			live = append(live, c)
		}
	}
	if len(live) == 0 {
		return nil
	}
	someUnmuted := false
	for _, c := range live {
		someUnmuted = someUnmuted || !c.Muted
	}
	if someUnmuted {
		return mi(MenuItem{Key: "channel_section_menu_mute", Label: "Mute all conversations", Do: func() { a.muteSection(ids, true) }})
	}
	return mi(MenuItem{Key: "channel_section_menu_unmute", Label: "Unmute all conversations", Do: func() { a.muteSection(ids, false) }})
}

func (a *App) deleteSectionItem(sec *sbSection) *MenuItem {
	if sec.Type != "standard" && sec.Type != "stars" {
		return nil
	}
	id, name, n := sec.ID, sec.Name, len(sec.All)
	return mi(MenuItem{Key: "channel_section_menu_remove_channel_section", Label: "Delete section", Danger: true, Disabled: !a.online(),
		Do: func() {
			if n > 0 {
				a.ShowOverlay(popups.NewDeleteSectionModal(a, name, func() { a.deleteSection(id) }))
				return
			}
			a.deleteSection(id)
		}})
}

func (a *App) leaveTempsItem(sec *sbSection) *MenuItem {
	if sec.Type != "temporary_channels" || len(sec.All) == 0 {
		return nil
	}
	ids := append([]string(nil), sec.All...)
	return mi(MenuItem{Key: "temp_channels_menu_leave_all", Label: "Leave all temporary channels", Danger: true, Do: func() {
		a.sectionRun("Couldn’t leave the channels", func(ctx context.Context, c *core.App) error {
			for _, id := range ids {
				if err := c.Leave(ctx, id); err != nil {
					return err
				}
			}
			return nil
		}, nil)
	}})
}

// ---- the two templates ----

// sectionMenuItems is the heading's ⋯ menu (getSectionHeadingEllipsisMenuTemplate).
func (a *App) sectionMenuItems(sec *sbSection) []MenuItem {
	return assembleMenuGroups(
		[]*MenuItem{heading(sec.Name), a.searchInSectionItem(sec), a.markAllReadItem(sec), a.createConversationItem(sec),
			a.shareItem(sec), a.browsePageItem(sec)},
		[]*MenuItem{heading("Manage section"), a.renameItem(sec), a.createSectionItem(sec), a.manageConversationsItem(sec)},
		[]*MenuItem{heading("Section settings"), a.filterItem(sec), a.sortItem(sec)},
		[]*MenuItem{heading("Notifications"), a.muteAllItem(sec)},
		[]*MenuItem{a.deleteSectionItem(sec), a.leaveTempsItem(sec)},
	)
}

// sectionContextItems is the heading's right-click menu
// (getSectionHeadingContextMenuTemplate).
func (a *App) sectionContextItems(sec *sbSection) []MenuItem {
	g1 := []*MenuItem{heading(sec.Name), a.markAllReadItem(sec), a.createConversationItem(sec), a.shareItem(sec)}
	switch sec.Type {
	case "channels", "recent_apps", "apps", "slack_connect", "agents":
		g1 = append(g1, a.browsePageItem(sec))
	}
	var vip *MenuItem
	if sec.Type == "direct_messages" {
		vip = mi(MenuItem{Key: "manage_vip", Label: "Manage VIPs", Do: func() { a.openPrefsSection("vip") }})
	}
	manage := assembleMenuGroups(
		[]*MenuItem{a.renameItem(sec), a.createSectionItem(sec), a.manageConversationsItem(sec)},
		[]*MenuItem{heading("Section settings"), vip, a.filterItem(sec), a.sortItem(sec)},
		[]*MenuItem{heading("Notifications"), a.muteAllItem(sec)},
		[]*MenuItem{a.deleteSectionItem(sec), a.leaveTempsItem(sec)},
	)
	g1 = append(g1, mi(MenuItem{Key: "manage_section_submenu", Label: "Manage section", Sub: manage}))

	m := a.model()
	rows := func(header string, ids []string) []*MenuItem {
		if len(ids) == 0 {
			return nil
		}
		out := []*MenuItem{heading(header)}
		for _, id := range ids {
			c := m.conv(id)
			if c == nil {
				continue
			}
			id := id
			out = append(out, mi(MenuItem{Label: c.displayName(), Icon: convIcon(c), Do: func() { a.Navigate("c/" + id) }}))
		}
		return out
	}
	var unread []string
	collapsed := a.sidebar.collapsed[sec.ID]
	switch {
	case (sec.Filter == "mentions") && len(sec.HiddenUnread) > 0 && !collapsed:
		unread = sec.HiddenUnread
	case collapsed:
		for _, id := range a.sectionUnread(sec) {
			if c := m.conv(id); c != nil && !c.Muted {
				unread = append(unread, id)
			}
		}
	}
	var dormant []*MenuItem
	if a.exp("filter_dormant_channels") == "on" && len(sec.HiddenDormant) > 0 {
		dormant = []*MenuItem{mi(MenuItem{Key: "dormant_channels_submenu", Label: "Inactive channels",
			Sub: assembleMenuGroups(rows("No messages in at least 30 days", sec.HiddenDormant))})}
	}
	return assembleMenuGroups(g1, rows("Unread conversations", unread), rows("Read conversations", sec.HiddenRead),
		rows("Muted conversations", sec.HiddenMuted), dormant)
}

func convIcon(c *convInfo) string {
	switch c.Kind {
	case "private":
		return "lock"
	case "im", "mpim", "app":
		return "dms"
	}
	return "hash"
}

// openSectionMenu opens a heading's ⋯ menu at anchor.
func (a *App) openSectionMenu(id string, anchor image.Point) {
	if sec := a.sectionByID(id); sec != nil {
		if items := a.sectionMenuItems(sec); items != nil {
			a.menu(anchor, items)
		}
	}
}

// openSectionContextMenu is the heading's right-click menu.
func (a *App) openSectionContextMenu(id string, anchor image.Point) {
	if sec := a.sectionByID(id); sec != nil {
		if items := a.sectionContextItems(sec); items != nil {
			a.menu(anchor, items)
		}
	}
}

// ---- actions ----

// sectionRun runs a core write off the UI goroutine; fail is the toast on
// error, done (UI goroutine) runs on success.
func (a *App) sectionRun(fail string, work func(ctx context.Context, c *core.App) error, done func()) {
	c := a.Core()
	if c == nil {
		a.Toast("Sign in first")
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := work(ctx, c)
		a.Post(func() {
			if err != nil {
				a.Toast(fail + ": " + err.Error())
				return
			}
			if done != nil {
				done()
			}
		})
	}()
}

func (a *App) setSectionPref(id, pref, value string) {
	a.sectionRun("Couldn’t change the section", func(ctx context.Context, c *core.App) error {
		return c.SetSectionPref(ctx, id, pref, value)
	}, nil)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return strconv.Itoa(n) + " " + many
}

func (a *App) anyDM(ids []string) bool {
	m := a.model()
	for _, id := range ids {
		if c := m.conv(id); c != nil && (c.Kind == "im" || c.Kind == "mpim" || c.Kind == "app") {
			return true
		}
	}
	return false
}

// markSectionRead is "Mark all as read" (markChannelsAsRead + its toast).
func (a *App) markSectionRead(id string) {
	sec := a.sectionByID(id)
	if sec == nil {
		return
	}
	ids := a.sectionUnread(sec)
	dm := a.anyDM(ids)
	var prev map[string]string
	a.sectionRun("Couldn’t mark the section read", func(ctx context.Context, c *core.App) error {
		var err error
		prev, err = c.MarkManyRead(ctx, ids)
		return err
	}, func() {
		n := len(prev)
		what := plural(n, "1 channel", "channels")
		if dm {
			what = plural(n, "1 conversation", "conversations")
		}
		a.ToastUndo("Marked "+what+" as read.", func() {
			a.sectionRun("Couldn’t undo", func(ctx context.Context, c *core.App) error { return c.UndoMarkRead(ctx, prev) }, nil)
		})
	})
}

// muteSection is "Mute all conversations" / "Unmute all conversations"
// (performBulkMuteUnmuteChannels + its toast).
func (a *App) muteSection(ids []string, mute bool) {
	dm := a.anyDM(ids)
	a.sectionRun("Couldn’t change notifications", func(ctx context.Context, c *core.App) error {
		return c.MuteMany(ctx, ids, mute)
	}, func() {
		n := len(ids)
		what := plural(n, "channel", "channels")
		if dm {
			what = plural(n, "conversation", "conversations")
		}
		verb := "Muted "
		if !mute {
			verb = "Unmuted "
		}
		a.ToastUndo(verb+what+".", func() {
			a.sectionRun("Couldn’t undo", func(ctx context.Context, c *core.App) error { return c.MuteMany(ctx, ids, !mute) }, nil)
		})
	})
}

// deleteSection is performChannelSectionDelete: "Deleted <section>." with
// Undo for custom sections.
func (a *App) deleteSection(id string) {
	sec := a.sectionByID(id)
	name := ""
	if sec != nil {
		name = sec.Name
	}
	var d core.DeletedSection
	a.sectionRun("Couldn’t delete the section", func(ctx context.Context, c *core.App) error {
		var err error
		d, err = c.DeleteSection(ctx, id)
		return err
	}, func() {
		if d.Section.Type != "standard" {
			a.Toast("Deleted " + name + ".")
			return
		}
		a.ToastUndo("Deleted "+name+".", func() {
			a.sectionRun("Couldn’t undo", func(ctx context.Context, c *core.App) error {
				_, err := c.UndoDeleteSection(ctx, d)
				return err
			}, nil)
		})
	})
}

func (a *App) openRenameSection(id, name, emoji string) {
	m := popups.NewRenameSectionModal(a, id, name, emoji)
	m.OnRenamed = func(oldName, oldEmoji, newName string) {
		a.ToastUndo("Renamed to "+newName+".", func() {
			a.sectionRun("Couldn’t undo", func(ctx context.Context, c *core.App) error {
				return c.RenameSection(ctx, id, oldName, oldEmoji)
			}, nil)
		})
	}
	a.ShowOverlay(m)
}

// sidebarConvs is what the section and huddle pickers offer: the sidebar's
// conversations, each once, by name.
func (a *App) sidebarConvs() []popups.SectionConv {
	m := a.model()
	seen := map[string]bool{}
	var out []popups.SectionConv
	for _, s := range m.Sections {
		for _, id := range append(append([]string(nil), s.All...), s.Rows...) {
			c := m.conv(id)
			if c == nil || seen[id] || c.Archived || c.Preview {
				continue
			}
			seen[id] = true
			ic := convIcon(c)
			out = append(out, popups.SectionConv{ID: id, Name: c.displayName(), Icon: ic, DM: ic == "dms"})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

// openCreateSection opens "Create a section" placed before next, or with
// edit set, "Manage conversations" for that section.
func (a *App) openCreateSection(next, edit string) {
	if a.Core() == nil {
		a.Toast("Sign in first")
		return
	}
	o := popups.CreateSectionOpts{Next: next, Edit: edit, Candidates: a.sidebarConvs(), Moved: a.bulkMoveToast}
	switch s := a.Core().Prefs().ChannelSort; s {
	case "recent", "priority":
		o.DefaultSort = s
	default:
		o.DefaultSort = "default"
	}
	if sec := a.sectionByID(edit); edit != "" && sec != nil {
		o.EditName, o.Selected = sec.Name, append([]string(nil), sec.All...)
	}
	a.ShowOverlay(popups.NewCreateSectionModal(a, o))
}

func (a *App) copySectionLink(name, emoji string, ids []string) {
	c := a.Core()
	if c == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		u, err := c.SectionLink(ctx, name, emoji, ids)
		a.Post(func() {
			if err != nil {
				a.Toast("Unable to copy section link")
				return
			}
			a.copyText(u)
			a.Toast("Section link copied")
		})
	}()
}

// bulkMoveToast is performBulkChannelMove's toast (boot-shared eq1K,
// build 4d0c78085232) once a move went through, with Undo (moveManyChannels'
// reverse, core.UndoSectionMove).
func (a *App) bulkMoveToast(m core.SectionMove) {
	if m.Count == 0 {
		return
	}
	a.ToastUndo(a.bulkMoveText(m), func() {
		a.sectionRun("Couldn’t undo", func(ctx context.Context, c *core.App) error { return c.UndoSectionMove(ctx, m) }, nil)
	})
}

// bulkMoveText is the toast's text: for one channel "Moved #x to S." /
// "Removed #x from S." / "Moved #x."; for several conversations, or a DM,
// "Moved N conversations to S." (with manage_conversations_strings
// "Updated conversations in S."), "Removed N conversations from S." or
// "Moved N conversations.". S counts only when it is the one custom
// section or Starred moved into (out of).
func (a *App) bulkMoveText(m core.SectionMove) string {
	md := a.model()
	one := func(ids []string) *sbSection {
		if len(ids) != 1 {
			return nil
		}
		if s := a.sectionByID(ids[0]); s != nil && (s.Type == "standard" || s.Type == "stars") {
			return s
		}
		return nil
	}
	name := func(s *sbSection) string {
		if s.DefaultName {
			return i18n.T("channel_sidebar", s.Name)
		}
		return s.Name
	}
	to, from := one(m.InsertSections()), one(m.RemoveSections())
	var w *convInfo
	if chs := m.Channels(); len(chs) == 1 {
		w = md.conv(chs[0])
	}
	if w == nil || w.Kind == "im" || w.Kind == "mpim" || w.Kind == "app" {
		n := "conversation"
		if m.Count != 1 {
			n = strconv.Itoa(m.Count) + " conversations"
		}
		switch {
		case to != nil && a.exp("manage_conversations_strings") == "on":
			return "Updated conversations in " + name(to) + "."
		case to != nil:
			return "Moved " + n + " to " + name(to) + "."
		case from != nil:
			return "Removed " + n + " from " + name(from) + "."
		}
		return "Moved " + n + "."
	}
	ent := "#" + w.displayName()
	switch {
	case to != nil:
		return "Moved " + ent + " to " + name(to) + "."
	case from != nil:
		return "Removed " + ent + " from " + name(from) + "."
	}
	return "Moved " + ent + "."
}
