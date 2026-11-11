package messages

import (
	"encoding/json"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"
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

// automatedSubtypes is KC8h isAutomatedSubtype's list.
var automatedSubtypes = map[string]bool{}

func init() {
	for _, s := range []string{"agent_view_added", "code_channel_review_submitted", "code_channel_lifecycle", "channel_join", "channel_leave",
		"channel_topic", "channel_purpose", "channel_archive", "channel_unarchive", "channel_convert_to_private", "channel_convert_to_public",
		"channel_convert_to_linked_private", "channel_convert_to_linked_private_read", "channel_convert_to_linked_private_edit",
		"channel_convert_to_linked_private_transfer", "channel_convert_shared", "group_join", "group_leave", "group_topic", "group_purpose",
		"group_archive", "group_unarchive", "group_name", "channel_name", "play_sound", "pinned_item", "unpinned_item", "sh_room_shared",
		"sh_room_created", "bot_enable", "bot_disable", "bot_add", "bot_remove", "bot_updated", "reminder_add", "reminder_delete",
		"reply_broadcast", "app_conversation_join", "app_conversation_leave", "app_conversation_created", "channel_posting_permissions",
		"retention_threshold", "joiner_notification", "joiner_notification_for_inviter", "prejoin_dm_welcome_party_notification",
		"slack_connect_dm_acceptance", "canvas", "mpdm_move", "canvas_open_by_default", "canvas_editing_permissions_changed",
		"channel_canvas_updated", "channel_canvas_updated_unread_ignore", "tabbed_canvas_updated", "tabbed_canvas_updated_unread_ignore",
		"automatic_ai_huddle_notes_enabled", "automatic_ai_huddle_notes_disabled", "workflow_channel_opt_in_enabled",
		"workflow_channel_opt_in_disabled", "welcome_party"} {
		automatedSubtypes[s] = true
	}
}

// isAutomatedSubtype is KC8h's isAutomatedSubtype.
func isAutomatedSubtype(s string) bool { return automatedSubtypes[s] }

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

// canBeShared is p6HR canBeShared for what Clack keeps of a message: not
// a file comment, and actionable (not a tombstone, ephemeral or
// restricted message).
func canBeShared(m *core.Message) bool {
	switch m.Subtype {
	case "file_comment", "tombstone":
		return false
	}
	return !m.Tombstone && !m.Pending && !m.Failed
}

// singleFile is iR00 isSingleFileMessageAction: one file and neither text
// nor blocks, in a channel list (not the thread pane, not a reply).
func singleFile(m *core.Message, thread bool) *richtext.File {
	if thread || m.ThreadTS != "" || m.Text != "" || len(m.Blocks) > 0 {
		return nil
	}
	var f *richtext.File
	for i := range m.Doc.Blocks {
		if b := &m.Doc.Blocks[i]; b.Kind == richtext.FileBlock && b.File != nil {
			if f != nil {
				return nil
			}
			f = b.File
		}
	}
	return f
}

// fileIsCanvas and fileIsList are 2HRR's.
func fileIsCanvas(f *richtext.File) bool { return f.Mode == "quip" || f.Type == "quip" }
func fileIsList(f *richtext.File) bool {
	return (f.Mode == "list" || f.Type == "list") && f.Subtype != "poll"
}

// shareAction is the share action for m: its label and data-qa, ok false
// when the toolbar has none.
func shareAction(app *core.App, m *core.Message, thread bool) (label, qa string, ok bool) {
	if app != nil && app.Experiment("system_messages_redesign_m1") == "on" && (isAutomatedSubtype(m.Subtype) || m.Subtype == "huddle_thread") {
		return "", "", false
	}
	if !canBeShared(m) {
		return "", "", false
	}
	if app != nil && app.Experiment("enable_new_channel_settings") == "on" {
		if c, ok := app.S.Conv(m.Channel); ok && restrictsSharing(c.Properties) {
			return "", "", false
		}
	}
	forwarding := false
	if app != nil {
		v, _ := app.TeamPref("private_message_forwarding")
		forwarding = string(v) == "true"
	}
	if f := singleFile(m, thread); f != nil {
		switch {
		case fileIsCanvas(f) && forwarding:
			return "Share canvas…", "share_file", true
		case fileIsCanvas(f):
			return "Share file…", "share_file", true
		case fileIsList(f):
			return "Share list…", "share_file", true
		case forwarding:
			return "Forward file…", "share_file", true
		}
		return "Share file…", "share_file", true
	}
	if forwarding {
		return "Forward message…", "share_message", true
	}
	return "Share message…", "share_message", true
}

// restrictsSharing reads properties.restrict_message_and_file_sharing (1oza).
func restrictsSharing(props json.RawMessage) bool {
	if len(props) == 0 {
		return false
	}
	var p struct {
		Restrict bool `json:"restrict_message_and_file_sharing"`
	}
	_ = json.Unmarshal(props, &p)
	return p.Restrict
}
