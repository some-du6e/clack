package richtext

import (
	"strconv"
	"strings"
)

// PlainText flattens a document to text (copy, previews, notifications).
// Mentions keep their display text ("@alice", "#general"), links their label,
// emoji their unicode (custom emoji ":name:"); blocks are separated by
// newlines, quotes are prefixed with "> " and list items with "• "/"1. ".
func PlainText(d Doc) string {
	var b strings.Builder
	writeBlocks(&b, d.Blocks)
	return strings.TrimRight(b.String(), "\n")
}

func writeBlocks(b *strings.Builder, bs []Block) {
	for i := range bs {
		writeBlock(b, &bs[i])
	}
}

func line(b *strings.Builder, s string) {
	if s == "" {
		return
	}
	b.WriteString(s)
	b.WriteByte('\n')
}

func writeBlock(b *strings.Builder, blk *Block) {
	switch blk.Kind {
	case Quote:
		for _, ln := range strings.Split(SpansText(blk.Spans), "\n") {
			b.WriteString("> " + ln + "\n")
		}
	case List:
		for i, it := range blk.Items {
			bullet := "• "
			if blk.Ordered {
				bullet = strconv.Itoa(blk.Offset+i+1) + ". "
			}
			b.WriteString(strings.Repeat("    ", blk.Indent) + bullet + SpansText(it) + "\n")
		}
	case Table:
		for _, row := range blk.Rows {
			cells := make([]string, 0, len(row))
			for _, cell := range row {
				cells = append(cells, strings.ReplaceAll(SpansText(cell), "\n", " "))
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
			line(b, SpansText(a.Pretext))
			if a.IsShare {
				line(b, a.AuthorName)
			}
			line(b, SpansText(a.Title))
			writeBlocks(b, a.Text)
		}
		for _, f := range blk.Fields {
			line(b, SpansText(f))
		}
		writeBlocks(b, blk.Children)
	default: // Paragraph, Preformatted, Header, Context, Section, Tombstone
		line(b, SpansText(blk.Spans))
		writeBlocks(b, blk.Children)
		for _, f := range blk.Fields {
			line(b, SpansText(f))
		}
	}
}

// SpansText joins the display text of spans; Break becomes "\n".
func SpansText(ss []Span) string {
	var b strings.Builder
	for _, s := range ss {
		switch s.Kind {
		case Break:
			b.WriteByte('\n')
		case InlineImage:
			// decorative
		default:
			b.WriteString(s.Text)
		}
	}
	return b.String()
}
