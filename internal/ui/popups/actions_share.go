package popups

import (
	"encoding/json"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"
)

// The share action (6bWJ, boot-shared, build 4d0c78085232): the hover
// toolbar's Forward button and the right-click menu's first group (kctM's
// CONTEXT branch) both read it. It is null for automated subtypes (KC8h)
// under system_messages_redesign_m1, redesigned huddle system messages
// (N4vB), messages that can't be shared (p6HR canBeShared: file comments,
// unknown, EKM/DLP/DSA-restricted, ephemeral, tombstones) and, under
// enable_new_channel_settings, channels whose properties
// restrict_message_and_file_sharing (1oza). Its label follows the team's
// private_message_forwarding pref (Db0x, teamPrefs[..] === true):
// "Forward message…" / "Share message…"; a message that is one file and
// nothing else outside a thread (iR00 isSingleFileMessageAction) says
// "Forward file…" / "Share file…", "Share canvas…" (a canvas with
// forwarding on), "Share list…"; data-qa share_file or share_message. Its
// shortcut hint is "F" with forwarding, else "S" (the key is SHARE either
// way, which binds both).

// ShareActionInfo is the share action of a message: its label, data-qa
// and the shortcut hint its menu row shows.
type ShareActionInfo struct {
	Label, QA, Hint string
}

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

// IsAutomatedSubtype is KC8h's isAutomatedSubtype.
func IsAutomatedSubtype(s string) bool { return automatedSubtypes[s] }

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

// privateForwarding is Db0x: the team's private_message_forwarding pref
// is true.
func privateForwarding(app *core.App) bool {
	if app == nil {
		return false
	}
	v, _ := app.TeamPref("private_message_forwarding")
	return string(v) == "true"
}

// ShareAction is the share action for m, ok false when Slack shows none.
// thread: the list is the thread pane.
func ShareAction(app *core.App, m *core.Message, thread bool) (ShareActionInfo, bool) {
	if app != nil && app.Experiment("system_messages_redesign_m1") == "on" && (IsAutomatedSubtype(m.Subtype) || m.Subtype == "huddle_thread") {
		return ShareActionInfo{}, false
	}
	if !canBeShared(m) {
		return ShareActionInfo{}, false
	}
	if app != nil && app.Experiment("enable_new_channel_settings") == "on" {
		if c, ok := app.S.Conv(m.Channel); ok && restrictsSharing(c.Properties) {
			return ShareActionInfo{}, false
		}
	}
	forwarding := privateForwarding(app)
	hint := "S"
	if forwarding {
		hint = "F"
	}
	info := func(label, qa string) (ShareActionInfo, bool) {
		return ShareActionInfo{Label: label, QA: qa, Hint: hint}, true
	}
	if app != nil && app.IsCanvasComment(m) { // 5t6x: a canvas comment first
		if forwarding {
			return info("Forward comment…", "share_message")
		}
		return info("Share comment…", "share_message")
	}
	if f := singleFile(m, thread); f != nil {
		switch {
		case fileIsCanvas(f) && forwarding:
			return info("Share canvas…", "share_file")
		case fileIsCanvas(f):
			return info("Share file…", "share_file")
		case fileIsList(f):
			return info("Share list…", "share_file")
		case forwarding:
			return info("Forward file…", "share_file")
		}
		return info("Share file…", "share_file")
	}
	if forwarding {
		return info("Forward message…", "share_message")
	}
	return info("Share message…", "share_message")
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
