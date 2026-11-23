package richtext

import (
	"encoding/json"

	"github.com/some-du6e/clack/internal/slack"
)

// A message_embed block: Slack's MessageEmbed (boot-shared ACXQ, CDN build
// 9f450b8ad3db). It reads the message at the block's channel and ts from
// the client's store (no fetch: getMessageByChannelAndTs) and draws it as
// a message unfurl: renderMessageAttachment with isMsgUnfurl, its props
// getMessageAttachmentProps' (j3iH) from the message:
//
//   - the message's blocks without related_automation_decorator ones, an
//     mcp_app (or a prototype that is one) as an italic "This content is
//     only available in the original conversation."; with no blocks and no
//     text, its first attachment's blocks (unless that is itself a
//     message unfurl);
//   - its text, ts and channel, its author (the member, or a bot's name:
//     getMemberNameForMessage), whether it's a thread reply (the card's
//     "From a thread in" context), its files.
//
// A message the client doesn't hold draws nothing. Slack's attachment
// renders an embedded message's own message_embed blocks again, with no
// bound of its own; Clack's builder draws an embed of a message already
// being embedded on the way here (a cycle: a message embedding itself)
// as nothing, and at most maxEmbedCards embeds in one conversion, so a
// chain of messages that each embed the one before costs a bounded
// number of cards. Left out: the unfurl's link to the message (fromUrl
// needs the team's domain), its other attachments (children) and a Slack
// Connect source team.

// MessageFinder is optional on a Resolver: a message the client holds
// (the store's FindHeld).
type MessageFinder interface {
	FindHeld(ch, ts string) (slack.Message, bool)
}

// maxEmbedCards is how many message_embed cards one conversion draws.
const maxEmbedCards = 20

// messageEmbed builds a message_embed block.
func (c *builder) messageEmbed(out []Block, b *bk) []Block {
	ch, ts := string(b.Channel), string(b.TS)
	mf, ok := c.r.(MessageFinder)
	if !ok || ch == "" || ts == "" || c.depth >= maxFallbackDepth || c.embedCards >= maxEmbedCards {
		return out
	}
	key := ch + "/" + ts
	if c.embedding[key] {
		return out // a message embedding itself, or one embedding it
	}
	m, ok := mf.FindHeld(ch, ts)
	if !ok {
		return out
	}
	a := embedAttachment(c.r, &m, ch)
	c.embedCards++
	if c.embedding == nil {
		c.embedding = map[string]bool{}
	}
	c.embedding[key] = true
	c.depth++ // an embed of a message that embeds this one
	blk, ok := c.attachment(&a)
	c.depth--
	delete(c.embedding, key)
	if !ok || blk.Att == nil {
		return out
	}
	blk.Att.Embed = true
	return append(out, blk)
}

// embedAttachment is getMessageAttachmentProps as an unfurl attachment.
func embedAttachment(r Resolver, m *slack.Message, ch string) slack.Attachment {
	a := slack.Attachment{IsMsgUnfurl: true, ChannelID: firstNonEmpty(m.Channel, ch), Text: m.Text, AuthorID: m.User, Files: m.Files}
	a.TS = json.RawMessage(`"` + m.TS + `"`)
	if a.ChannelID != "" {
		if n := r.ChannelName(a.ChannelID); n != a.ChannelID {
			a.ChannelName = n
		}
	}
	// getMemberNameForMessage: the member, else the bot's name
	switch {
	case m.User != "":
		a.AuthorName = r.UserName(m.User)
	case m.BotProfile != nil && m.BotProfile.Name != "":
		a.AuthorName = m.BotProfile.Name
	default:
		a.AuthorName = m.Username
	}
	// isReplyUnfurl: a reply in a thread (not its root)
	a.IsReplyUnfurl = m.ThreadTS != "" && m.ThreadTS != m.TS
	blocks := embedBlocks(m.Blocks)
	if len(blocks) == 0 && m.Text == "" && len(m.Attachments) > 0 {
		if f := m.Attachments[0]; len(f.Blocks) > 0 && !f.IsMsgUnfurl {
			blocks = f.Blocks
		}
	}
	a.Blocks = blocks
	return a
}

// embedBlocks are the message's blocks as the unfurl draws them.
func embedBlocks(raw json.RawMessage) json.RawMessage {
	var parts []json.RawMessage
	if json.Unmarshal(raw, &parts) != nil || len(parts) == 0 {
		return nil
	}
	notice, _ := json.Marshal(map[string]any{"type": "rich_text", "block_id": "", "elements": []any{map[string]any{
		"type": "rich_text_section", "elements": []any{map[string]any{"type": "text", "style": map[string]any{"italic": true},
			"text": "This content is only available in the original conversation."}}}}})
	out := make([]json.RawMessage, 0, len(parts))
	for _, p := range parts {
		var t struct {
			Type          string `json:"type"`
			CandidateType string `json:"candidate_type"`
		}
		_ = json.Unmarshal(p, &t)
		switch {
		case t.Type == "related_automation_decorator":
			continue
		case t.Type == "mcp_app" || t.Type == "prototype" && t.CandidateType == "mcp_app":
			out = append(out, notice)
		default:
			out = append(out, p)
		}
	}
	b, _ := json.Marshal(out)
	return b
}
