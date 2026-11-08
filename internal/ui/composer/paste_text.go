package composer

import (
	"sort"
	"strings"
)

// docFromPlainText turns pasted plain text into a Doc when it holds a
// list: lines starting with "- ", "* ", "+ ", "•", "◦", "▪", "‣", "·"
// or "1." / "1)" become list items, nested by their leading indentation.
// atLineStart says whether the paste starts a line; if not, the first
// line merges into the current one and is never a list item. It returns
// nil when no line is a list item.
func docFromPlainText(s string, atLineStart bool) *Doc {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if !strings.ContainsAny(s, "-*+•◦▪‣·⁃0123456789") || len(s) > phMaxHTML {
		return nil
	}
	lines := strings.Split(s, "\n")
	type item struct {
		kind  BlockKind
		width int
		text  string
	}
	items := make([]item, len(lines))
	var widths []int
	any := false
	for i, l := range lines {
		items[i] = item{kind: Para, text: l}
		if i == 0 && !atLineStart {
			continue
		}
		k, w, rest, ok := ptListLine(l)
		if !ok {
			continue
		}
		items[i] = item{kind: k, width: w, text: rest}
		widths = append(widths, w)
		any = true
	}
	if !any {
		return nil
	}
	// Indent levels: the distinct leading widths, in order (so two-space
	// and four-space nesting both work).
	sort.Ints(widths)
	level := map[int]int{}
	for _, w := range widths {
		if _, ok := level[w]; !ok {
			level[w] = min(len(level), edMaxIndent)
		}
	}
	b := rtNewBuilder()
	for _, it := range items {
		l := Line{Kind: it.kind}
		if rtIsList(it.kind) {
			l.Indent = level[it.width]
		}
		b.line(l)
		b.text(it.text, Attr{})
	}
	d := b.d
	if len(d.Runes) > phMaxRunes {
		return nil
	}
	return d
}

// ptListLine parses one plain-text list line: its kind, the width of its
// leading indentation (a tab counts four), and the text after the marker.
func ptListLine(l string) (BlockKind, int, string, bool) {
	w, i := 0, 0
	for i < len(l) {
		switch l[i] {
		case ' ':
			w++
		case '\t':
			w += 4
		default:
			goto marker
		}
		i++
	}
	return 0, 0, "", false
marker:
	rest := l[i:]
	rs := []rune(rest)
	if len(rs) < 2 {
		return 0, 0, "", false
	}
	var kind BlockKind
	n := 0 // marker length in runes
	switch rs[0] {
	case '-', '*', '+':
		kind, n = Bullet, 1
		if !ptSpace(rs[1]) {
			return 0, 0, "", false
		}
	case '•', '◦', '▪', '‣', '·', '⁃', '▫', '○', '■', '□':
		kind, n = Bullet, 1
	default:
		for n < len(rs) && n < 3 && rs[n] >= '0' && rs[n] <= '9' {
			n++
		}
		if n == 0 || n+1 >= len(rs) || (rs[n] != '.' && rs[n] != ')') || !ptSpace(rs[n+1]) {
			return 0, 0, "", false
		}
		kind, n = Ordered, n+1
	}
	text := strings.TrimLeftFunc(string(rs[n:]), ptSpace)
	if text == "" {
		return 0, 0, "", false
	}
	return kind, w, text, true
}

func ptSpace(r rune) bool { return r == ' ' || r == '\t' || r == ' ' }

// clipPickHTMLType picks the HTML target from an X11 TARGETS or Wayland
// MIME type list, or "".
func clipPickHTMLType(types []string) string {
	best := ""
	for _, t := range types {
		l := strings.ToLower(strings.TrimSpace(t))
		switch {
		case l == "text/html":
			return t
		case best == "" && strings.HasPrefix(l, "text/html;"):
			best = t
		}
	}
	return best
}

// Mac pasteboard type kinds, best first.
const (
	macNone = iota
	macHTML
	macRTF
	macRTFD
)

// clipPickMacType picks the pasteboard type to read on macOS: HTML
// (browsers, Notes, Word), else RTF (TextEdit, Pages), which AppKit
// converts to HTML.
func clipPickMacType(types []string) (string, int) {
	pick, kind := "", macNone
	for _, t := range types {
		k := macNone
		switch t {
		case "public.html", "Apple HTML pasteboard type":
			k = macHTML
		case "public.rtf", "NeXT Rich Text Format v1.0 pasteboard type":
			k = macRTF
		case "com.apple.flat-rtfd", "NeXT RTFD pasteboard type":
			k = macRTFD
		}
		if k != macNone && (kind == macNone || k < kind) {
			pick, kind = t, k
		}
	}
	return pick, kind
}
