package canvas

import (
	"strings"

	"github.com/some-du6e/clack/internal/richtext"
)

// Editable reports whether b can be saved back through canvases.edit
// without losing anything: its kind is one the canvas markdown writes, and
// every inline round-trips (Markdown, then ParseMarkdown, gives the same
// spans). Underline and highlight have no canvas markdown; dates, inline
// images, @here and user groups have no documented form; a mention whose
// id the export didn't carry (and Clack couldn't resolve) would save as
// plain text; hard line breaks inside a block have no documented form
// either. why says what is in the way (for the UI's feedback).
func Editable(b Block) (ok bool, why string) {
	switch b.Kind {
	case Paragraph, Heading1, Heading2, Heading3, Bullet, Numbered, Check:
	case Code:
		if strings.Contains(b.Code, "```") {
			return false, "a code block containing ```"
		}
		got := ParseMarkdown(Markdown(b), nil)
		if len(got) != 1 || got[0].Kind != Code || got[0].Code != b.Code {
			return false, "this code block"
		}
		return true, ""
	default:
		return false, "this kind of block"
	}
	for _, s := range b.Spans {
		switch s.Kind {
		case richtext.Text, richtext.Link, richtext.Emoji:
		case richtext.User, richtext.Channel:
			if s.ID == "" {
				return false, "a mention Clack can't match to a person or channel"
			}
		case richtext.Break:
			return false, "a line break"
		case richtext.Date:
			return false, "a date"
		case richtext.Broadcast:
			return false, "an @" + firstNonEmpty(s.ID, "here") + " mention"
		default:
			return false, "an inline object"
		}
		if s.Style&richtext.Underline != 0 {
			return false, "underlined text"
		}
		if s.Style&richtext.Highlight != 0 {
			return false, "highlighted text"
		}
		if s.Kind == richtext.Link && s.Style&richtext.Code != 0 {
			return false, "a link styled as code"
		}
	}
	if b.Kind == Paragraph && len(trimEdges(b.Spans)) == 0 {
		return true, "" // an empty line
	}
	got := ParseMarkdown(Markdown(b), nil)
	if len(got) != 1 {
		return false, "this text"
	}
	g := got[0]
	if g.Kind != b.Kind || (b.Kind.IsList() && (g.Level != b.Level || g.Checked != b.Checked)) ||
		(b.Kind == Numbered && g.Number != max(1, b.Number)) {
		return false, "this text"
	}
	if !sameInline(trimEdges(merge(append([]richtext.Span(nil), b.Spans...))), trimEdges(g.Spans)) {
		return false, "formatting Clack can't save yet"
	}
	return true, ""
}

// trimEdges drops leading and trailing blank space (markdown lines are
// trimmed; it doesn't show).
func trimEdges(sp []richtext.Span) []richtext.Span {
	out := append([]richtext.Span(nil), sp...)
	for len(out) > 0 && out[0].Kind == richtext.Text {
		out[0].Text = strings.TrimLeft(out[0].Text, " \t ")
		if out[0].Text != "" {
			break
		}
		out = out[1:]
	}
	for len(out) > 0 && out[len(out)-1].Kind == richtext.Text {
		l := &out[len(out)-1]
		l.Text = strings.TrimRight(l.Text, " \t ")
		if l.Text != "" {
			break
		}
		out = out[:len(out)-1]
	}
	return out
}

// sameInline compares what a reader sees: text and links by text, style
// and target; mentions and emoji by id.
func sameInline(a, b []richtext.Span) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		x, y := a[i], b[i]
		if x.Kind != y.Kind || x.Style != y.Style {
			return false
		}
		switch x.Kind {
		case richtext.User, richtext.Channel, richtext.Emoji:
			if x.ID != y.ID {
				return false
			}
		default:
			if x.Text != y.Text || x.URL != y.URL {
				return false
			}
		}
	}
	return true
}
