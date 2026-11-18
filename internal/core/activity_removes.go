package core

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/some-du6e/clack/internal/slack"
)

// Slack's messageChanged (boot-render VxbD) takes the entries about a
// message out of Activity ("Remove activity item" {channelId,
// messageTs}, P0K2) when the new copy is a tombstone (R+b2 isTombstone)
// or is moderated or thread_hidden (DRL6 isModerated), and when the edit
// took the viewer's mention out: checkForMentionInMessage (c+16, with its
// default options) matches the event's previous_message and not the new
// copy. Only message_changed does this; messageDeleted removes them too,
// and a "delete" says that by itself.

// removesActivity reports a message_changed whose update removes the
// Activity entries about its message (Event.Removes).
func (a *App) removesActivity(prev, m *slack.Message) bool {
	switch m.Subtype {
	case "tombstone", "moderated", "thread_hidden":
		return true
	}
	if prev == nil || !a.mayMention(prev) {
		return false
	}
	o := mentionOpts{here: true, broadcast: true, keywords: true}
	return a.mentionsViewer(mentionCopy(prev), o) && !a.mentionsViewer(mentionCopy(m), o)
}

// mentionCopy is what checkForMentionInMessage reads of a message as the
// socket sent it.
func mentionCopy(m *slack.Message) *Message {
	v := &Message{TS: m.TS, ThreadTS: m.ThreadTS, Subtype: m.Subtype, Text: m.Text, Blocks: m.Blocks, Attachments: m.RawAttachments}
	if v.Attachments == nil && len(m.Attachments) > 0 {
		v.Attachments, _ = json.Marshal(m.Attachments)
	}
	return v
}

// mayMention is false for a message that can't mention the viewer, told
// without parsing its blocks (most edits on a busy workspace are of
// messages that never named them): with no highlight words, a mention
// needs the viewer's id, a broadcast or a user group in the text, the
// blocks or the attachments.
func (a *App) mayMention(m *slack.Message) bool {
	if len(m.Attachments) > 0 && len(m.RawAttachments) == 0 || len(a.S.HighlightWords()) > 0 {
		return true
	}
	self := a.S.SelfID()
	str := func(s string) bool {
		return self != "" && strings.Contains(s, self) || strings.Contains(s, "<!")
	}
	raw := func(b []byte) bool {
		return self != "" && bytes.Contains(b, []byte(self)) || bytes.Contains(b, []byte("<!")) || bytes.Contains(b, []byte(`\u003c!`)) ||
			bytes.Contains(b, []byte(`"broadcast"`)) || bytes.Contains(b, []byte(`"usergroup"`))
	}
	return str(m.Text) || raw(m.Blocks) || raw(m.RawAttachments)
}
