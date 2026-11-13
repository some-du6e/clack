package richtext

// PreviewFromMessage is Slack's SimpleMessagePreview (boot-shared cjey,
// build fa938a086569), the body of an Activity row (ActivityMessage
// /ad9): the message's previewable text blocks and nothing else.
//
//   - truncateBlocks (QnfD) keeps the blocks isContentBlock (fEfI)
//     passes: section and header with a text, rich_text with elements.
//     A context block (an agent's "done in 6.6s" line), actions, images,
//     dividers and Block Kit agent blocks are never part of a preview;
//     the blocks kept are drawn one per line.
//   - With none, the text is getTaskBlockTitle (zqto: when every block
//     is a plan or task_card, the first title, a plan's getPlanTitle),
//     else getMessageRootText (c7LH): the message's text; a message
//     unfurl's text or URL when the text is empty or only the unfurl's
//     link (isSimpleMessageUnfurl, l8BP, which also skips the blocks);
//     else the first attachment's fallback, or pretext and text.
//   - Files, attachments and the "(edited)" label are never drawn
//     (SimpleMessagePreview has no EditedLabel).
//
// ok is false when the preview isn't made from blocks or a text (a
// message without blocks, a tombstone, a file with no text): the caller
// keeps the message's own document.

import (
	"encoding/json"
	"strings"

	"github.com/some-du6e/clack/internal/slack"
)

// previewBlock is the part of a block isContentBlock reads.
type previewBlock struct {
	Type     string            `json:"type"`
	Text     json.RawMessage   `json:"text"`
	Elements []json.RawMessage `json:"elements"`
}

// previewable is fEfI's isPreviewableTextBlock && hasPreviewableTextContent.
func (b *previewBlock) previewable() bool {
	switch b.Type {
	case "section", "header":
		return len(b.Text) > 0 && string(b.Text) != "null"
	case "rich_text":
		return len(b.Elements) > 0
	}
	return false
}

// PreviewFromMessage returns the preview document of m (see above).
func PreviewFromMessage(r Resolver, m *slack.Message) (Doc, bool) {
	if m == nil || m.Subtype == "tombstone" || m.Subtype == "message_deleted" || m.Subtype == "huddle_thread" {
		return Doc{}, false
	}
	var raws []json.RawMessage
	if len(m.Blocks) <= 2 || json.Unmarshal(m.Blocks, &raws) != nil || len(raws) == 0 {
		return Doc{}, false
	}
	simple := simpleMessageUnfurl(m, raws)
	if !simple {
		var d Doc
		kept := false
		for _, raw := range raws {
			var b previewBlock
			if json.Unmarshal(raw, &b) != nil || !b.previewable() {
				continue
			}
			kept = true
			d.Blocks = append(d.Blocks, previewBlockText(r, m, raw, &b)...)
		}
		if kept {
			return d, true
		}
	}
	text := ""
	if !simple {
		text = taskBlockTitle(raws)
	}
	if text == "" {
		text = messageRootText(m)
	}
	if strings.TrimSpace(text) == "" {
		return Doc{}, false
	}
	return Doc{Blocks: FromMrkdwn(r, text)}, true
}

// previewBlockText is 3MFy PreviewBlockText: a rich_text block as rich
// text (the block alone: no files, attachments or huddle); a section's or
// header's text object only (no fields, accessory or header styling):
// mrkdwn formatted, plain_text as text, its :emoji: drawn unless "emoji"
// is false.
func previewBlockText(r Resolver, m *slack.Message, raw json.RawMessage, b *previewBlock) []Block {
	if b.Type == "rich_text" {
		one, _ := json.Marshal([]json.RawMessage{raw})
		txt := slack.Message{Type: m.Type, TS: m.TS, User: m.User, BotID: m.BotID, Channel: m.Channel, Blocks: one}
		return FromMessage(r, &txt).Blocks
	}
	var t textObj
	if json.Unmarshal(b.Text, &t) != nil {
		return nil
	}
	if t.Type == "mrkdwn" {
		return FromMrkdwn(r, t.Text)
	}
	sp := plainSpansNoEmoji(t.Text)
	if t.Type == "plain_text" && (t.Emoji == nil || *t.Emoji) {
		sp = plainSpans(r, t.Text)
	}
	if len(sp) == 0 {
		return nil
	}
	return []Block{{Kind: Paragraph, Spans: sp}}
}

// taskBlockTitle is zqto getTaskBlockTitle: when every block is a plan or
// task_card, the first one's title (a plan's getPlanTitle).
func taskBlockTitle(raws []json.RawMessage) string {
	bs := make([]bk, 0, len(raws))
	for _, raw := range raws {
		var b bk
		if json.Unmarshal(raw, &b) != nil || (b.Type != "plan" && b.Type != "task_card") {
			return ""
		}
		bs = append(bs, b)
	}
	for i := range bs {
		t := ""
		if bs[i].Title != nil {
			t = bs[i].Title.Text
		}
		if t == "" && bs[i].Type == "plan" {
			t = planTitle(bs[i].Tasks)
		}
		if t != "" {
			return t
		}
	}
	return ""
}

// messageRootText is c7LH getMessageRootText without the file (ok false
// then keeps the file's own document).
func messageRootText(m *slack.Message) string {
	a := m.Text
	if len(m.Attachments) == 0 {
		return a
	}
	t := &m.Attachments[0]
	if t.IsMsgUnfurl && (a == "" || simpleMessageUnfurl(m, nil)) {
		a = firstNonEmpty(t.Text, t.OriginalURL)
	}
	if a == "" {
		switch {
		case t.Fallback != "":
			a = t.Fallback
		case t.Pretext != "" && t.Text != "":
			a = t.Pretext + " " + t.Text
		default:
			a = firstNonEmpty(t.Text, t.Pretext)
		}
	}
	return a
}

// simpleMessageUnfurl is l8BP isSimpleMessageUnfurl: the message is only a
// shared message's link (its text, or one rich_text link to it).
func simpleMessageUnfurl(m *slack.Message, raws []json.RawMessage) bool {
	if len(m.Attachments) == 0 || !m.Attachments[0].IsMsgUnfurl {
		return false
	}
	from := m.Attachments[0].FromURL
	if raws == nil && len(m.Blocks) > 2 {
		_ = json.Unmarshal(m.Blocks, &raws)
	}
	if len(raws) > 1 {
		return false
	}
	if len(raws) == 1 {
		var b struct {
			Type     string `json:"type"`
			Elements []struct {
				Elements []struct {
					Type string `json:"type"`
					URL  string `json:"url"`
					Text string `json:"text"`
				} `json:"elements"`
			} `json:"elements"`
		}
		if json.Unmarshal(raws[0], &b) == nil && b.Type == "rich_text" {
			if len(b.Elements) != 1 || len(b.Elements[0].Elements) != 1 {
				return false
			}
			s := b.Elements[0].Elements[0]
			return s.Type == "link" && s.URL == from && (s.Text == "" || s.Text == from)
		}
	}
	return m.Text == "" || m.Text == "<"+from+">"
}
