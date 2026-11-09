package canvas

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
)

// Names resolves mention ids while reading markdown (nil: ids only).
type Names interface {
	UserName(id string) string
	ChannelName(id string) string
}

// ParseMarkdown reads canvas markdown (what Markdown writes, and what
// canvases.edit receives) into blocks without section ids. Clack uses it
// for the fake server and for the editor's markdown shortcuts.
func ParseMarkdown(src string, names Names) []Block {
	lines := strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n")
	var out []Block
	counts := map[int]int{} // numbered counters per level
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		t := strings.TrimSpace(l)
		switch {
		case t == "":
			continue
		case strings.HasPrefix(t, "```"):
			var code []string
			for i++; i < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[i]), "```"); i++ {
				code = append(code, lines[i])
			}
			out = append(out, Block{Kind: Code, Code: strings.Join(code, "\n")})
			continue
		case t == "---" || t == "***" || t == "___":
			out = append(out, Block{Kind: Divider})
			continue
		case strings.HasPrefix(t, ">"):
			var inner []string
			for ; i < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i]), ">"); i++ {
				s := strings.TrimPrefix(strings.TrimSpace(lines[i]), ">")
				inner = append(inner, strings.TrimPrefix(s, " "))
			}
			i--
			out = append(out, Block{Kind: Quote, Children: ParseMarkdown(strings.Join(inner, "\n"), names)})
			continue
		case strings.HasPrefix(t, "|"):
			var rows [][]Cell
			header := false
			for ; i < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i]), "|"); i++ {
				r := strings.TrimSpace(lines[i])
				if sepRE.MatchString(r) {
					header = len(rows) == 1
					continue
				}
				var row []Cell
				for _, c := range splitRow(r) {
					row = append(row, Cell{Blocks: []Block{{Kind: Paragraph, Spans: ParseInline(strings.ReplaceAll(c, "<br>", "\n"), names)}}})
				}
				rows = append(rows, row)
			}
			i--
			if header && len(rows) > 0 && allEmpty(rows[0]) {
				rows, header = rows[1:], false
			}
			out = append(out, Block{Kind: Table, Rows: rows, Header: header})
			continue
		}
		level := indentLevel(l)
		switch {
		case strings.HasPrefix(t, "### "):
			out = append(out, Block{Kind: Heading3, Spans: ParseInline(t[4:], names)})
		case strings.HasPrefix(t, "## "):
			out = append(out, Block{Kind: Heading2, Spans: ParseInline(t[3:], names)})
		case strings.HasPrefix(t, "# "):
			out = append(out, Block{Kind: Heading1, Spans: ParseInline(t[2:], names)})
		case checkRE.MatchString(t):
			m := checkRE.FindStringSubmatch(t)
			out = append(out, Block{Kind: Check, Level: level, Checked: m[1] == "x" || m[1] == "X", Spans: ParseInline(m[2], names)})
		case strings.HasPrefix(t, "- ") || strings.HasPrefix(t, "* ") || strings.HasPrefix(t, "+ "):
			out = append(out, Block{Kind: Bullet, Level: level, Spans: ParseInline(t[2:], names)})
		case numRE.MatchString(t):
			m := numRE.FindStringSubmatch(t)
			n, _ := strconv.Atoi(m[1])
			if prev := lastNumbered(out, level); prev == nil {
				counts[level] = n
			} else {
				counts[level]++
			}
			out = append(out, Block{Kind: Numbered, Level: level, Number: counts[level], Spans: ParseInline(m[2], names)})
		case imgRE.MatchString(t):
			m := imgRE.FindStringSubmatch(t)
			out = append(out, Block{Kind: Image, Image: &Img{Alt: m[1], URL: m[2]}})
		default:
			out = append(out, Block{Kind: Paragraph, Spans: ParseInline(t, names)})
		}
	}
	return out
}

// lastNumbered returns the numbered item a new one at level continues: the
// nearest earlier block that is a list item at level (deeper items in
// between don't break the run; anything else does).
func lastNumbered(bs []Block, level int) *Block {
	for i := len(bs) - 1; i >= 0; i-- {
		b := &bs[i]
		if !b.Kind.IsList() || b.Level < level {
			return nil
		}
		if b.Level == level {
			if b.Kind == Numbered {
				return b
			}
			return nil
		}
	}
	return nil
}

var (
	checkRE = regexp.MustCompile(`^[-*+] \[([ xX])\] ?(.*)$`)
	numRE   = regexp.MustCompile(`^(\d{1,9})[.)] (.*)$`)
	imgRE   = regexp.MustCompile(`^!\[([^\]]*)\]\((https://[^)\s]+)\)$`)
	sepRE   = regexp.MustCompile(`^\|(\s*:?-{3,}:?\s*\|)+$`)
)

func indentLevel(l string) int {
	n := 0
	for _, r := range l {
		switch r {
		case ' ':
			n++
		case '\t':
			n += 2
		default:
			return min(n/2, 8)
		}
	}
	return min(n/2, 8)
}

func splitRow(r string) []string {
	r = strings.TrimSpace(r)
	r = strings.TrimPrefix(r, "|")
	r = strings.TrimSuffix(r, "|")
	var cells []string
	var cur strings.Builder
	for i := 0; i < len(r); i++ {
		if r[i] == '\\' && i+1 < len(r) && r[i+1] == '|' {
			cur.WriteByte('|')
			i++
			continue
		}
		if r[i] == '|' {
			cells = append(cells, strings.TrimSpace(cur.String()))
			cur.Reset()
			continue
		}
		cur.WriteByte(r[i])
	}
	return append(cells, strings.TrimSpace(cur.String()))
}

func allEmpty(row []Cell) bool {
	for _, c := range row {
		for _, b := range c.Blocks {
			if len(b.Spans) > 0 {
				return false
			}
		}
	}
	return true
}

var (
	mentionRE = regexp.MustCompile(`^!\[\]\(([@#])([A-Z0-9]{1,24})\)`)
	linkRE    = regexp.MustCompile(`^\[((?:\\.|[^\]\\])*)\]\(([^)\s]+)\)`)
	inlImgRE  = regexp.MustCompile(`^!\[((?:\\.|[^\]\\])*)\]\((https://[^)\s]+)\)`)
	emojiRE   = regexp.MustCompile(`^:([a-z0-9_+'-]+(?:::skin-tone-[2-6])?):`)
	urlRE     = regexp.MustCompile(`^https?://[^\s<>()]+`)
	bcastRE   = regexp.MustCompile(`^@(here|channel|everyone)\b`)
)

// ParseInline reads canvas inline markdown.
func ParseInline(s string, names Names) []richtext.Span {
	var out []richtext.Span
	var st richtext.Style
	var text strings.Builder
	flush := func() {
		if text.Len() > 0 {
			out = append(out, richtext.Span{Kind: richtext.Text, Text: text.String(), Style: st})
			text.Reset()
		}
	}
	toggle := func(f richtext.Style) {
		flush()
		st ^= f
	}
	atWordStart := func(i int) bool { return i == 0 || s[i-1] == ' ' || strings.ContainsRune("*_~(", rune(s[i-1])) }
	for i := 0; i < len(s); {
		rest := s[i:]
		if st&richtext.Code != 0 && rest[0] != '`' {
			text.WriteByte(rest[0])
			i++
			continue
		}
		switch {
		case rest[0] == '\\' && len(rest) > 1:
			text.WriteByte(rest[1])
			i += 2
			continue
		case rest[0] == '\n':
			flush()
			out = append(out, richtext.Span{Kind: richtext.Break})
			i++
			continue
		case rest[0] == '`':
			toggle(richtext.Code)
			i++
			continue
		case strings.HasPrefix(rest, "**"):
			toggle(richtext.Bold)
			i += 2
			continue
		case strings.HasPrefix(rest, "~~"):
			toggle(richtext.Strike)
			i += 2
			continue
		case rest[0] == '~':
			toggle(richtext.Strike)
			i++
			continue
		case (rest[0] == '_' || rest[0] == '*') && (st&richtext.Italic != 0 || atWordStart(i)):
			toggle(richtext.Italic)
			i++
			continue
		}
		if m := mentionRE.FindStringSubmatch(rest); m != nil {
			flush()
			sp := richtext.Span{ID: m[2], Style: st}
			if m[1] == "@" {
				sp.Kind, sp.Text = richtext.User, "@"+m[2]
				if names != nil {
					if n := names.UserName(m[2]); n != "" {
						sp.Text = "@" + n
					}
				}
			} else {
				sp.Kind, sp.Text = richtext.Channel, "#"+m[2]
				if names != nil {
					if n := names.ChannelName(m[2]); n != "" {
						sp.Text = "#" + n
					}
				}
			}
			out = append(out, sp)
			i += len(m[0])
			continue
		}
		if m := inlImgRE.FindStringSubmatch(rest); m != nil {
			flush()
			out = append(out, richtext.Span{Kind: richtext.InlineImage, URL: m[2], Text: unescape(m[1])})
			i += len(m[0])
			continue
		}
		if m := linkRE.FindStringSubmatch(rest); m != nil {
			if u := safeURL(m[2]); u != "" {
				flush()
				out = append(out, richtext.Span{Kind: richtext.Link, Text: unescape(m[1]), URL: u, Style: st})
				i += len(m[0])
				continue
			}
		}
		if m := emojiRE.FindStringSubmatch(rest); m != nil {
			flush()
			out = append(out, richtext.Span{Kind: richtext.Emoji, ID: m[1], Style: st})
			i += len(m[0])
			continue
		}
		if atWordStart(i) {
			if m := bcastRE.FindStringSubmatch(rest); m != nil {
				flush()
				out = append(out, richtext.Span{Kind: richtext.Broadcast, ID: m[1], Text: "@" + m[1], Self: true, Style: st})
				i += len(m[0])
				continue
			}
			if m := urlRE.FindString(rest); m != "" {
				m = strings.TrimRight(m, ".,;:!?")
				flush()
				out = append(out, richtext.Span{Kind: richtext.Link, Text: m, URL: m, Style: st})
				i += len(m)
				continue
			}
		}
		text.WriteByte(rest[0])
		i++
	}
	flush()
	return merge(out)
}

func unescape(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
