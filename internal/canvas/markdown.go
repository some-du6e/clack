package canvas

import (
	"strconv"
	"strings"

	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
)

// Markdown is a block as the canvas markdown Slack's canvases.create /
// canvases.edit take (docs.slack.dev "Canvas markdown"): # headings, - / 1.
// / - [ ] lists indented two spaces per level, > quotes, ``` code, ---,
// pipe tables, ![](@U…) and ![](#C…) mentions, :emoji:, **bold**, _italic_,
// ~strike~, `code` and [text](url) links.
func Markdown(b Block) string {
	var w strings.Builder
	writeBlock(&w, b, "")
	return strings.TrimRight(w.String(), "\n")
}

// DocMarkdown is the whole document as canvas markdown.
func DocMarkdown(d Doc) string {
	var w strings.Builder
	for i, b := range d.Blocks {
		if i > 0 && !(b.Kind.IsList() && d.Blocks[i-1].Kind.IsList()) {
			w.WriteByte('\n')
		}
		writeBlock(&w, b, "")
	}
	return strings.TrimRight(w.String(), "\n")
}

func writeBlock(w *strings.Builder, b Block, prefix string) {
	line := func(s string) {
		w.WriteString(prefix)
		w.WriteString(s)
		w.WriteByte('\n')
	}
	switch b.Kind {
	case Heading1:
		line("# " + Inline(b.Spans))
	case Heading2:
		line("## " + Inline(b.Spans))
	case Heading3:
		line("### " + Inline(b.Spans))
	case Bullet, Numbered, Check:
		ind := strings.Repeat("  ", max(0, b.Level))
		mark := "- "
		switch b.Kind {
		case Numbered:
			mark = strconv.Itoa(max(1, b.Number)) + ". "
		case Check:
			mark = "- [ ] "
			if b.Checked {
				mark = "- [x] "
			}
		}
		line(ind + mark + Inline(b.Spans))
	case Quote:
		if len(b.Children) == 0 {
			line("> " + Inline(b.Spans))
			return
		}
		for i, c := range b.Children {
			if i > 0 && !(c.Kind.IsList() && b.Children[i-1].Kind.IsList()) {
				line(">")
			}
			writeBlock(w, c, prefix+"> ")
		}
	case Callout:
		for _, c := range b.Children {
			writeBlock(w, c, prefix+"> ")
		}
	case Code:
		line("```")
		for _, l := range strings.Split(b.Code, "\n") {
			line(l)
		}
		line("```")
	case Divider:
		line("---")
	case Table:
		cols := 0
		for _, r := range b.Rows {
			cols = max(cols, len(r))
		}
		if cols == 0 {
			return
		}
		row := func(r []Cell) {
			var cells []string
			for i := 0; i < cols; i++ {
				t := ""
				if i < len(r) {
					var parts []string
					for _, cb := range r[i].Blocks {
						parts = append(parts, strings.ReplaceAll(Inline(cb.Spans), "|", `\|`))
					}
					t = strings.Join(parts, "<br>")
				}
				cells = append(cells, t)
			}
			line("| " + strings.Join(cells, " | ") + " |")
		}
		rows := b.Rows
		if b.Header && len(rows) > 0 {
			row(rows[0])
			rows = rows[1:]
		} else {
			row(make([]Cell, cols))
		}
		line("|" + strings.Repeat(" --- |", cols))
		for _, r := range rows {
			row(r)
		}
	case Image:
		if b.Image != nil && strings.HasPrefix(b.Image.URL, "https://") {
			line("![" + escape(b.Image.Alt) + "](" + b.Image.URL + ")")
		}
	case Embed:
		if b.URL != "" {
			line(b.URL)
		}
	case File:
		if b.Title != "" {
			line(escape(b.Title))
		}
	case Columns:
		for _, col := range b.Columns {
			for _, c := range col {
				writeBlock(w, c, prefix)
			}
		}
	default:
		t := Inline(b.Spans)
		if t == "" {
			line("")
			return
		}
		for _, l := range strings.Split(t, "\n") {
			line(l)
		}
	}
}

// Inline is spans as canvas inline markdown.
func Inline(spans []richtext.Span) string {
	var w strings.Builder
	for _, s := range spans {
		var t string
		switch s.Kind {
		case richtext.Break:
			w.WriteString("\n")
			continue
		case richtext.User:
			if s.ID != "" {
				w.WriteString("![](@" + s.ID + ")")
				continue
			}
			t = escape(s.Text)
		case richtext.Channel:
			if s.ID != "" {
				w.WriteString("![](#" + s.ID + ")")
				continue
			}
			t = escape(s.Text)
		case richtext.Broadcast:
			w.WriteString("@" + firstNonEmpty(s.ID, strings.TrimPrefix(s.Text, "@")))
			continue
		case richtext.Emoji:
			t = ":" + s.ID + ":"
		case richtext.Link:
			label := escape(s.Text)
			if label == "" || s.Text == s.URL {
				t = s.URL
			} else {
				t = "[" + label + "](" + s.URL + ")"
			}
		case richtext.InlineImage:
			t = "![" + escape(s.Text) + "](" + s.URL + ")"
		default:
			if s.Style&richtext.Code != 0 {
				t = s.Text
			} else {
				t = escape(s.Text)
			}
		}
		w.WriteString(wrapStyle(t, s.Style))
	}
	return escapeLead(w.String())
}

// wrapStyle puts markers around t, keeping its edge spaces outside them
// (markdown ignores "** x **").
func wrapStyle(t string, st richtext.Style) string {
	if st == 0 || strings.TrimSpace(t) == "" {
		return t
	}
	lead := len(t) - len(strings.TrimLeft(t, " "))
	trail := len(t) - len(strings.TrimRight(t, " "))
	core := t[lead : len(t)-trail]
	if st&richtext.Code != 0 {
		core = "`" + core + "`"
	}
	if st&richtext.Strike != 0 {
		core = "~" + core + "~"
	}
	if st&richtext.Italic != 0 {
		core = "_" + core + "_"
	}
	if st&richtext.Bold != 0 {
		core = "**" + core + "**"
	}
	return t[:lead] + core + t[len(t)-trail:]
}

var mdEscaper = strings.NewReplacer(`\`, `\\`, `*`, `\*`, `_`, `\_`, `~`, `\~`, "`", "\\`", `[`, `\[`, `]`, `\]`)

func escape(s string) string { return mdEscaper.Replace(s) }

// escapeLead keeps a line that starts like a block marker a paragraph.
func escapeLead(s string) string {
	if strings.HasPrefix(s, "#") || strings.HasPrefix(s, ">") || strings.HasPrefix(s, "- ") ||
		strings.HasPrefix(s, "+ ") || strings.HasPrefix(s, "---") {
		s = `\` + s
	}
	if i := strings.IndexByte(s, '.'); i > 0 && i < 10 && strings.HasPrefix(s[i:], ". ") {
		if _, err := strconv.Atoi(s[:i]); err == nil {
			s = s[:i] + `\` + s[i:]
		}
	}
	return s
}
