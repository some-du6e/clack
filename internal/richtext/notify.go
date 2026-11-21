package richtext

import (
	"strconv"
	"strings"

	"github.com/some-du6e/clack/internal/slack"
)

// NotificationText is extractTextFromMessage (boot-shared Eley), the body
// Slack's notification shows for a message, but for its last line: the
// caller writes "message {subtype} (message missing text)" for a message
// with no files and no attachments. It reads convertMessageToPlainText
// (client-boot 2xQY) with blockSpacing "single" and the default
// verbosity. That converter expands condition blocks only for message
// summaries and has no fallback chain: each block goes through its own
// converter (its eh table, notifyBlock), a type it has none for adds
// nothing, each block's text is trimmed, and blocks that add no text leave
// the message text as written (getMessageText, untrimmed: only a message
// summary trims it). Only when both are empty does the body come from
// what d draws after the blocks (its files) or from the attachments: a
// message unfurl's blocks (convertBlocksToPlainText's default spacing, a
// blank line apart), else the first attachment's fallback, text, pretext
// or footer (textContentFromAttachments). Emoji are their text under
// shouldRenderEmojiAsText (client-boot pfK8, emoji_mode as_text: r's
// EmojiAsText), as FromMessageIn draws them. Not ported: a file subtype's
// meta line (getFileMetaTextForMessage), which Slack reads for a
// file_share message in place of its text, and for a file with no title.
func NotificationText(r Resolver, m *slack.Message, d Doc, st SpanText) string {
	nc := newBuilder(r, m, m.Channel)
	nc.notify = true
	if s := notifyBlocks(notifyEmoji(r, nc.blocks(parseBlocks(m.Blocks))), st, "\n"); s != "" {
		return s
	}
	if s := PlainTextWith(Doc{Blocks: notifyEmoji(r, FromMrkdwn(r, m.Text))}, st); s != "" {
		return s
	}
	// d's own part: what FromMessageIn drew for the blocks (or the text);
	// its files follow (a file's title)
	dc := newBuilder(r, m, m.Channel)
	own := 0
	if raw := dc.expanded(m.Blocks); !blocksDrawNothing(raw, parseBlocks(raw)) {
		own = len(dc.blocks(parseBlocks(raw)))
	}
	if own == 0 && m.Text != "" && (!rendersBlocks(m) || dc.missing) {
		own = len(FromMrkdwn(r, m.Text))
	}
	for _, b := range d.Blocks[min(own, len(d.Blocks)):] {
		if b.Kind == FileBlock && b.File != nil {
			return strings.TrimSpace(firstNonEmpty(b.File.Title, b.File.Name))
		}
	}
	for i := range m.Attachments { // getForwardedAttachmentBlocks
		a := &m.Attachments[i]
		if !a.IsMsgUnfurl {
			continue
		}
		bs := a.Blocks
		if len(parseBlocks(bs)) == 0 && len(a.MessageBlocks) > 0 {
			bs = a.MessageBlocks[0].Message.Blocks
		}
		if s := notifyBlocks(notifyEmoji(r, nc.blocks(parseBlocks(bs))), st, "\n\n"); s != "" {
			return s
		}
		break
	}
	for i := range m.Attachments {
		a := &m.Attachments[i]
		if t := firstNonEmpty(a.Fallback, a.Text, a.Pretext, a.Footer); t != "" {
			return t
		}
	}
	return ""
}

// notifyEmoji is extractTextFromMessage's convertEmoji: "text" under
// shouldRenderEmojiAsText (EmojiToText), else as drawn.
func notifyEmoji(r Resolver, bs []Block) []Block {
	if e, ok := r.(emojiTexter); ok && e.EmojiAsText() {
		EmojiToText(bs)
	}
	return bs
}

// notifyBlocks is convertBlocksToPlainText: each block's text trimmed,
// the empty ones dropped, sep apart ("\n" with blockSpacing "single", else
// a blank line).
func notifyBlocks(bs []Block, st SpanText, sep string) string {
	var out []string
	for i := range bs {
		if s := strings.TrimSpace(notifyBlock(&bs[i], st)); s != "" {
			out = append(out, s)
		}
	}
	return strings.Join(out, sep)
}

// notifyBlock is a block's text by 2xQY's converters in the default
// verbosity: a section its text and fields, a line each (no accessory); a
// context its text elements joined by spaces (no images); an image block
// its title (not its alt text); a table its rows, cells joined by ", "; a
// container its title; a share_shortcut its title
// (convertStringTitledBlockToPlainText); a video block its title
// (convertVideoBlockToPlainText; Clack draws it as an attachment); a
// rich_text list its items as "• item." (notifyList). Actions, alerts,
// callouts, cards, calls, loading, inputs, files, dividers and types with
// no converter add nothing.
func notifyBlock(b *Block, st SpanText) string {
	switch b.Kind {
	case Paragraph, Preformatted, Quote, Header, RichHeader, Tombstone, Agent:
		return spansText(b.Spans, st)
	case Section:
		var parts []string
		if len(b.Spans) > 0 {
			parts = append(parts, spansText(b.Spans, st))
		} else if len(b.Children) > 0 {
			parts = append(parts, notifyBlocks(b.Children, st, "\n"))
		}
		for _, f := range b.Fields {
			if t := spansText(f, st); t != "" {
				parts = append(parts, t)
			}
		}
		return strings.Join(parts, "\n")
	case Context:
		var parts []string
		for i := range b.Children {
			if c := &b.Children[i]; c.Kind == Paragraph {
				if t := spansText(c.Spans, st); t != "" {
					parts = append(parts, t)
				}
			}
		}
		return strings.Join(parts, " ")
	case List:
		return notifyList(b, st)
	case ImageBlock:
		if b.Image != nil {
			return b.Image.Title
		}
	case Table:
		rows := make([]string, 0, len(b.Rows))
		for _, row := range b.Rows {
			cells := make([]string, 0, len(row))
			for _, cell := range row {
				cells = append(cells, spansText(cell, st))
			}
			rows = append(rows, strings.Join(cells, ", "))
		}
		return strings.Join(rows, "\n")
	case AttachmentBlock: // a video block: convertVideoBlockToPlainText
		if b.Att != nil && b.Att.VideoURL != "" {
			return spansText(b.Att.Title, st)
		}
	case ContainerBlock: // convertPlainTextTitledBlockToPlainText
		if b.Container != nil {
			return spansText(b.Container.Title, st)
		}
	case ShortcutBlock:
		if b.Shortcut != nil {
			return b.Shortcut.Title
		}
	}
	return ""
}

// notifyList is convertRichTextBlockToPlainText's list in the default
// verbosity: each item "<marker> <text>." with no indent, the marker by
// the list's indent (getUnorderedListCharacter: •, ◦, ■ in turn;
// getOrderedListCharacter: 1., a., i. in turn, counted from offset+1). A
// check list is an unordered list here.
func notifyList(b *Block, st SpanText) string {
	lines := make([]string, 0, len(b.Items))
	for i, it := range b.Items {
		text := spansText(it, st) + "."
		if b.Ordered {
			lines = append(lines, orderedMarker(b.Indent, ListNumber(b, i))+". "+text)
			continue
		}
		lines = append(lines, []string{"\u2022", "\u25e6", "\u25a0"}[b.Indent%3]+" "+text)
	}
	return strings.Join(lines, "\n")
}

// orderedMarker is getOrderedListCharacter: n, a letter run
// (convertNumberToLetterIncrement) or a roman numeral by indent. The
// roman numeral is CSS lower-roman's (LowerRoman: decimal above 3999, as
// the list itself draws), not convertNumberToRomanNumeral's run of one
// "m" per thousand, which a sender's offset turns into gigabytes.
func orderedMarker(indent, n int) string {
	switch indent % 3 {
	case 1:
		s := ""
		for ; n > 0; n /= 26 {
			n--
			s = string(rune('a'+n%26)) + s
		}
		return s
	case 2:
		return LowerRoman(n)
	}
	return strconv.Itoa(n)
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
