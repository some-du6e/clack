package views

// Slack's per-item rules for Activity rows, read from the web client
// (build a84662d2, public CDN chunks, 2026-11-09). Module ids are the
// webapp build's (gantry-v2-shared-boot-async); see docs/ACTIVITY.md.
//
//   - destination   hliv gA (getActivityDestination), oVmm Ip
//   - context pill  aJri withPrefixes, variant "pill" (the grouped
//     Detailed row's entry_context: PillTypeIcon + text)
//   - pill icon     sGb0 Z1 (getActivityIconType) and _F (icon names)
//   - sender        wM7g ActivityRowContentSenderName (ReactionSender,
//     ThreadSender, InviteSenderName)
//   - body          w4rX ActivityRowContentDetails (InviteContent,
//     ReactionContent, ReminderMessage, ListContent, ...)
//   - unread count  hliv g_ / sGb0 oY
//   - hover actions Gh4i ActivityRowContentActions (l32C, CEdx, 9MTh)
//   - item menu     HGmO getActivityItemMenuTemplate (the ⋮ button and
//     the row's context menu)

import (
	"context"
	"image"
	"strconv"
	"strings"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/popups"
)

// actDest is Slack's activity destination type (oVmm Ip).
type actDest uint8

const (
	destUnknown actDest = iota
	destChannel
	destMPDM
	destDM
	destApp
	destList
	destInvite
	destWorkspaceInvite
	destFile
	destLater
	destSystem
)

func actIsList(t string) bool {
	switch t {
	case "list_record_assigned", "list_record_edited", "list_user_mentioned", "list_todo_notification",
		"list_approval_request", "list_approval_reviewed":
		return true
	}
	return false
}

// actMentionType is sGb0 X8: the mention entry types.
func actMentionType(t string) bool {
	return actIsMention(t) || t == "unjoined_channel_mention"
}

// actInviteType is sGb0 yo.
func actInviteType(t string) bool {
	switch t {
	case "internal_channel_invite", "quietly_added_to_channel", "external_channel_invite", "external_dm_invite":
		return true
	}
	return false
}

// actExternal is sGb0 bL: a Slack Connect invitation.
func actExternal(t string) bool { return t == "external_channel_invite" || t == "external_dm_invite" }

// actBundleType is sGb0 oY: the entries whose unread count is a number of
// messages (the rest count as one).
func actBundleType(t string) bool {
	switch t {
	case "bot_dm", "bot_dm_bundle", "channel", "dm", "mpdm", "thread_v2":
		return true
	}
	return false
}

// actReminderSource reports whether a saved reminder points at something
// Slack names in "Reminder from {source}" (aJri: message, file,
// canvas_section, list_record).
func actReminderSource(item string) bool {
	switch item {
	case "message", "file", "canvas_section", "list_record":
		return true
	}
	return false
}

// destOf is hliv gA for an item in a conversation of the given kind.
func (v *Activity) destOf(it *core.ActivityItem, kind string) actDest {
	switch {
	case actIsList(it.Type):
		return destList
	case it.Type == "generic_system_alert":
		return destSystem
	case it.Type == "saved_reminder":
		switch it.ReminderItem {
		case "reminder":
			return destLater
		case "file", "canvas_section":
			return destFile
		case "list_record":
			return destList
		case "message":
			if d := v.kindDest(it.Channel, kind); d != destUnknown {
				return d
			}
		}
		return destChannel
	}
	if v.appConv(it, kind) {
		return destApp
	}
	switch {
	case actInviteType(it.Type):
		return destInvite
	case it.Type == "team_joiner_invite" || it.Type == "prejoin_dm_welcome_party_alert":
		return destWorkspaceInvite
	}
	return v.kindDest(it.Channel, kind)
}

// kindDest is sGb0 KC (getDestinationTypeFromChannel).
func (v *Activity) kindDest(ch, kind string) actDest {
	switch kind {
	case "channel", "private":
		return destChannel
	case "im":
		return destDM
	case "mpim":
		return destMPDM
	}
	switch {
	case strings.HasPrefix(ch, "D"):
		return destDM
	case strings.HasPrefix(ch, "C"), strings.HasPrefix(ch, "G") && kind == "":
		return destChannel
	}
	return destUnknown
}

// appConv reports a DM with an app or bot (gA's App destination).
func (v *Activity) appConv(it *core.ActivityItem, kind string) bool {
	if it.Type == "bot_dm" || it.Type == "bot_dm_bundle" {
		return true
	}
	if kind != "im" && !strings.HasPrefix(it.Channel, "D") {
		return false
	}
	c, ok := v.h.Core().Conv(it.Channel)
	if !ok || c.User == "" {
		return false
	}
	if c.User == "USLACKBOT" {
		return true
	}
	u, ok := v.h.Core().User(c.User)
	return ok && u.IsBot
}

// actPillIcon is aJri PillTypeIcon: the Slack icon before the pill text
// ("" for reactions and channel posts).
func actPillIcon(it *core.ActivityItem) string {
	if it.Type == "message_reaction" || it.Type == "channel" {
		return ""
	}
	if it.Type == "saved_reminder" {
		return "reminder"
	}
	switch {
	case actMentionType(it.Type):
		return "mentions"
	case it.Type == "bot_dm" || it.Type == "bot_dm_bundle":
		return "apps"
	case it.Type == "dm" || it.Type == "mpdm":
		return "direct-messages"
	case it.Type == "thread_v2":
		return "threads"
	case actInviteType(it.Type) || it.Type == "shared_workspace_invite" || it.Type == "team_joiner_invite":
		return "paper-plane"
	case it.Type == "generic_system_alert" || it.Type == "prejoin_dm_welcome_party_alert":
		return "notifications"
	}
	return "mentions" // Z1's default
}

// destName is withPrefixes' {channel}: the destination as the pill names
// it (M in aJri).
func (v *Activity) destName(it *core.ActivityItem, dest actDest, title string) string {
	switch dest {
	case destDM:
		return "Direct Message"
	case destApp:
		return "App"
	case destInvite:
		return "Invitation"
	case destWorkspaceInvite:
		return "Workspace invitation"
	case destLater:
		if v.h.Core().Experiment("later_todos") == "on" {
			return "To-do"
		}
		return "Later"
	case destMPDM, destChannel:
		return title
	case destList:
		return v.actListTitle(it.ListID)
	case destSystem:
		return actAlertLabel(it.Title)
	}
	return ""
}

// pillText is the grouped row's context pill: withPrefixes with variant
// "pill" (the grouped flag on). The reaction pill is "Reacted" followed by
// the emoji (emoji true).
func (v *Activity) pillText(it *core.ActivityItem, dest actDest, title string) (text string, emoji bool) {
	if dest == destUnknown {
		return "", false
	}
	g := v.destName(it, dest, title)
	switch {
	case it.Type == "saved_reminder":
		if actReminderSource(it.ReminderItem) {
			return strings.TrimSpace("Reminder from " + g), false
		}
		return "Reminder", false
	case actMentionType(it.Type):
		if it.Type == "keyword" {
			return "Keyword in " + g, false
		}
		return "Mention in " + g, false
	case it.Type == "thread_v2":
		return "Thread in " + g, false
	case it.Type == "message_reaction":
		return "Reacted", true
	case (dest == destList || dest == destFile) && g != "":
		return "Mention in " + g, false
	}
	return g, false
}

// actListTitle is a loaded list's name ("" until it's loaded).
func (v *Activity) actListTitle(id string) string {
	if id == "" {
		return ""
	}
	if d := v.h.Core().CachedList(id); d != nil {
		return d.Info.Title
	}
	return ""
}

// actListRecord is a loaded list item's title and a column's name.
func (v *Activity) actListRecord(listID, recID, colID string) (title, column string) {
	d := v.h.Core().CachedList(listID)
	if d == nil {
		return "", ""
	}
	for i := range d.Meta.Schema {
		if c := &d.Meta.Schema[i]; c.ID == colID || c.Key == colID {
			column = c.Name
		}
	}
	p := d.Meta.Primary()
	for i := range d.Records {
		if r := &d.Records[i]; r.ID == recID && p != nil {
			title = r.Field(p.Key).PlainText()
		}
	}
	return title, column
}

// listBody is w4rX ListContent ("" when the list isn't loaded: Slack
// renders nothing without it either).
func (v *Activity) listBody(it *core.ActivityItem) string {
	if it.ListID == "" || it.ListRecord == "" {
		return ""
	}
	title, column := v.actListRecord(it.ListID, it.ListRecord, it.ListColumn)
	if title == "" {
		return ""
	}
	switch it.Type {
	case "list_user_mentioned":
		return "@mention • " + column + " • " + title
	case "list_record_edited":
		return "Edited " + title + ": " + column
	case "list_record_assigned":
		return "Assigned to you: " + title
	}
	return "List update: " + title
}

// actOthers lists ids without the ones in skip, keeping order and
// dropping repeats.
func actOthers(ids []string, skip ...string) []string {
	var out []string
	seen := map[string]bool{}
	for _, s := range skip {
		seen[s] = true
	}
	for _, id := range ids {
		if id != "" && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// reactionSender is wM7g ReactionSender: the reactor, with "& others"
// (Detailed) or "+{n}" (Dense) when other people reacted to the message
// too. Everyone who reacted, minus the viewer and the reactor.
func (v *Activity) reactionSender(it *core.ActivityItem, d *actDesc) {
	var users []string
	if m := it.Msg; m != nil {
		for _, r := range m.Reactions {
			users = append(users, r.Users...)
		}
	}
	main := ""
	if it.Reaction != nil {
		main = it.Reaction.User
	}
	if main == "" {
		if o := actOthers(users, v.selfID()); len(o) > 0 {
			main = o[0]
		}
	}
	if main == "" {
		return
	}
	d.name, d.avatar = v.userName(main), ""
	if u, ok := v.user(main); ok {
		d.avatar = u.Avatar
	}
	d.nameDense = d.name
	if n := len(actOthers(users, v.selfID(), main)); n > 0 {
		d.nameDense = d.name + " +" + strconv.Itoa(n)
		d.name += " & others"
	}
}

// threadSender is wM7g ThreadSender: the reply's author, then the other
// participants (the root's reply_users, minus the viewer, the author and
// Slackbot): "{a} and {b}", "{a} & {n} others", or "{a} +{n}" in Dense.
func (v *Activity) threadSender(it *core.ActivityItem, d *actDesc, author string) {
	if author == "" || it.Root == nil {
		return
	}
	o := actOthers(it.Root.ReplyUsers, v.selfID(), author, "USLACKBOT")
	d.nameDense = d.name
	switch len(o) {
	case 0:
		return
	case 1:
		d.name += " and " + v.userName(o[0])
	default:
		d.name += " & " + strconv.Itoa(len(o)) + " others"
	}
	d.nameDense += " +" + strconv.Itoa(len(o))
}

// actUnreadCount is the status badge's number (hliv g_): a bundle's
// unread count, 1 for anything else.
func actUnreadCount(it *core.ActivityItem) int {
	if actBundleType(it.Type) && it.Count > 0 {
		return it.Count
	}
	return 1
}

// ---- hover actions ----

// actBtn is one ActivityRowContentActions button.
type actBtn uint8

const (
	actBtnMarkRead actBtn = iota
	actBtnMarkUnread
	actBtnClear
	actBtnRestore
	actBtnMore
)

// actBtnIcon is the button's Slack icon and label (its tooltip and
// aria-label).
func actBtnIcon(b actBtn) (icon, label string) {
	switch b {
	case actBtnMarkRead:
		return "mark-as-read", "Mark as read"
	case actBtnMarkUnread:
		return "mark-as-unread", "Mark as unread"
	case actBtnClear:
		return "eraser", "Clear"
	case actBtnRestore:
		return "eraser-filled", "Restore notification"
	}
	return "ellipsis-vertical-filled", "More actions"
}

// hoverButtons is ActivityRowContentActions for an item: Mark as read
// while unread, Mark as unread once read (not for reactions), Clear (or,
// in Cleared items, Restore notification) when it has a key, More actions
// when it has a ts.
func (v *Activity) hoverButtons(it *core.ActivityItem, unread bool) []actBtn {
	var out []actBtn
	switch {
	case unread:
		out = append(out, actBtnMarkRead)
	case it.Type != "message_reaction":
		out = append(out, actBtnMarkUnread)
	}
	if it.Key != "" {
		if v.showCleared {
			out = append(out, actBtnRestore)
		} else {
			out = append(out, actBtnClear)
		}
	}
	if it.FeedTS != "" || it.TS != "" {
		out = append(out, actBtnMore)
	}
	return out
}

// ---- the item menu ----

// actMenuOpen opens an item's menu: getActivityItemMenuTemplate, the same
// template for the ⋮ button (origin overflow_menu) and the row's context
// menu. Without a popups host there is no menu.
func (v *Activity) actMenuOpen(at image.Point, it core.ActivityItem, key string, unread bool) {
	ph, ok := v.h.(popups.Host)
	if !ok {
		return
	}
	items := v.itemMenu(ph, it, key, unread)
	if len(items) == 0 {
		return
	}
	ph.ShowOverlay(popups.NewMenu(ph, at, items))
}

// itemMenu builds getActivityItemMenuTemplate's rows for the activity
// inbox (not the DMs tab), in Slack's groups:
//
//	mark_as_read / mark_as_unread, save_for_later
//	remind_submenu, toggle_notifications_replies, mute_conversation, copy_link
//	clear_all_before
//	open_in_home
//
// Rows Clack can't carry out are left out (see docs/ACTIVITY.md).
func (v *Activity) itemMenu(ph popups.Host, it core.ActivityItem, key string, unread bool) []popups.MenuItem {
	c := v.h.Core()
	m := it.Msg
	ch, ts := it.Channel, it.TS
	if m != nil {
		ch, ts = firstNonEmpty(m.Channel, ch), m.TS
	}
	var groups [][]popups.MenuItem

	var g1 []popups.MenuItem
	if unread {
		g1 = append(g1, popups.MenuItem{Key: "mark_as_read", Label: "Mark as read", Icon: "mark-as-read", Hint: "U", Keys: []string{"u"},
			Do: func() { v.markRead(&it, key) }})
	} else if it.Type != "message_reaction" {
		g1 = append(g1, popups.MenuItem{Key: "mark_as_unread", Label: "Mark as unread", Icon: "mark-unread", Hint: "U", Keys: []string{"u"},
			Do: func() { v.markUnread(&it, key) }})
	}
	if m != nil && ch != "" && ts != "" {
		saved := c.SavedState(ch, ts)
		if c.Experiment("later_todos") == "on" {
			lbl, icon := "Add to-do", "check-circle"
			if saved {
				lbl, icon = "Remove to-do", "check-circle-filled"
			}
			g1 = append(g1, popups.MenuItem{Key: "save_for_later", Label: lbl, Icon: icon, Hint: "A", Keys: []string{"a"},
				Do: func() {
					v.write("Couldn’t update to-dos: ", func(ctx context.Context, c *core.App) error {
						if saved {
							return c.Unsave(ctx, ch, ts)
						}
						return c.AddTodo(ctx, ch, ts, 0)
					}, func() {})
				}})
		} else {
			lbl, icon := "Save for later", "bookmark"
			if saved {
				lbl, icon = "Remove from Later", "bookmark-filled"
			}
			g1 = append(g1, popups.MenuItem{Key: "save_for_later", Label: lbl, Icon: icon, Hint: "A", Keys: []string{"a"},
				Do: func() {
					v.write("Couldn’t update Later: ", func(ctx context.Context, c *core.App) error {
						if saved {
							return c.Unsave(ctx, ch, ts)
						}
						return c.Save(ctx, ch, ts, 0)
					}, func() {})
				}})
		}
	}
	groups = append(groups, g1)

	var g2 []popups.MenuItem
	if m != nil && ch != "" && ts != "" {
		var sub []popups.MenuItem
		for _, p := range laterPresets {
			p := p
			sub = append(sub, popups.MenuItem{Label: p.label, Do: func() {
				due := p.at(time.Now()).Unix()
				v.write("Couldn’t set the reminder: ", func(ctx context.Context, c *core.App) error {
					return c.Save(ctx, ch, ts, due)
				}, func() {})
			}})
		}
		g2 = append(g2, popups.MenuItem{Key: "remind_submenu", Label: "Remind me about this", Sub: sub})
	}
	if it.Type == "thread_v2" && it.ThreadTS != "" && ch != "" {
		sub := it.Root != nil && it.Root.FollowingThread
		lbl := "Get notified about new replies"
		if sub {
			lbl = "Turn off notifications for replies"
		}
		thread := it.ThreadTS
		g2 = append(g2, popups.MenuItem{Key: "toggle_notifications_replies", Label: lbl, Do: func() {
			v.write("Couldn’t change thread notifications: ", func(ctx context.Context, c *core.App) error {
				return c.SubscribeThread(ctx, ch, thread, !sub)
			}, func() {})
		}})
	}
	if mute := it.Type == "at_user" || it.Type == "at_user_group" || it.Type == "internal_channel_invite" || it.Type == "external_channel_invite"; mute && it.Channel != "" && !(m != nil && m.User == "USLACKBOT") {
		noun := "channel"
		if k := c.ConvRef(it.Channel).Kind; isDMKind(k) || strings.HasPrefix(it.Channel, "D") {
			noun = "conversation"
		}
		lbl := "Mute " + noun
		if popups.IsMuted(ph, it.Channel) {
			lbl = "Unmute " + noun
		}
		g2 = append(g2, popups.MenuItem{Key: "mute_conversation", Label: lbl, Do: popups.ChannelMenuAction(ph, it.Channel, "toggle_mute")})
	}
	if m != nil && it.Channel != "" {
		g2 = append(g2, popups.MenuItem{Key: "copy_link", Label: "Copy", Sub: []popups.MenuItem{
			{Key: "copy_name", Label: "Copy name", DoCtx: func(gtx C) { popups.CopyText(gtx, v.copyName(it.Channel)) }},
			{Key: "copy_link", Label: "Copy link", DoCtx: func(gtx C) { popups.CopyText(gtx, popups.ChannelLink(ph, it.Channel)) }},
		}})
	}
	groups = append(groups, g2)

	if it.FeedTS != "" && !v.showCleared {
		before := it.FeedTS
		groups = append(groups, []popups.MenuItem{{Key: "clear_all_before", Label: "Clear all before…", Icon: "eraser",
			Do: func() { v.clearAllBefore(ph, before) }}})
	}
	if m != nil && it.Channel != "" {
		thread := actThread(&it)
		groups = append(groups, []popups.MenuItem{{Key: "open_in_home", Label: "Open in home", Icon: "home", Do: func() {
			if thread != "" {
				v.h.Navigate("c/" + it.Channel + "/t/" + thread)
				return
			}
			v.h.Navigate("c/" + it.Channel)
		}}})
	}

	var out []popups.MenuItem
	for _, g := range groups {
		if len(g) == 0 {
			continue
		}
		if len(out) > 0 {
			out = append(out, popups.Sep())
		}
		out = append(out, g...)
	}
	return out
}

// copyName is the conversation's name as Slack's Copy name copies it.
func (v *Activity) copyName(ch string) string {
	return strings.TrimPrefix(v.convTitle(ch, nil), "#")
}

// clearAllBefore is ClearAllBeforeDialog: "Clear all before this?", then
// activity.clearBefore with the view's filters.
func (v *Activity) clearAllBefore(ph popups.Host, before string) {
	f, filtered := v.clearFilters()
	body := "This will clear all notifications older than this one from your activity feed."
	if filtered {
		body = "This will clear all notifications matching your current filters that are older than this one."
	}
	ph.ShowOverlay(popups.NewModal(ph, popups.ModalOpts{
		Title:        "Clear all before this?",
		EnterSubmits: true,
		Body: func(gtx C) D {
			return v.para(gtx, 15, 22, wReg, v.p.Text, 0, body)
		},
		Buttons: []*popups.ModalButton{
			{Label: "Cancel", Kind: popups.BtnOutline},
			{Label: "Clear", Kind: popups.BtnPrimary, Do: func() bool {
				v.write("Couldn’t clear activity: ", func(ctx context.Context, c *core.App) error {
					if err := c.ClearActivityBefore(ctx, before, f); err != nil {
						return err
					}
					v.load(actRefresh)
					return nil
				}, func() {})
				return true
			}},
		},
	}))
}

// clearFilters is uby+ o: the view's filters as activity.clearBefore takes
// them (nil without any).
func (v *Activity) clearFilters() (*core.ActivityFilters, bool) {
	types, _ := v.types()
	f := core.ActivityFilters{UnreadOnly: v.unread.Value, ArchiveOnly: v.showCleared}
	if types != "" {
		f.EntryTypes = strings.Split(types, ",")
	}
	if len(f.EntryTypes) == 0 && !f.UnreadOnly && !f.ArchiveOnly {
		return nil, false
	}
	return &f, true
}
