package core

import (
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
)

// What a notification's text reads of a message's files
// (richtext.NoteFiles), as boot-shared Eley's extractTextFromMessage and
// MXE2's getFileMetaTextForMessage read them (CDN build 35b42de8b870,
// 2026-11-21):
//
//   - The first file from the file store, by id: Eley's
//     `getFileById(e, head(t.files).id)` (4ZwG Lp) for the title and the
//     meta line; the clips from the message's own files (Qre6 over
//     t.files). Slack's store merges every copy of a file it gets (4OY3
//     upsertFiles' processFile: {...held, ...copy}, for a file
//     shouldShowFile shows), so it differs from the message's copy where
//     that copy lacks what an earlier one had: a stub (core fillFiles'
//     fileStub). Clack keeps files in the messages, every held copy
//     refreshed together (onFileChange, fillFiles), so the store's copy
//     is the message's held one, or for a stub a complete copy another
//     held message carries (store.HeldFile, as keepFiles does for a
//     message_changed copy). That read happens when the toast is built
//     (NoteFilesNow); a message's own conversion reads its own copy.
//   - The sender getFileMetaTextForMessage compares the file's uploader
//     with: getMsgSenderEntity's userId (hVKV, noteSender).

// noteFiles is m's NoteFiles: the first file the message's own copy, or
// with held the store's (a stub's complete copy: store.HeldFile).
func (a *App) noteFiles(m *slack.Message, held bool) *richtext.NoteFiles {
	if len(m.Files) == 0 {
		return nil
	}
	first := &m.Files[0]
	if held && fileStub(first) {
		if f, ok := a.S.HeldFile(first.ID); ok {
			first = &f
		}
	}
	return noteFilesWith(m, first)
}

// NoteFilesOf is m's NoteFiles read from its own files, the sender
// included: what a message's conversion keeps.
func NoteFilesOf(m *slack.Message) *richtext.NoteFiles {
	if len(m.Files) == 0 {
		return nil
	}
	return noteFilesWith(m, &m.Files[0])
}

func noteFilesWith(m *slack.Message, first *slack.File) *richtext.NoteFiles {
	n := richtext.NoteFilesOf(m.Files, first)
	n.Sender = noteSender(m, first)
	return n
}

// NoteFilesNow is m's NoteFiles as a notification built now reads them:
// the message as the store holds it, its first file the file store's
// copy (getFileById). A message the store doesn't hold keeps its own.
func (a *App) NoteFilesNow(m *Message) *richtext.NoteFiles {
	if m == nil {
		return nil
	}
	sm, ok := a.S.FindHeld(m.Channel, m.TS)
	if !ok || len(sm.Files) == 0 {
		return m.NoteFiles
	}
	return a.noteFiles(&sm, true)
}

// noteSender is getMsgSenderEntity's userId (client-boot hVKV, build
// 35b42de8b870) for m, whose first file in the store is file: the member
// whose file isn't "someone else's" in getFileMetaTextForMessage, "" for
// a sender with none. In its order:
//
//   - s is t.user || t.comment.user (a legacy file_comment's comment),
//     replaced by a canvas comment root's document_comment
//     most_recent_editor, else its first author.
//   - A huddle message (iub5 mb isHuddleMsg), Slack's welcome party
//     (BiCp AZ), a Slack AI huddle notes message (BiCp Iu with
//     ai_context.summary.type huddle, unreachable after the next rule),
//     a channel or tab canvas update (BiCp cb, Ni) and a Slackbot
//     response (iub5 ZO) name a username, no member.
//   - An AI huddle summary (ai_context.summary.type "huddle") is t.user;
//     a list edit (iub5 aH) list_edit.created_by; a tab added (BiCp gA)
//     tab_added_user_id; a folder item added (BiCp Lo)
//     item_added_user_id.
//   - Slackbot posting a bot's file (s USLACKBOT, the file's bot_id: the
//     email bot's forward among them) is the file's entity (vITP xR
//     getFileEntityId): file.user, none when that is Slackbot too.
//   - Slackbot otherwise, under Slackbot AI (YwqI gT
//     isSlackbotAiNextEnabled, on: messages/custom_response.go), unless
//     its message is Slackbot AI's (X1VC), is "Slack", no member.
//   - Else s.
func noteSender(m *slack.Message, file *slack.File) string {
	s := m.User
	if s == "" && m.Comment != nil {
		s = m.Comment.User
	}
	if dc := m.DocumentComment; dc != nil {
		if dc.MostRecentEditor != "" {
			s = dc.MostRecentEditor
		} else if len(dc.Authors) > 0 {
			s = dc.Authors[0]
		}
	}
	summary := ""
	if c := m.AIContext; c != nil && c.Summary != nil {
		summary = c.Summary.Type
	}
	switch {
	case noteHuddleMsg(m), m.Subtype == "welcome_party":
		return ""
	case summary == "huddle":
		return m.User
	case m.Subtype == "list_edit" && m.ListEdit != nil:
		return m.ListEdit.CreatedBy
	case noteSubtype(m.Subtype, "channel_canvas_updated"), noteSubtype(m.Subtype, "tabbed_canvas_updated"):
		return ""
	case noteSubtype(m.Subtype, "channel_tab_added"):
		return m.TabAddedUserID
	case noteSubtype(m.Subtype, "folder_item_added"):
		return m.ItemAddedUserID
	case m.Subtype == SubtypeSlackbotResponse && m.User == slackbotID:
		return ""
	case s == slackbotID && file != nil && file.BotID != "":
		if file.User == slackbotID {
			return ""
		}
		return file.User
	case m.User == slackbotID && !noteSlackbotAI(m):
		return ""
	}
	return s
}

// noteSubtype is BiCp's pair of subtypes: sub, or its _unread_ignore
// twin.
func noteSubtype(subtype, sub string) bool {
	return subtype == sub || subtype == sub+"_unread_ignore"
}

// noteHuddleMsg is iub5 mb isHuddleMsg: a room_id (makeHuddleObject's,
// from msg.room) on a message with no subtype, a thread_broadcast (BiCp
// SM) or a huddle message (BiCp kn), as messages.isHuddleMsg reads it.
func noteHuddleMsg(m *slack.Message) bool {
	if len(m.Room) <= 2 {
		return false
	}
	switch m.Subtype {
	case "", "thread_broadcast", "huddle_message", "huddle_thread":
		return true
	}
	return false
}

// noteSlackbotAI is X1VC isSlackbotAiMessage: Slackbot's message with
// ai_context.type slackbot_ai or subtype ai.
func noteSlackbotAI(m *slack.Message) bool {
	return m.User == slackbotID && (m.AIContext != nil && m.AIContext.Type == "slackbot_ai" || m.Subtype == "ai")
}
