package richview

import (
	"sync"

	"github.com/some-du6e/clack/internal/richtext"
)

// Attachment text collapses like Slack's (2026-11 client):
// WithTextTruncation cuts a legacy attachment's text at 300 characters
// or 5 line breaks and appends " Show more" (c-message_attachment__text_
// expander); expanded, "Show less" sits on its own line. Shared /
// forwarded message attachments use TruncatedBlocks' 700 characters / 5
// lines. Message bodies in the message pane never collapse (Slack only
// passes truncateLimits to previews and unfurls).
const (
	attMaxChars      = 300
	attMaxNewlines   = 5
	shareMaxChars    = 700
	shareMaxNewlines = 5
)

// truncateBlocks cuts bs to maxChars runes or maxNewlines line breaks;
// cut reports whether anything was dropped.
func truncateBlocks(bs []richtext.Block, maxChars, maxNewlines int) (out []richtext.Block, cut bool) {
	chars, nl := 0, 0
	// spans keeps spans until the budget runs out; ok=false means stop.
	spans := func(ss []richtext.Span) ([]richtext.Span, bool) {
		var keep []richtext.Span
		for _, sp := range ss {
			if sp.Kind == richtext.Break {
				nl++
				if nl >= maxNewlines {
					return keep, false
				}
				keep = append(keep, sp)
				continue
			}
			n := len([]rune(sp.Text))
			if sp.Kind == richtext.Emoji || sp.Kind == richtext.InlineImage {
				n = 1
			}
			if chars+n > maxChars {
				if sp.Kind == richtext.Text {
					r := []rune(sp.Text)
					sp.Text = string(r[:max(0, maxChars-chars)]) + "…"
					keep = append(keep, sp)
				}
				return keep, false
			}
			chars += n
			keep = append(keep, sp)
		}
		return keep, true
	}
	for i, b := range bs {
		if i > 0 {
			nl++
			if nl >= maxNewlines {
				return out, true
			}
		}
		switch b.Kind {
		case richtext.List:
			nb := b
			nb.Items = nil
			for j, it := range b.Items {
				if j > 0 {
					nl++
					if nl >= maxNewlines {
						return append(out, nb), true
					}
				}
				ks, ok := spans(it)
				nb.Items = append(nb.Items, ks)
				if !ok {
					return append(out, nb), true
				}
			}
			out = append(out, nb)
		case richtext.Paragraph, richtext.Quote, richtext.Preformatted, richtext.Header, richtext.Context, richtext.Section:
			if len(b.Children) > 0 {
				ch, c := truncateBlocks(b.Children, maxChars-chars, maxNewlines-nl)
				nb := b
				nb.Children = ch
				out = append(out, nb)
				if c {
					return out, true
				}
				continue
			}
			ks, ok := spans(b.Spans)
			nb := b
			nb.Spans = ks
			out = append(out, nb)
			if !ok {
				return out, true
			}
		default:
			out = append(out, b)
		}
	}
	return out, false
}

// withExpander adds the " Show more" / "Show less" link to text blocks.
func withExpander(bs []richtext.Block, more bool) []richtext.Block {
	label := "Show less"
	if more {
		label = "Show more"
	}
	link := richtext.Span{Kind: richtext.Link, Text: label, URL: expandURL}
	if more && len(bs) > 0 {
		last := &bs[len(bs)-1]
		switch last.Kind {
		case richtext.Paragraph, richtext.Section, richtext.Quote, richtext.Context:
			if len(last.Children) == 0 {
				last.Spans = append(append(last.Spans[:len(last.Spans):len(last.Spans)], richtext.Span{Kind: richtext.Text, Text: " "}), link)
				return bs
			}
		}
	}
	return append(bs, richtext.Block{Kind: richtext.Paragraph, Spans: []richtext.Span{link}})
}

// Block Kit section text truncates the same way (WithTextTruncation:
// maxCharacters 300, maxNewlines 5) where Options.Truncate is set: the
// section's text and each of its fields on their own, all expanded
// together. Slack keeps the state in its expandables store under
// "blockText" + message ts + block_id for the session; kitExpanded is
// that store.
var (
	kitExpMu    sync.Mutex
	kitExpanded = map[string]bool{}
)

func kitIsExpanded(key string) bool {
	kitExpMu.Lock()
	defer kitExpMu.Unlock()
	return kitExpanded[key]
}

func setKitExpanded(key string, on bool) {
	kitExpMu.Lock()
	defer kitExpMu.Unlock()
	if on {
		kitExpanded[key] = true
	} else {
		delete(kitExpanded, key)
	}
}

// kitTruncKey is the expandables key of section b in this layout ("" =
// the section doesn't truncate here).
func (l *lay) kitTruncKey(b *richtext.Block) string {
	if !l.opts.Truncate || !b.Kit || b.Expand || l.inAtt {
		return ""
	}
	id := b.BlockID
	if id == "" {
		id = "\x00" // Slack falls back to the block index; one per message is enough here
	}
	return "blockText/" + l.opts.Channel + "/" + l.opts.TS + "/" + id
}

// kitTruncate is section text (or one field) as Slack shows it: cut with
// " Show more" while key isn't expanded, "Show less" after it when it is.
func kitTruncate(bs []richtext.Block, key string) []richtext.Block {
	short, cut := truncateBlocks(bs, attMaxChars, attMaxNewlines)
	if !cut {
		return bs
	}
	if kitIsExpanded(key) {
		return withExpander(append([]richtext.Block(nil), bs...), false)
	}
	return withExpander(short, true)
}

// expandURL marks the expander link (never a real URL).
const expandURL = "clack-expand:"
