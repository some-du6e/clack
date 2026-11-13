package core

import (
	"encoding/json"

	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
)

// PreviewDoc is Slack's SimpleMessagePreview of m (the body of an
// Activity row, richtext.PreviewFromMessage): the message's section,
// header and rich_text blocks only (never a context block, Block Kit
// elements, files or attachments), else a task block's title or the
// message's text. ok is false when Slack's preview isn't made from blocks
// or a text (a message without blocks, a file without text): m.Doc
// stands.
func (a *App) PreviewDoc(m *Message) (richtext.Doc, bool) {
	if m == nil {
		return richtext.Doc{}, false
	}
	sm := slack.Message{Type: "message", TS: m.TS, User: m.User, BotID: m.BotID, Channel: m.Channel, Subtype: m.Subtype,
		Text: m.Text, Blocks: m.Blocks}
	if len(m.Attachments) > 2 {
		_ = json.Unmarshal(m.Attachments, &sm.Attachments)
	}
	return richtext.PreviewFromMessage(a.S, &sm)
}

// DisplaysEdited is Slack's shouldDisplayAsEdited (client-boot): an
// edited message shows "(edited)" unless it has a bot_id (an app's
// message its bot edited, as an agent's reply that a chat.update
// finished, shows none; a canvas document comment would need to be
// archived too). Every view that draws a message's label follows it;
// Activity's previews (SimpleMessagePreview) draw no label at all.
func (m *Message) DisplaysEdited() bool { return m.Edited && m.BotID == "" }
