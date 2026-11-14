package richtext

import (
	"strconv"
	"strings"
)

// PlainText flattens a document to text (copy, previews, notifications).
// Mentions keep their display text ("@alice", "#general"), links their label,
// emoji their unicode (custom emoji ":name:"); blocks are separated by
// newlines, quotes are prefixed with "> " and list items with "• "/"1. ".
func PlainText(d Doc) string { return PlainTextWith(d, nil) }

// SpanText decides a span's text for PlainTextWith: ok false keeps the
// span's own.
type SpanText func(s Span) (text string, ok bool)

// PlainTextWith is PlainText with text deciding the spans' text where it
// answers (a notification's channel mentions, written the way Slack's
// message-to-text pass writes them). A nil text is PlainText.
func PlainTextWith(d Doc, text SpanText) string {
	var b strings.Builder
	writeBlocks(&b, d.Blocks, text)
	return strings.TrimRight(b.String(), "\n")
}

func writeBlocks(b *strings.Builder, bs []Block, text SpanText) {
	for i := range bs {
		writeBlock(b, &bs[i], text)
	}
}

func line(b *strings.Builder, s string) {
	if s == "" {
		return
	}
	b.WriteString(s)
	b.WriteByte('\n')
}

func writeBlock(b *strings.Builder, blk *Block, text SpanText) {
	switch blk.Kind {
	case Quote:
		for _, ln := range strings.Split(spansText(blk.Spans, text), "\n") {
			b.WriteString("> " + ln + "\n")
		}
	case List:
		for i, it := range blk.Items {
			bullet := "• "
			if blk.Ordered {
				bullet = strconv.Itoa(blk.Offset+i+1) + ". "
			}
			b.WriteString(strings.Repeat("    ", blk.Indent) + bullet + spansText(it, text) + "\n")
		}
	case Table:
		for _, row := range blk.Rows {
			cells := make([]string, 0, len(row))
			for _, cell := range row {
				cells = append(cells, strings.ReplaceAll(spansText(cell, text), "\n", " "))
			}
			line(b, strings.TrimRight(strings.Join(cells, "\t"), "\t")) // trailing empty cells add nothing (decision 2026-11-07: trim)
		}
	case Divider:
	case ImageBlock:
		if blk.Image != nil {
			line(b, firstNonEmpty(blk.Image.Title, blk.Image.Alt))
		}
	case Actions:
	case FileBlock:
		if f := blk.File; f != nil {
			line(b, firstNonEmpty(f.Title, f.Name))
		}
	case AttachmentBlock:
		if a := blk.Att; a != nil {
			line(b, spansText(a.Pretext, text))
			if a.IsShare {
				line(b, a.AuthorName)
			}
			line(b, spansText(a.Title, text))
			writeBlocks(b, a.Text, text)
		}
		for _, f := range blk.Fields {
			line(b, spansText(f, text))
		}
		writeBlocks(b, blk.Children, text)
	default: // Paragraph, Preformatted, Header, Context, Section, Tombstone
		line(b, spansText(blk.Spans, text))
		writeBlocks(b, blk.Children, text)
		for _, f := range blk.Fields {
			line(b, spansText(f, text))
		}
	}
}

// SpansText joins the display text of spans; Break becomes "\n".
func SpansText(ss []Span) string { return spansText(ss, nil) }

func spansText(ss []Span, text SpanText) string {
	var b strings.Builder
	for _, s := range ss {
		switch s.Kind {
		case Break:
			b.WriteByte('\n')
		case InlineImage:
			// decorative
		default:
			if text != nil {
				if t, ok := text(s); ok {
					b.WriteString(t)
					continue
				}
			}
			b.WriteString(s.Text)
		}
	}
	return b.String()
}
