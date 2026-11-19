package richtext

import (
	"strings"

	"github.com/some-du6e/clack/internal/slack"
)

// NotificationText is the body Slack's notification shows for a message
// with blocks. getContentForNotification (boot-shared Eley) reads it
// through extractTextFromMessage and convertMessageToPlainText (client-boot
// 2xQY), which expands condition blocks only for message summaries and
// has no fallback chain: a condition, a fallback_canary and a type it has
// no converter for add nothing, and blocks that add no text leave the
// message text. d is m's Doc (FromMessage); what it draws after the
// blocks (files, attachments) follows as drawn.
func NotificationText(r Resolver, m *slack.Message, d Doc, st SpanText) string {
	// d's own part: what FromMessageIn drew for the blocks (or the text)
	dc := newBuilder(r, m, m.Channel)
	own := len(dc.blocks(parseBlocks(dc.expanded(m.Blocks))))
	if own == 0 && m.Text != "" && (!hasMessageBlocks(m.Blocks) || dc.missing) {
		own = len(FromMrkdwn(r, m.Text))
	}
	nc := newBuilder(r, m, m.Channel)
	nc.notify = true
	s := strings.TrimSpace(PlainTextWith(Doc{Blocks: nc.blocks(parseBlocks(m.Blocks))}, st))
	if s == "" {
		s = PlainTextWith(Doc{Blocks: FromMrkdwn(r, m.Text)}, st)
	}
	if own < len(d.Blocks) {
		if rest := PlainTextWith(Doc{Blocks: d.Blocks[own:]}, st); rest != "" {
			if s != "" {
				s += "\n"
			}
			s += rest
		}
	}
	return s
}

// SearchText is the plain text a search result shows for d. Slack's
// search previews give MessageBlocks NoUnsupportedBlockNotice as their
// fallback (boot-shared vVRo), so the "can't be fully displayed" card adds
// nothing there; what a block's fallback payload draws still counts.
func SearchText(d Doc) string {
	return PlainText(Doc{Blocks: withoutCards(d.Blocks)})
}

func withoutCards(bs []Block) []Block {
	var out []Block
	for _, b := range bs {
		if b.Kind == UnknownBlock {
			continue
		}
		if len(b.Children) > 0 {
			b.Children = withoutCards(b.Children)
		}
		out = append(out, b)
	}
	return out
}
