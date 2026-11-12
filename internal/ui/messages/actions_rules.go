package messages

import (
	"encoding/json"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/popups"
)

// The hover toolbar's thread and share buttons, ported from Slack's
// message action selectors (build 4d0c78085232, boot-shared and
// client-boot):
//
//   - The thread action (t8wB) is null in the thread pane (THREAD,
//     MEDIA_VIEWER_THREAD and SAVED_FOR_LATER_PAGE containers) and when
//     canStartOrOpenThread (sSNY) is false: a thread_hidden message, or one
//     with no replies (reply_count, or a broadcast with a thread_ts) that
//     canReplyOrComment (RpcX) rejects. Its label: "View thread" in the
//     Threads view or when you can't reply and it has replies (a reply
//     count, or a broadcast: R+b2 isThreadBroadcast), "Reply to thread"
//     when it has replies or is a reply, else "Reply in thread".
//   - The share action (6bWJ) is null for automated subtypes (KC8h) under
//     system_messages_redesign_m1, redesigned huddle system messages
//     (N4vB), messages that can't be shared (p6HR canBeShared: file
//     comments, unknown, EKM/DLP/DSA-restricted, ephemeral, tombstones) and,
//     under enable_new_channel_settings, channels whose properties
//     restrict_message_and_file_sharing (1oza). Its label follows the
//     team's private_message_forwarding pref (Db0x, teamPrefs[..] ===
//     true): "Forward message…" / "Share message…"; a message that is one
//     file and nothing else outside a thread (iR00 isSingleFileMessageAction)
//     says "Forward file…" / "Share file…", "Share canvas…" (a canvas with
//     forwarding on), "Share list…"; data-qa share_file or share_message.

// isAutomatedSubtype is KC8h's isAutomatedSubtype (popups keeps the
// list: the share action there reads it too).
func isAutomatedSubtype(s string) bool { return popups.IsAutomatedSubtype(s) }

// isReply is azCc isReply: a thread_ts that isn't the message's own ts.
func isReply(m *core.Message) bool { return m.ThreadTS != "" && m.ThreadTS != m.TS }

// hasReplies is the thread action's hasReplies: R+b2 isThreadBroadcast or
// a reply count.
func hasReplies(m *core.Message) bool { return m.Subtype == "thread_broadcast" || m.ReplyCount > 0 }

// canReplyOrComment is RpcX canReplyOrComment for a channel message, as far
// as Clack knows the channel (ignoreMembership and ignoreImWithDeletedMmbr
// false, the thread action's call).
func canReplyOrComment(app *core.App, m *core.Message) bool {
	sub := m.Subtype
	if sub == "welcome_party" {
		return true
	}
	actionableCode := sub == "code_channel_lifecycle" || sub == "code_channel_review_submitted" // 6B6N br
	switch {
	case isAutomatedSubtype(sub) && !actionableCode,
		m.Pending || m.Failed, // vITy isTempMsg: a local ts
		sub == "thread_hidden",
		isReply(m),
		sub == "file_comment" || sub == "file_mention" || sub == "file_share" || sub == "file_upload": // rXmn
		return false
	}
	if app == nil {
		return true
	}
	if !app.PostingIn(m.Channel).CanReply() { // Opi6 canThread: is_non_threadable or read-only
		return false
	}
	conv, ok := app.S.Conv(m.Channel)
	if !ok {
		return true
	}
	if barrier(conv.Properties) { // Opi6 hasBarrier
		return false
	}
	if conv.IsIM {
		if conv.User == "USLACK" { // ZgxJ isChannelSystemNotificationIm
			return false
		}
		if u, ok := app.User(conv.User); ok && u.Deleted { // Opi6 isImWithDeletedMember
			return false
		}
	}
	// a public channel the viewer only previews (aK5v isPublicChannel, JUrK
	// isMemberOfChannel)
	if conv.IsChannel && !conv.IsPrivate && !conv.IsMPIM && !conv.IsMember {
		return false
	}
	return true
}

// barrier reads the conversation's has_barrier (an information barrier).
func barrier(props json.RawMessage) bool {
	if len(props) == 0 {
		return false
	}
	var p struct {
		HasBarrier bool `json:"has_barrier"`
	}
	_ = json.Unmarshal(props, &p)
	return p.HasBarrier
}

// replyLabel is the thread action's name for m, "" when the toolbar has
// no thread button. thread: the list is the thread pane.
func replyLabel(app *core.App, m *core.Message, thread bool) string {
	if thread {
		return ""
	}
	canReply := canReplyOrComment(app, m)
	// sSNY canStartOrOpenThread
	if m.Subtype == "thread_hidden" || !(m.ReplyCount > 0 || (m.Subtype == "thread_broadcast" && m.ThreadTS != "") || canReply) {
		return ""
	}
	switch {
	case !canReply && hasReplies(m):
		return "View thread"
	case hasReplies(m) || isReply(m):
		return "Reply to thread"
	}
	return "Reply in thread"
}

// shareAction is the share action for m (popups.ShareAction, which the
// right-click menu's Forward row reads as well): its label and data-qa,
// ok false when the toolbar has none.
func shareAction(app *core.App, m *core.Message, thread bool) (label, qa string, ok bool) {
	a, ok := popups.ShareAction(app, m, thread)
	return a.Label, a.QA, ok
}
