package popups

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/slack"
)

// Which message actions Slack offers on messages that aren't ordinary
// ones, from boot-shared build 83aff18a1847b8e9 (module ids renumber
// between builds: match them by export name). The hover toolbar is
// actions_MessageActions (shouldRenderActions) around MessageActions,
// whose buttons are the hover list (menuUsageLocation HOVER) and whose
// keys are the same actions' shortcuts (getActionsForKeyboardShortcuts),
// so a key does nothing where the button isn't; ⋮ opens
// MessageActionsMenu, and the right-click menu is the CONTEXT list (add
// reaction, thread, share, to-do) over the same menu.
//
// An ephemeral message ("Only visible to you", core.App.IsEphemeral)
// from the server has the toolbar with ⋮ alone:
//
//   - Add reaction and the quick reactions: canAddReaction needs
//     isActionable, which no ephemeral message is.
//   - Reply in thread: canStartOrOpenThread is false without replies, as
//     canReplyOrComment rejects isEphemeral.
//   - Forward / Share: canBeShared needs isActionable.
//   - Save for later and Add to-do: both return null on isEphemeral.
//   - Add to list: shouldShowAddToList needs !is_ephemeral.
//   - ⋮ More actions: always there (isOnlyDeletableHere is about
//     tombstones).
//
// Its menu keeps Copy message (canCopyMessageText has no ephemeral case),
// Delete message… (canDeleteMessageHelper answers true for any ephemeral
// message first: animateAndDeleteMessageApi then only animates it out and
// removes it here, with no chat.delete), and the workflow, flag and
// report rows by their own rules. It drops Mark unread (canMarkUnread),
// the save row, the reminder row (canAddReminder), the follow row
// (canStartOrOpenThread), Copy link (canCopyLink), Organize (pin:
// canPinMessage; the submenu itself needs isActionable) and Connect to
// apps (isActionable). Slack has no separate "Dismiss message" action:
// Delete message… is it.
//
// The relatives:
//
//   - a temporary message (isTempMsg: a pending or failed send, an
//     ephemeral this client made, core.App.IsTempMsg) has no toolbar and
//     so no keys (shouldRenderActions); a thread_hidden,
//     ekm_access_denied, dlp_tombstone or dsa_restricted message neither.
//   - a tombstone (a deleted root kept for its replies) has the toolbar:
//     its replies give it the thread action, and the to-do and add to
//     list buttons have no tombstone case; no reactions or share
//     (isActionable). ⋮ is there for a member (isOnlyDeletableHere hides
//     it from others), its menu without the reminder, Copy link, Copy
//     message, Organize and Connect to apps rows.
//   - an app's "Only visible to you" reply in a DM is an ephemeral
//     message like any other: the label isn't drawn there
//     (MessageLabels), but the actions follow is_ephemeral alone.
//   - plugin message actions (MAMenu) get the menu's props whatever the
//     message: Slack's MAMenu filters nothing, so a plugin's rows follow
//     its own rules.

// isReplyMsg is SPMg isReply: a thread_ts that isn't the message's own ts.
func isReplyMsg(m *core.Message) bool { return m.ThreadTS != "" && m.ThreadTS != m.TS }

// hasRepliesMsg is the thread action's hasReplies: isThreadBroadcast or a
// reply count.
func hasRepliesMsg(m *core.Message) bool { return m.Subtype == "thread_broadcast" || m.ReplyCount > 0 }

// CanReplyOrComment is canReplyOrComment (EJBA) for a channel message, as
// far as Clack knows the channel (ignoreMembership and
// ignoreImWithDeletedMmbr false, the thread action's call).
func CanReplyOrComment(app *core.App, m *core.Message) bool { return canReplyOrComment(app, m, false) }

// canReplyOrComment is CanReplyOrComment with its ignoreMembership.
func canReplyOrComment(app *core.App, m *core.Message, ignoreMembership bool) bool {
	sub := m.Subtype
	if sub == "welcome_party" {
		return true
	}
	actionableCode := sub == "code_channel_lifecycle" || sub == "code_channel_review_submitted" // ZD2Y br
	switch {
	case IsAutomatedSubtype(sub) && !actionableCode,
		m.Pending || m.Failed, // isTempMsg
		m.Ephemeral,           // isEphemeral
		sub == "thread_hidden",
		isReplyMsg(m),
		sub == "file_comment" || sub == "file_mention" || sub == "file_share" || sub == "file_upload": // Jg0n isFileSubtype
		return false
	}
	if app == nil {
		return true
	}
	if app.IsTempMsg(m) || app.IsEphemeral(m) {
		return false
	}
	if !app.PostingIn(m.Channel).CanReply() { // canThread: is_non_threadable or read-only
		return false
	}
	conv, ok := app.S.Conv(m.Channel)
	if !ok {
		return true
	}
	if hasBarrier(conv.Properties) {
		return false
	}
	if conv.IsIM {
		if conv.User == "USLACK" { // isChannelSystemNotificationIm
			return false
		}
		if u, ok := app.User(conv.User); ok && u.Deleted { // isImWithDeletedMember
			return false
		}
	}
	// a public channel the viewer only previews (isPublicChannel,
	// isMemberOfChannel)
	if !ignoreMembership && conv.IsChannel && !conv.IsPrivate && !conv.IsMPIM && !conv.IsMember {
		return false
	}
	return !threadLocked(app, m) // its thread's replies turned off (i8z0 isThreadLocked)
}

// threadLocked is i8z0 isThreadLocked for m's thread (+sBl L: its
// thread_ts, a root's own included): the root is locked.
func threadLocked(app *core.App, m *core.Message) bool {
	if m.ThreadTS == "" {
		return false
	}
	if m.TS == m.ThreadTS && m.Locked {
		return true
	}
	return app != nil && app.ThreadLocked(m.Channel, m.ThreadTS)
}

// hasBarrier reads the conversation's has_barrier (an information
// barrier).
func hasBarrier(props json.RawMessage) bool {
	if len(props) == 0 {
		return false
	}
	var p struct {
		HasBarrier bool `json:"has_barrier"`
	}
	_ = json.Unmarshal(props, &p)
	return p.HasBarrier
}

// CanStartOrOpenThread is canStartOrOpenThread (gV6m): never on a
// thread_hidden message; else its replies (a reply count, or a broadcast
// with its thread_ts), or canReplyOrComment.
func CanStartOrOpenThread(app *core.App, m *core.Message) bool {
	if m.Subtype == "thread_hidden" {
		return false
	}
	return m.ReplyCount > 0 || (m.Subtype == "thread_broadcast" && m.ThreadTS != "") || CanReplyOrComment(app, m)
}

// canFollow is the follow row's canStartOrOpenThread (QdFb) on a
// top-level message, ignoreMembership as QdFb passes it.
func canFollow(app *core.App, m *core.Message, ignoreMembership bool) bool {
	if m.Subtype == "thread_hidden" {
		return false
	}
	return m.ReplyCount > 0 || (m.Subtype == "thread_broadcast" && m.ThreadTS != "") || canReplyOrComment(app, m, ignoreMembership)
}

// ThreadAction is the thread action (LKNk) for m in where: its name, ok
// false when Slack has none. None in the thread pane (THREAD,
// MEDIA_VIEWER_THREAD and SAVED_FOR_LATER_PAGE containers) or when
// canStartOrOpenThread is false. "View thread" in the Threads view or
// when the viewer can't reply and it has replies, "Reply to thread" when
// it has replies or is a reply, else "Reply in thread".
func ThreadAction(app *core.App, m *core.Message, where MessageContext) (string, bool) {
	if where == InThread || !CanStartOrOpenThread(app, m) {
		return "", false
	}
	canReply := CanReplyOrComment(app, m)
	switch {
	case where == InThreadsView || !canReply && hasRepliesMsg(m):
		return "View thread", true
	case hasRepliesMsg(m) || isReplyMsg(m):
		return "Reply to thread", true
	}
	return "Reply in thread", true
}

// ToolbarShown is shouldRenderActions for m's own kind: no toolbar on a
// temporary message, a thread_hidden one, or an ekm_access_denied,
// dlp_tombstone or dsa_restricted one.
func ToolbarShown(app *core.App, m *core.Message) bool {
	switch m.Subtype {
	case "thread_hidden", "ekm_access_denied", "dlp_tombstone", "dsa_restricted":
		return false
	}
	if app == nil {
		return !m.Pending && !m.Failed
	}
	return !app.IsTempMsg(m)
}

// isEphemeral is core's IsEphemeral, the message's own flag without a
// core.
func isEphemeral(app *core.App, m *core.Message) bool {
	if app == nil {
		return m.Ephemeral
	}
	return app.IsEphemeral(m)
}

// actionable is core's Actionable (isActionable), its message-only part
// without a core.
func actionable(app *core.App, m *core.Message) bool {
	if app != nil {
		return app.Actionable(m)
	}
	switch m.Subtype {
	case "ekm_access_denied", "dlp_tombstone", "dsa_restricted", "retention_threshold", "tombstone", "moderated", "thread_hidden":
		return false
	}
	return !m.Tombstone && !m.Ephemeral && !m.Pending && !m.Failed && m.TS != ""
}

// redesignedHuddle is isRedesignedHuddleSystemMessage (okNg): a
// huddle_thread message under system_messages_redesign_m1.
func redesignedHuddle(app *core.App, m *core.Message) bool {
	return m.Subtype == "huddle_thread" && app != nil && app.Experiment("system_messages_redesign_m1") == "on"
}

// TodoShown is whether the to-do action (addToLaterTodos, on3u) is
// there: none on an ephemeral message, on an automated one (but a code
// channel's actionable system messages), nor on a redesigned huddle
// system message that isn't a to-do already.
func TodoShown(app *core.App, m *core.Message) bool {
	if isEphemeral(app, m) {
		return false
	}
	actionableCode := m.Subtype == "code_channel_lifecycle" || m.Subtype == "code_channel_review_submitted"
	if IsAutomatedSubtype(m.Subtype) && !actionableCode {
		return false
	}
	return IsTodo(app, m) || !redesignedHuddle(app, m)
}

// AddToListShown is shouldShowAddToList (yHBT) outside the right-click
// menu: not an automated message under system_messages_redesign_m1, nor a
// redesigned huddle system message, nor an ephemeral one.
func AddToListShown(app *core.App, m *core.Message) bool {
	if app != nil && app.Experiment("system_messages_redesign_m1") == "on" && IsAutomatedSubtype(m.Subtype) {
		return false
	}
	return !redesignedHuddle(app, m) && !isEphemeral(app, m)
}

// MoreShown is whether MessageActionButtonEllipsis (⋮) renders:
// isOnlyDeletableHere — a tombstone, retention_threshold or moderated
// message in a conversation the viewer isn't a member of — hides it
// unless they may delete the message, which canDeleteMessageHelper never
// allows for those.
func MoreShown(app *core.App, m *core.Message) bool {
	only := m.Tombstone || m.Subtype == "tombstone" || m.Subtype == "retention_threshold" || m.Subtype == "moderated"
	if !only || app == nil {
		return true
	}
	conv, ok := app.S.Conv(m.Channel)
	if !ok || conv.IsIM || conv.IsMPIM {
		return true
	}
	return conv.IsMember
}

// copyLinkShown is canCopyLink (iub5 gP) for what Clack keeps: no link to
// a file comment, an ephemeral message, a tombstone, a moderated one or
// a retention_threshold one.
func copyLinkShown(app *core.App, m *core.Message) bool {
	switch m.Subtype {
	case "file_comment", "tombstone", "moderated", "thread_hidden", "retention_threshold":
		return false
	}
	return !m.Tombstone && !isEphemeral(app, m)
}

// copyTextShown is canCopyMessageText (c10I): not a tombstone, a moderated
// (moderated, thread_hidden), dlp_tombstone or dsa_restricted message, nor
// an automated one (h3YX isAutomatedSubtype: a join, a topic change); and
// the message has text or blocks, which one that's only a file or only
// attachments hasn't. Mod+C is its key, so it's bound only where this is.
func copyTextShown(m *core.Message) bool {
	switch m.Subtype {
	case "tombstone", "moderated", "thread_hidden", "dlp_tombstone", "dsa_restricted":
		return false
	}
	if m.Tombstone || IsAutomatedSubtype(m.Subtype) {
		return false
	}
	return m.Text != "" || hasBlocks(m.Blocks)
}

// hasBlocks is (blocks?.length ?? 0) > 0 for a message's raw blocks.
func hasBlocks(raw json.RawMessage) bool {
	var bs []json.RawMessage
	return json.Unmarshal(raw, &bs) == nil && len(bs) > 0
}

// reminderSubtypes are canAddReminder's allow-list (iub5 kk) besides no
// subtype at all, with the code channels' actionable subtypes (ZD2Y xC).
var reminderSubtypes = map[string]bool{"bot_message": true, "canvas_sharing_message": true, "file_share": true, "file_mention": true,
	"file_comment": true, "me_message": true, "reply_broadcast": true, "thread_broadcast": true, "story_share": true, "async_meeting": true,
	"code_channel_lifecycle": true, "code_channel_review_submitted": true}

// remindShown is canAddReminder (iub5 kk): none on a file comment or an
// ephemeral message; else a message with no subtype or one of
// reminderSubtypes (Slackbot's "ai" messages too, behind a flag Clack
// doesn't have). Tombstones, system messages and the rest have none.
func remindShown(app *core.App, m *core.Message) bool {
	if m.Subtype == "file_comment" || m.Tombstone || isEphemeral(app, m) {
		return false
	}
	return m.Subtype == "" || reminderSubtypes[m.Subtype]
}

// ContextMenuShown is whether MessageActionsContextMenu (DtN2) opens on
// m: never on a moderated (moderated or thread_hidden), dlp_tombstone,
// dsa_restricted or ekm_access_denied message.
func ContextMenuShown(m *core.Message) bool {
	switch m.Subtype {
	case "moderated", "thread_hidden", "dlp_tombstone", "dsa_restricted", "ekm_access_denied":
		return false
	}
	return true
}

// canMarkUnread is L4xk canMarkUnread: not an ephemeral message (iub5 aw)
// nor an archived canvas comment ($Q), in a conversation that isn't
// archived (+NpH) and that the viewer is a member of (crlq g6:
// is_member, or a file channel's transparent membership, which Clack's
// conversations never are). DMs and group DMs are the viewer's.
func canMarkUnread(app *core.App, m *core.Message) bool {
	if isEphemeral(app, m) || (m.DocumentComment != nil && m.DocumentComment.IsArchived) {
		return false
	}
	if app == nil {
		return true
	}
	conv, ok := app.S.Conv(m.Channel)
	if !ok {
		return true // not stored: Slack's selector has no channel to refuse
	}
	if conv.IsArchived {
		return false
	}
	return conv.IsIM || conv.IsMPIM || conv.IsMember
}

// MarkUnreadShown is whether the Mark unread action exists for m: in the
// conversation QRkh (none on an automated message, but a code channel's
// actionable ones, under system_messages_redesign_m1, nor on a
// redesigned huddle system message), in a thread or the Threads view RjOa
// (none on a redesigned huddle system message); both need canMarkUnread.
// Its key, U, is the action's shortcut, so U is unbound wherever the row
// isn't (getActionsForKeyboardShortcuts).
func MarkUnreadShown(app *core.App, m *core.Message, inThread bool) bool {
	if !inThread && app != nil && app.Experiment("system_messages_redesign_m1") == "on" && IsAutomatedSubtype(m.Subtype) &&
		m.Subtype != "code_channel_lifecycle" && m.Subtype != "code_channel_review_submitted" {
		return false
	}
	if redesignedHuddle(app, m) {
		return false
	}
	return canMarkUnread(app, m)
}

// pinRuleSubtypes are the subtypes canPinMessage and canUnpinMessage
// refuse at once.
var pinRuleSubtypes = map[string]bool{"pinned_item": true, "unpinned_item": true, "sh_room_created": true, "sh_room_shared": true}

// canPinHere is gUAL h: the viewer may post in the conversation
// (canUserPostInChannel, FS2H: not archived, not read-only; Clack's
// PostingIn) and, in a channel, is a member of it (silw V: public or
// private channel, crlq bl).
func canPinHere(app *core.App, ch string) bool {
	if app == nil {
		return true
	}
	conv, ok := app.S.Conv(ch)
	if !ok {
		return false // EmXR OL: no channel, no pin
	}
	if !canUserPostIn(app, conv) {
		return false
	}
	if (conv.IsChannel || conv.IsGroup || conv.IsPrivate) && !conv.IsIM && !conv.IsMPIM && !conv.IsMember {
		return false
	}
	return true
}

// PinShown is the pin action's rule (Zn/q): canPin, that is canPinMessage
// (XRuW) for a message not pinned here or canUnpinMessage for one that is,
// and none on a redesigned huddle system message that isn't pinned.
//   - canPinMessage: not ephemeral, nor pinned_item, unpinned_item,
//     sh_room_created or sh_room_shared; canPinHere; not a tombstone,
//     moderated or retention_threshold message, nor one of a locked
//     thread (its root included), nor a channel_join, channel_leave, group_join or group_leave
//     message, nor in a channel shared with another org a channel_topic,
//     channel_purpose, channel_name, group_topic, group_purpose or
//     group_name one.
//   - canUnpinMessage: the same first checks and canPinHere, not a
//     tombstone.
//
// Its key, P, is the action's shortcut, so P is unbound wherever it isn't.
func PinShown(app *core.App, m *core.Message) bool {
	if !m.Pinned && redesignedHuddle(app, m) {
		return false
	}
	if isEphemeral(app, m) || pinRuleSubtypes[m.Subtype] || !canPinHere(app, m.Channel) {
		return false
	}
	tomb := m.Tombstone || m.Subtype == "tombstone"
	if m.Pinned {
		return !tomb
	}
	switch m.Subtype {
	case "moderated", "retention_threshold", "channel_join", "channel_leave", "group_join", "group_leave":
		return false
	case "channel_topic", "channel_purpose", "channel_name", "group_topic", "group_purpose", "group_name":
		if app != nil {
			if conv, ok := app.S.Conv(m.Channel); ok && conv.IsExtShared {
				return false
			}
		}
	}
	if tomb {
		return false
	}
	if threadLocked(app, m) { // a message of a locked thread, its root included
		return false
	}
	return true
}

// msgFrecency are the message menus' rows' frecencyIds (each action's
// selector, build 83aff18a1847b8e9: getMessageActionsFrecencyId "MA" + the
// id it names, or a bare id). The follow row's and the pin row's follow
// the state they show (thread_follow / thread_unfollow, pin / unpin).
var msgFrecency = map[string]string{
	"add_reaction": "MAadd_reaction", "comment": "MAthread_reply", "share_message": "MAshare",
	"add_to_later_todos": "add_to_later_todos", "later": "later",
	"mark_unread": "MAmark_unread", "mark_thread_unread": "MAmark_thread_unread", "remind_later": "MAremind_later",
	"copy_link": "MAcopy_link", "copy_text": "MAcopy_text", "edit_message": "MAedit", "delete_message": "MAdelete",
	"broadcast_message": "MAbroadcast_message", "remove_broadcast": "MAremove",
	"flag_message": "MAflag_message", "report_message": "MAreport_message",
	"view_workflow_details": "MAview_workflow_details", "copy_workflow_id": "MAcopy_workflow_id",
}

// withFrecency gives a message menu's rows their frecencyIds
// (MenuItem.Frecency); a submenu's rows take their parent's (the reminder
// presets are RemindSubmenu's, which records the reminder row's id) unless
// they have their own (Organize ▸ Pin, Connect to apps' shortcuts).
func withFrecency(items []MenuItem) []MenuItem {
	for i := range items {
		it := &items[i]
		if it.Frecency == "" {
			switch it.Action {
			case "subscribe":
				it.Frecency = "MAthread_follow"
				if it.Icon == "bell-off" {
					it.Frecency = "MAthread_unfollow"
				}
			case "pin": // a pin row built without one (the message menu sets it by m.Pinned)
				it.Frecency = "MApin"
			default:
				it.Frecency = msgFrecency[it.Action]
			}
		}
		if len(it.Sub) > 0 {
			it.Sub = withFrecency(append([]MenuItem(nil), it.Sub...))
			if it.Action == "remind_later" {
				for j := range it.Sub {
					if it.Sub[j].Frecency == "" && !it.Sub[j].Separator {
						it.Sub[j].Frecency = it.Frecency
					}
				}
			}
		}
	}
	return items
}

// EditShown is the Edit message action (Oi/D) for the viewer on their own
// message m: canEditMessage (8AvF) with the viewer as the author (another
// person's edit needs the team's "edit" permission, which Clack's
// permissions don't hold: none). No edit
//   - in Slack's system DM (USLACK), nor on an automated message (a join,
//     a topic change), a file share, a reply_broadcast notice, a tombstone,
//     a moderated message or a bookmark_created / bookmark_removed one;
//   - on blocks other than rich_text and context (the portable content
//     composer off), or rich text with none;
//   - in the viewer's own DM: always (Rm5e), before the rest;
//   - past the team's msg_edit_window_mins (0: never);
//   - where the viewer can't post (canUserPostInChannel: an archived or
//     read-only conversation) or, for a reply, reply (canThread);
//   - in a locked thread, nor on a message not yet sent (isTempMsg).
func EditShown(app *core.App, m *core.Message, self string) bool {
	if self == "" || m.User != self || m.TS == "" {
		return false
	}
	sub := m.Subtype
	switch sub {
	case "reply_broadcast", "tombstone", "moderated", "thread_hidden", "bookmark_created", "bookmark_removed",
		"file_comment", "file_mention", "file_share", "file_upload":
		return false
	}
	if m.Tombstone || IsAutomatedSubtype(sub) {
		return false
	}
	var conv *slack.Conversation
	if app != nil {
		if c, ok := app.S.Conv(m.Channel); ok {
			conv = c
		}
	}
	if conv != nil && conv.IsIM && conv.User == "USLACK" { // isChannelSystemNotificationIm
		return false
	}
	if hasBlocks(m.Blocks) && (app == nil || app.Experiment("portable_content_composer") != "treatment") {
		var bs []struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal(m.Blocks, &bs)
		rich := false
		for _, b := range bs {
			switch b.Type {
			case "rich_text":
				rich = true
			case "context":
			default:
				return false
			}
		}
		if !rich {
			return false
		}
	}
	if conv != nil && conv.IsIM && conv.User == self { // the viewer's own DM
		return true
	}
	if app != nil {
		var w int64 = -1
		if v, ok := app.TeamPref("msg_edit_window_mins"); ok {
			_ = json.Unmarshal(v, &w)
		}
		if w == 0 || w > -1 && time.Since(tsTime(m.TS)).Minutes() > float64(w) {
			return false
		}
	}
	if isReplyMsg(m) && !canThreadIn(app, conv) || !isReplyMsg(m) && !canUserPostIn(app, conv) {
		return false
	}
	if threadLocked(app, m) || m.Pending || m.Failed || app != nil && app.IsTempMsg(m) {
		return false
	}
	return true
}

// tsTime is a message ts as a time.
func tsTime(ts string) time.Time {
	sec, frac, _ := strings.Cut(ts, ".")
	s, _ := strconv.ParseInt(sec, 10, 64)
	us, _ := strconv.ParseInt((frac + "000000")[:6], 10, 64)
	return time.Unix(s, us*1000)
}

// canUserPostIn is canUserPostInChannel (FS2H c) as far as Clack knows
// conv: not archived, not read-only or thread-only (Posting), not Slack's
// system DM, nor a DM with a deactivated person.
func canUserPostIn(app *core.App, conv *slack.Conversation) bool {
	if conv == nil {
		return true
	}
	if conv.IsArchived || conv.IsIM && conv.User == "USLACK" {
		return false
	}
	if app == nil {
		return true
	}
	if conv.IsIM {
		if u, ok := app.User(conv.User); ok && u.Deleted {
			return false
		}
	}
	return app.PostingFor(conv).CanPost()
}

// canThreadIn is pS2E canThread for conv: replies may be posted there.
func canThreadIn(app *core.App, conv *slack.Conversation) bool {
	if conv == nil || app == nil {
		return true
	}
	return !conv.IsArchived && app.PostingFor(conv).CanReply()
}

// deleteKeepSubtypes are the automated subtypes canDeleteMyMessageHelper
// still lets the author delete.
var deleteKeepSubtypes = map[string]bool{"pinned_item": true, "sh_room_created": true, "sh_room_shared": true, "reply_broadcast": true, "channel_topic": true}

// DeleteShown is the Delete message… action (n+vA over 0Y8N
// canDeleteMessageHelper) for the viewer (self) on m, with the base
// permissions (the viewer may delete their own messages, not apps'):
//   - any ephemeral message (removed here alone);
//   - none on a tombstone, a retention, bookmark_created,
//     bookmark_removed, huddle_thread or moderated message, nor under
//     system_messages_redesign_m1 on an automated one (the channel
//     manager's exception aside, which Clack's viewer isn't);
//   - in a DM only a bot_message, Slackbot's (USLACKBOT) or the viewer's
//     own; elsewhere the viewer's own;
//   - where the viewer can post (a reply: reply), and not in a locked
//     thread; the viewer's own automated messages only for pinned_item,
//     sh_room_created, sh_room_shared, reply_broadcast and channel_topic.
func DeleteShown(app *core.App, m *core.Message, self string) bool {
	if isEphemeral(app, m) {
		return true
	}
	sub := m.Subtype
	switch sub {
	case "tombstone", "retention", "bookmark_created", "bookmark_removed", "huddle_thread", "moderated":
		return false
	}
	if m.Tombstone || m.TS == "" {
		return false
	}
	if app != nil && app.Experiment("system_messages_redesign_m1") == "on" && IsAutomatedSubtype(sub) {
		return false
	}
	var conv *slack.Conversation
	if app != nil {
		if c, ok := app.S.Conv(m.Channel); ok {
			conv = c
		}
	}
	own := self != "" && m.User == self
	dm := conv != nil && conv.IsIM
	if dm && !(sub == "bot_message" || m.User == "USLACKBOT" || own) {
		return false
	}
	if !dm && !own {
		return false
	}
	if isReplyMsg(m) && !canThreadIn(app, conv) || !isReplyMsg(m) && !canUserPostIn(app, conv) {
		return false
	}
	if dm {
		return true
	}
	if IsAutomatedSubtype(sub) && !deleteKeepSubtypes[sub] {
		return false
	}
	return !threadLocked(app, m) && (!isReplyMsg(m) || canThreadIn(app, conv))
}

// connectToAppsShown is JuvA te's row for m beyond isActionable: e6 gives
// it no submenu (so no row) on an automated message under
// system_messages_redesign_m1, nor on a redesigned huddle system message.
func connectToAppsShown(app *core.App, m *core.Message) bool {
	if app != nil && app.Experiment("system_messages_redesign_m1") == "on" && IsAutomatedSubtype(m.Subtype) {
		return false
	}
	return !redesignedHuddle(app, m)
}
