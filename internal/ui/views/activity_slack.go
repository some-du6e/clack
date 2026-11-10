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

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/popups"
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

// actEntryKind is Gjwd getActivityIconType (gantry client-boot-render's
// build, the one Slack's shipping CSS pairs with): the item's icon kind.
// Saved reminders resolve without their channel, as the rows call it.
func actEntryKind(it *core.ActivityItem) string {
	switch {
	case actMentionType(it.Type) || it.Type == "code_review_requested":
		return "at_mention"
	case it.Type == "bot_dm" || it.Type == "bot_dm_bundle":
		return "app"
	case it.Type == "dm" || it.Type == "mpdm":
		return "dm"
	case it.Type == "thread_v2":
		return "thread"
	case actInviteType(it.Type) || it.Type == "shared_workspace_invite" || it.Type == "team_joiner_invite":
		return "invite"
	case it.Type == "message_reaction":
		return "reaction"
	case it.Type == "saved_reminder":
		switch it.ReminderItem {
		case "file":
			return "file"
		case "list_record", "list_record_field":
			return "list"
		case "canvas_section":
			return "canvas"
		}
		return "later"
	case it.Type == "channel":
		return "channel"
	case it.Type == "generic_system_alert" || it.Type == "prejoin_dm_welcome_party_alert":
		return "notification"
	}
	return "at_mention"
}

// actIconName is Gjwd _F: the Slack icon for an icon kind.
func actIconName(kind string) string {
	switch kind {
	case "at_mention":
		return "mentions"
	case "dm":
		return "direct-messages"
	case "app":
		return "apps"
	case "reaction":
		return "reaction"
	case "invite":
		return "paper-plane"
	case "thread":
		return "threads"
	case "channel":
		return "channel"
	case "list":
		return "lists"
	case "canvas":
		return "canvas"
	case "file":
		return "file"
	case "reminder":
		return "reminder"
	case "later":
		return "bookmark"
	case "notification":
		return "notifications"
	}
	return ""
}

// actStandaloneIcon is Gjwd PJ: the kinds whose icon leads the Detailed
// row's footer (ActivityDecoupledTypeIcon, 16px).
func actStandaloneIcon(kind string) bool {
	switch kind {
	case "thread", "at_mention", "invite", "list", "canvas", "file", "reminder", "later":
		return true
	}
	return false
}

// actFooterDest is 0S4w bi: the destinations the footer (and the Dense
// row's location) names.
func actFooterDest(d actDest) bool {
	switch d {
	case destChannel, destMPDM, destDM, destApp, destList, destInvite, destWorkspaceInvite, destFile:
		return true
	}
	return false
}

// destName is VeYL's destination label (M) with the layout revisions on:
// "Direct message", "App", "Invitation", "Workspace invitation", "To-do"
// or "Later", the group's or channel's name, the list's or file's title,
// the alert category.
func (v *Activity) destName(it *core.ActivityItem, dest actDest, title string) string {
	switch dest {
	case destDM:
		return "Direct message"
	case destApp:
		// rZx3 AN: a Slackbot DM, with slackbot_unread_v2 on
		if v.slackbotDM(it.Channel) && v.h.Core().Experiment("slackbot_unread_v2") == "on" {
			return "Slackbot"
		}
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
	case destFile:
		// FileName: the file's title from the files store (nothing until
		// it's known)
		return v.fileTitles[it.ReminderID]
	case destSystem:
		return actAlertLabel(it.Title)
	}
	return ""
}

// actFoot is the Detailed row's footer text (VeYL getFooterContent with
// the layout revisions on): plain text, then the destination in bold
// (activity_row_content__footer_name), then plain text.
type actFoot struct {
	pre, name, post string
	nameIcon        string // an icon inside the name (a list's lists icon)
}

func (f actFoot) text() string { return f.pre + f.name + f.post }

// footerText is getFooterContent: "Reminder from {source}" or "Reminder",
// "Mention in {channel}" (+ " thread"), "Thread in {channel}", the bare
// destination for reactions, posts, DMs and apps, "Mention in {list}" for
// list items, and "Invitation" / "Workspace invitation" unbolded. ok false
// is no text (a destination the footer doesn't name).
func (v *Activity) footerText(it *core.ActivityItem, dest actDest, title string) (actFoot, bool) {
	if !actFooterDest(dest) {
		return actFoot{}, false
	}
	g := v.destName(it, dest, title)
	var icon string
	switch dest {
	case destList:
		list, row, _ := actListRow(it)
		if list == "" || row == "" {
			return actFoot{}, false // getFooterContent: a list entry without an item says nothing
		}
		icon = "lists"
	case destFile:
		if g != "" {
			icon = "file" // FileName: the file icon, inline, before the title
		}
	}
	f := actFoot{name: g, nameIcon: icon}
	switch {
	case it.Type == "saved_reminder":
		if !actReminderSource(it.ReminderItem) {
			return actFoot{pre: "Reminder"}, true
		}
		f.pre = "Reminder from "
	case actMentionType(it.Type) || it.Type == "code_review_requested":
		f.pre = "Mention in "
		if it.ThreadTS != "" {
			f.post = " thread"
		}
	case it.Type == "thread_v2":
		f.pre = "Thread in "
	case it.Type == "message_reaction":
	case dest == destList || dest == destFile:
		f.pre = "Mention in "
	case dest == destChannel:
	case dest == destInvite || dest == destWorkspaceInvite:
		return actFoot{pre: g}, true // H, outside the bold span
	}
	return f, true
}

// location is the Dense row's location column (variant "footer" outside
// Detailed): the destination's label alone.
func (v *Activity) location(it *core.ActivityItem, dest actDest, title string) (text, icon string, ok bool) {
	if !actFooterDest(dest) {
		return "", "", false
	}
	if dest == destList {
		list, row, _ := actListRow(it)
		if list == "" || row == "" {
			return "", "", false
		}
		icon = "lists"
	}
	name := v.destName(it, dest, title)
	if dest == destFile && name != "" {
		icon = "file"
	}
	return name, icon, true
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

// actListRow is hliv ez: the list, item and column a list entry points
// at. Only these entries carry an item (the approval entries don't).
func actListRow(it *core.ActivityItem) (list, row, col string) {
	switch it.Type {
	case "list_record_assigned", "list_record_edited", "list_user_mentioned", "list_todo_notification":
		return it.ListID, it.ListRecord, it.ListColumn
	case "saved_reminder":
		if it.ReminderItem == "list_record" {
			return it.ListID, it.ListRecord, ""
		}
	}
	return it.ListID, "", ""
}

// actListBody is w4rX ListContent's sentence around Hi+d ListItemName:
// the item's title, "Untitled item" until it's known, after the
// list-item icon (not in "List update:"). ok is false when Slack renders
// nothing (no item).
type actListBody struct {
	pre, item, post string
	icon            bool
}

func (v *Activity) listBody(it *core.ActivityItem) (actListBody, bool) {
	list, row, colID := actListRow(it)
	if list == "" || row == "" {
		return actListBody{}, false
	}
	title, column := v.actListRecord(list, row, colID)
	b := actListBody{item: firstNonEmpty(title, "Untitled item"), icon: true}
	switch it.Type {
	case "list_user_mentioned":
		b.pre = "@mention \u2022 " + column + " \u2022 "
	case "list_record_edited":
		b.pre, b.post = "Edited ", ": "+column
	case "list_record_assigned":
		b.pre = "Assigned to you: "
	default:
		b.pre, b.icon = "List update: ", false
	}
	return b, true
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

// reactionSender is laKt ReactionSender with density "dense" (both
// rows pass it): the reactor, "{name} +{n}" when other people reacted to
// the message too. Everyone who reacted, minus the viewer and the reactor.
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
	// both rows pass density "dense" to the sender (U5s4), so Detailed
	// reads "{name} +{count}" too
	if n := len(actOthers(users, v.selfID(), main)); n > 0 {
		d.name += " +" + strconv.Itoa(n)
	}
	d.nameDense = d.name
}

// threadSender is wM7g ThreadSender: the reply's author, then the other
// participants (the root's reply_users, minus the viewer, the author and
// Slackbot): "{a} and {b}", "{a} & {n} others", or "{a} +{n}" in Dense.
func (v *Activity) threadSender(it *core.ActivityItem, d *actDesc, author string) {
	if author == "" || it.Root == nil {
		return
	}
	o := actOthers(it.Root.ReplyUsers, v.selfID(), author, "USLACKBOT")
	if len(o) > 0 { // density "dense" in both rows: "{name} +{count}"
		d.name += " +" + strconv.Itoa(len(o))
	}
	d.nameDense = d.name
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
	actBtnComplete   // xSv9 ActivityItemMarkCompleteButton (Dense)
	actBtnReschedule // Pck2 ActivityItemRescheduleButton
	actBtnCount
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
	case actBtnComplete:
		return "check", "Mark Complete"
	case actBtnReschedule:
		return "clock", "Reschedule"
	}
	return "ellipsis-vertical-filled", "More actions"
}

// hoverButtons is ActivityRowContentActions for an item: Mark Complete
// (Dense, an incomplete reminder: cZ "dense" "actions"), Reschedule (a
// reminder whose saved item exists and isn't completed), Mark as read
// while unread, Mark as unread once read (not for reactions), Clear (or,
// in Cleared items, Restore notification) when it has a key, More actions
// when it has a ts. A reminder about a deleted message has Clear alone
// (isReminderMessageDeleted).
func (v *Activity) hoverButtons(it *core.ActivityItem, unread bool) []actBtn {
	var out []actBtn
	if it.Type == "saved_reminder" {
		if it.ReminderItem == "message" && it.Msg != nil && it.Msg.Tombstone {
			if it.Key != "" {
				return []actBtn{actBtnClear}
			}
			return nil
		}
		if v.dense && v.reminderIncomplete(it) {
			out = append(out, actBtnComplete)
		}
		if _, ok := core.ReminderKey(it); ok {
			if s, _, gone := v.reminderSaved(it); !gone && (s == nil || !s.Completed()) {
				out = append(out, actBtnReschedule)
			}
		}
	}
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
// inbox (not the DMs tab; activity_inbox on), in Slack's groups (gantry
// boot-render wrqs, renderMenuPlaceholderItem):
//
//	mark_as_read / mark_as_unread, save_for_later, change_notifications
//	remind_submenu, notify_broadcast_mentions, toggle_notifications_replies,
//	  mute_conversation, move_to_section, toggle_vip_user, copy_link
//	summarize_thread
//	clear_all_before
//	open_in_home, open_in_split_view, open_in_new_window
//
// Left out because Clack can't carry them out (docs/ACTIVITY.md):
// summarize_thread (ACCESS_SLACK_AI_CHANNEL_THREAD_SUMMARIES, Slack AI),
// open_in_split_view and open_in_new_window (no split view or second
// window). The Copy ▸ submenu with Copy huddle link is the DMs tab's;
// the inbox has the message's Copy link.
func (v *Activity) itemMenu(ph popups.Host, it core.ActivityItem, key string, unread bool) []popups.MenuItem {
	c := v.h.Core()
	m := it.Msg
	ch, ts := it.Channel, it.TS
	if m != nil {
		ch, ts = firstNonEmpty(m.Channel, ch), m.TS
	}
	conv, _ := c.Conv(it.Channel)
	isDM := conv.Kind == "im" || strings.HasPrefix(it.Channel, "D") && conv.Kind == ""
	muted := it.Channel != "" && popups.IsMuted(ph, it.Channel)
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
	// change_notifications: channel posts and channel invites (ex), the
	// ChannelNotificationPrefMenu as a 320px submenu, its description the
	// channel's setting (getNotificationPrefDescription)
	chanItem := (it.Type == "internal_channel_invite" || it.Type == "external_channel_invite" || it.Type == "channel") && it.Channel != "" && !isDM
	if chanItem {
		g1 = append(g1, popups.MenuItem{Key: "change_notifications", Label: "Edit notifications", Icon: "notifications",
			Subtitle: v.notifyDescription(it.Channel, muted), Sub: popups.ChannelNotifyItems(ph, it.Channel)})
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
		// onAddReminder "Custom": the custom reminder dialog
		sub = append(sub, popups.MenuItem{Label: "Custom…", Do: func() { ph.ShowOverlay(popups.NewReminderModal(ph, ch, ts)) }})
		g2 = append(g2, popups.MenuItem{Key: "remind_submenu", Label: "Remind me about this", Sub: sub})
	}
	if it.Type == "at_channel" && it.Channel != "" && !muted {
		// notify_broadcast_mentions: the channel's suppress_at_channel
		suppressed := c.NotifyPrefs(it.Channel).SuppressAtChannel
		lbl := "Don\u2019t notify for @here and @channel"
		if suppressed {
			lbl = "Notify for @here and @channel"
		}
		chID := it.Channel
		g2 = append(g2, popups.MenuItem{Key: "notify_broadcast_mentions", Label: lbl, Do: func() {
			v.write("Couldn’t change notifications: ", func(ctx context.Context, c *core.App) error {
				return c.SetSuppressAtChannel(ctx, chID, !suppressed)
			}, func() {})
		}})
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
		if muted {
			lbl = "Unmute " + noun
		}
		g2 = append(g2, popups.MenuItem{Key: "mute_conversation", Label: lbl, Do: popups.ChannelMenuAction(ph, it.Channel, "toggle_mute")})
	}
	if chanItem && popups.MoveChannelItems != nil {
		// move_to_section: "Move channel" ▸ the sections with a custom
		// section, else "Move to new section" (Create a section)
		chID := it.Channel
		if items, custom := popups.MoveChannelItems(chID); custom {
			g2 = append(g2, popups.MenuItem{Key: "move_to_section", Label: "Move channel", Sub: items})
		} else if popups.CreateSectionWith != nil {
			g2 = append(g2, popups.MenuItem{Key: "move_to_section", Label: "Move to new section", Do: func() { popups.CreateSectionWith(chID) }})
		}
	}
	if vip, ok := v.vipItem(ph, it.Channel); ok {
		g2 = append(g2, vip)
	}
	if m != nil && ch != "" && ts != "" {
		thread := ""
		if m.ThreadTS != "" && m.ThreadTS != ts {
			thread = m.ThreadTS
		}
		link := popups.MessageLink(ph, ch, ts, thread, m.Permalink)
		g2 = append(g2, popups.MenuItem{Key: "copy_link", Label: "Copy link", DoCtx: func(gtx C) { popups.CopyText(gtx, link) }})
	}
	groups = append(groups, g2)

	if it.FeedTS != "" && !v.showCleared && !(v.searchOn && strings.TrimSpace(v.query.text()) != "") {
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

// notifyDescription is getNotificationPrefDescription (forceShowText):
// "Muted", else the channel's desktop setting ("All new posts", "Just
// mentions", "Nothing"); "" for the default.
func (v *Activity) notifyDescription(ch string, muted bool) string {
	if muted {
		return "Muted"
	}
	c := v.h.Core()
	d := c.NotifyPrefs(ch).Desktop
	if d == "" || d == "default" {
		d = c.Prefs().GlobalDesktop // getCalculatedDesktopNotifySetting
	}
	switch d {
	case "everything":
		return "All new posts"
	case "mentions_dms":
		return "Just mentions"
	case "nothing":
		return "Nothing"
	}
	return ""
}

// vipItem is toggle_vip_user (isU+ getToggleVipActionMaybe) for a DM: on
// a paid team, the DM's person when they can be a VIP (not a bot, not
// you, not deactivated): "Add to VIP" / "Remove from VIP" → users.priority
// add / remove, then "Added {name} to VIPs" / "Removed {name} from VIPs".
func (v *Activity) vipItem(ph popups.Host, ch string) (popups.MenuItem, bool) {
	c := v.h.Core()
	if !c.PaidTeam() || ch == "" {
		return popups.MenuItem{}, false
	}
	conv, ok := c.Conv(ch)
	if !ok || conv.Kind != "im" || conv.User == "" || conv.User == v.selfID() || conv.User == "USLACKBOT" {
		return popups.MenuItem{}, false
	}
	u, ok := v.user(conv.User)
	if !ok || u.IsBot || u.Deleted {
		return popups.MenuItem{}, false
	}
	uid, name := conv.User, firstNonEmpty(u.RealName, displayName(u))
	isVIP := false
	for _, id := range c.VIPs() {
		isVIP = isVIP || id == uid
	}
	if isVIP {
		return popups.MenuItem{Key: "toggle_vip_user", Label: "Remove from VIP", Do: func() {
			popups.ActDo(ph, "Removed "+name+" from VIPs", func(ctx context.Context, a *core.App) error { return a.RemoveVIP(ctx, uid) })
		}}, true
	}
	return popups.MenuItem{Key: "toggle_vip_user", Label: "Add to VIP", Do: func() {
		popups.ActDo(ph, "Added "+name+" to VIPs", func(ctx context.Context, a *core.App) error { return a.AddVIP(ctx, uid) })
	}}, true
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

// slackbotDM is s0AY D: the conversation is your DM with Slackbot.
func (v *Activity) slackbotDM(ch string) bool {
	if ch == "" {
		return false
	}
	c, ok := v.h.Core().Conv(ch)
	return ok && c.User == "USLACKBOT"
}
