package popups

import (
	"encoding/json"

	"github.com/some-du6e/clack/internal/core"
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
func CanReplyOrComment(app *core.App, m *core.Message) bool {
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
	if conv.IsChannel && !conv.IsPrivate && !conv.IsMPIM && !conv.IsMember {
		return false
	}
	return true
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

// copyTextShown is canCopyMessageText (c10I) for the kinds above: none
// on a tombstone, a moderated, dlp_tombstone or dsa_restricted message.
func copyTextShown(m *core.Message) bool {
	switch m.Subtype {
	case "tombstone", "moderated", "thread_hidden", "dlp_tombstone", "dsa_restricted":
		return false
	}
	return !m.Tombstone
}

// remindShown is canAddReminder (iub5 kk) for the kinds above: none on a
// file comment or an ephemeral message, and its subtype allow-list (no
// subtype, bot_message, file_share, thread_broadcast…) has no tombstone,
// moderated, thread_hidden or retention_threshold.
func remindShown(app *core.App, m *core.Message) bool {
	switch m.Subtype {
	case "file_comment", "tombstone", "moderated", "thread_hidden", "retention_threshold", "dlp_tombstone", "dsa_restricted", "ekm_access_denied":
		return false
	}
	return !m.Tombstone && !isEphemeral(app, m)
}
