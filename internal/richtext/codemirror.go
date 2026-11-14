package richtext

import (
	"regexp"
	"strings"
	"unicode/utf16"

	xhtml "golang.org/x/net/html"
)

// Snippets' highlighted code. Slack's server sends a snippet's first lines
// as CodeMirror's static highlight (preview_highlight) and files.info its
// whole content the same way (content_highlight_html):
//
//	<div class="CodeMirror cm-s-default CodeMirrorServer" oncopy="…">
//	<div class="CodeMirror-code">
//	<div><pre><span class="cm-keyword">def</span> <span class="cm-def">fib</span>(n):</pre></div>
//	…
//	</div>
//	</div>
//
// FileSnippet (boot-shared MT+a) puts that HTML in the page as it is
// (dangerouslySetInnerHTML) under the client theme's CodeMirror class;
// the theme's .cm-* rules colour the spans (richview snippet.go). Clack
// never renders the HTML: it reads the lines and their spans' classes.

// CodeMirror is a parsed CodeMirror static highlight.
type CodeMirror struct {
	// Root is the class list of the outermost .CodeMirror element
	// ("CodeMirror cm-s-default CodeMirrorServer"): its cm-s-* theme
	// class styles the spans too, under the client's own theme class.
	Root  []string
	Lines []CodeLine
	// CSV: the cm-csv-tsv-wrapper layout (a CSV or TSV snippet's rows
	// of cm-col cells), read here as lines of their cells' text.
	CSV bool
}

// CodeLine is one line of code (one div of .CodeMirror-code).
type CodeLine []CodeRun

// CodeRun is text with the classes of the spans around it, outermost
// first ("cm-string", or "cm-variable cm-property" for one span with two
// token styles).
type CodeRun struct {
	Text    string
	Classes []string
}

// Plain is the line's text.
func (l CodeLine) Plain() string {
	var b strings.Builder
	for _, r := range l {
		b.WriteString(r.Text)
	}
	return b.String()
}

// Plain is the code's text, the lines joined by newlines: what Slack's
// FileSnippet handleCopy puts on the clipboard (the selection with its
// zero-width spaces dropped).
func (c *CodeMirror) Plain() string {
	if c == nil {
		return ""
	}
	ls := make([]string, len(c.Lines))
	for i, l := range c.Lines {
		ls[i] = l.Plain()
	}
	return strings.Join(ls, "\n")
}

// ParseCodeMirror reads CodeMirror's static highlight HTML; nil when s
// holds no .CodeMirror-code. Each child of .CodeMirror-code is a line, its
// text the pre's (white-space: pre-wrap; the newline straight after
// <pre> dropped, as HTML parsing drops it). Zero-width spaces (Slack's
// server puts them where a line would otherwise be empty, and
// FileSnippet's copy handler drops them) are left out.
func ParseCodeMirror(s string) *CodeMirror {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	z := xhtml.NewTokenizer(strings.NewReader(s))
	type elem struct {
		tag     string
		classes []string
		line    bool // a line of .CodeMirror-code
		code    bool // .CodeMirror-code itself
	}
	var (
		cm       CodeMirror
		stack    []elem
		cur      = -1 // the line being read (index into cm.Lines)
		found    bool
		code     int  // len(stack) inside .CodeMirror-code, 0 outside
		pre      int  // pre elements open in the line
		afterPre bool // the next text follows a <pre> start tag
	)
	classesOf := func(t xhtml.Token) []string {
		for _, a := range t.Attr {
			if a.Key == "class" {
				return strings.Fields(a.Val)
			}
		}
		return nil
	}
	// the class lists of the spans (and other inline elements) between
	// the line's pre and the text, outermost first
	spanClasses := func() []string {
		var out []string
		inLine := false
		for i := range stack {
			e := &stack[i]
			if e.line {
				inLine = true
				continue
			}
			if !inLine || e.tag == "pre" || e.tag == "div" || len(e.classes) == 0 {
				continue
			}
			out = append(out, strings.Join(e.classes, " "))
		}
		return out
	}
	add := func(txt string) {
		if cur < 0 || txt == "" {
			return
		}
		cls := spanClasses()
		l := &cm.Lines[cur]
		if n := len(*l); n > 0 && sameClasses((*l)[n-1].Classes, cls) {
			(*l)[n-1].Text += txt
			return
		}
		*l = append(*l, CodeRun{Text: txt, Classes: cls})
	}
	for {
		tt := z.Next()
		if tt == xhtml.ErrorToken {
			break
		}
		t := z.Token()
		switch tt {
		case xhtml.StartTagToken, xhtml.SelfClosingTagToken:
			afterPre = false
			e := elem{tag: t.Data, classes: classesOf(t)}
			if cm.Root == nil && hasClass(e.classes, "CodeMirror") {
				cm.Root = e.classes
			}
			if hasClass(e.classes, "cm-csv-tsv-wrapper") {
				cm.CSV = true
			}
			switch {
			case hasClass(e.classes, "CodeMirror-code"):
				e.code = true
				found = true
			case code > 0 && len(stack) == code:
				// a child of .CodeMirror-code (> div): a line
				e.line = true
				cm.Lines = append(cm.Lines, nil)
				cur = len(cm.Lines) - 1
				pre = 0
			case cm.CSV && cur >= 0 && hasClass(e.classes, "cm-col") && len(cm.Lines[cur]) > 0:
				// a CSV row's next cell (display: table-cell), a tab apart
				cm.Lines[cur] = append(cm.Lines[cur], CodeRun{Text: "\t"})
			}
			if t.Data == "br" && (pre > 0 || cm.CSV) {
				add("\n")
			}
			if tt == xhtml.StartTagToken && !voidTag(t.Data) {
				stack = append(stack, e)
				if e.code {
					code = len(stack)
				}
				if t.Data == "pre" && cur >= 0 {
					pre++
					afterPre = true
				}
			}
		case xhtml.EndTagToken:
			afterPre = false
			for i := len(stack) - 1; i >= 0; i-- {
				if stack[i].tag != t.Data {
					continue
				}
				for _, e := range stack[i:] {
					switch {
					case e.line:
						cur = -1
					case e.code:
						code = 0
					case e.tag == "pre" && pre > 0:
						pre--
					}
				}
				stack = stack[:i]
				break
			}
		case xhtml.TextToken:
			txt := t.Data
			if afterPre {
				txt = strings.TrimPrefix(txt, "\n")
				afterPre = false
			}
			if pre == 0 && !cm.CSV {
				continue // between the lines' divs, or outside the pre
			}
			add(strings.ReplaceAll(txt, "​", ""))
		}
	}
	if !found {
		return nil
	}
	return &cm
}

func hasClass(cs []string, c string) bool {
	for _, x := range cs {
		if x == c {
			return true
		}
	}
	return false
}

func sameClasses(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func voidTag(t string) bool {
	switch t {
	case "br", "img", "hr", "input", "meta", "link", "wbr", "col", "area", "base", "embed", "source", "track", "param":
		return true
	}
	return false
}

// SnippetDisplayMax is computePreviewsForSnippet's limit (client-boot
// QS2I): a fetched snippet's content and content_highlight_html are cut
// to 12,800 characters for display, and the file marked is_truncated.
const SnippetDisplayMax = 12800

// TruncateHTML is the html-truncate package Slack's
// computePreviewsForSnippet calls (gantry-v2-vendors LvCQ: truncate(html,
// 12800) with its default "..." ellipsis), ported as it behaves: it
// counts the characters outside tags (UTF-16 code units; the "\n"s
// between tags count, an entity counts as its letters and "@" as
// "&#64;"), cuts where the count would pass n, appends the ellipsis when
// what's left is longer than n, and closes the open tags; cut reports
// that it did. When it doesn't cut, s comes back as it was.
func TruncateHTML(s string, n int) (out string, cut bool) {
	orig := s
	s = strings.ReplaceAll(s, "@", "&#64;")
	var (
		items   []string
		total   int
		content strings.Builder
		self    string // the last self-closing match: html-truncate keeps it across tags
		urlLast int    // URL_REGEX.lastIndex (the regex is global)
	)
	for {
		loc := htTag.FindStringIndex(s)
		if loc == nil {
			if total >= n {
				break
			}
			m := htURLExec(s, &urlLast)
			if m == nil || u16Len(s[:m[0]]) >= n {
				content.WriteString(u16Prefix(s, n-total))
				break
			}
			for m != nil {
				content.WriteString(u16Prefix(s, u16Len(s[:m[1]])-total))
				s = s[m[1]:]
				m = htURLExec(s, &urlLast)
			}
			break
		}
		matched := s[loc[0]:loc[1]]
		idx := u16Len(s[:loc[0]])
		if total+idx > n {
			content.WriteString(u16Prefix(s, n-total))
			break
		}
		total += idx
		content.WriteString(s[:loc[0]])
		if len(matched) > 1 && matched[1] == '/' {
			if len(items) > 0 {
				items = items[:len(items)-1]
			}
		} else if self = htSelfClose.FindString(matched); self == "" {
			items = append(items, htGetTag(matched))
		}
		if self != "" {
			content.WriteString(self)
		} else {
			content.WriteString(matched)
		}
		s = s[loc[1]:]
	}
	if u16Len(s) <= n {
		return orig, false
	}
	content.WriteString("...")
	for i := len(items) - 1; i >= 0; i-- {
		if items[i] != "img" && items[i] != "br" {
			content.WriteString("</" + items[i] + ">")
		}
	}
	out = content.String()
	if loc := htImg.FindStringIndex(out); loc != nil { // _removeImageTag (keepImageTag unset)
		out = out[:loc[0]] + out[loc[1]:]
	}
	return out, true
}

// html-truncate's patterns. JavaScript's \s (no u flag) is wider than
// RE2's; \w is the same ASCII class in both.
const (
	jsSpace = `[\t\n\v\f\r \x{a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}]`
	htKV    = `([\w|-]+` + jsSpace + `*=` + jsSpace + `*"[^"]*"` + jsSpace + `*)*`
	htSl    = jsSpace + `*/?` + jsSpace + `*`
)

var (
	htSelfClose = regexp.MustCompile(`</?\w+` + jsSpace + `*` + htKV + jsSpace + `*/` + jsSpace + `*>`)
	htTag       = regexp.MustCompile(`</?\w+` + jsSpace + `*` + htKV + htSl + `>`)
	htURL       = regexp.MustCompile(`(((ftp|https?)://)[\-\w@:%_\+.~#?,&/=]+)|((mailto:)?[_.\w\-]+@([\w][\w\-]+\.)+[a-zA-Z]{2,3})`)
	htImg       = regexp.MustCompile(`<img` + jsSpace + `*` + htKV + htSl + `>`)
)

// htURLExec is URL_REGEX.exec(s) with its lastIndex: a global regex
// starts where its last match ended, in whichever string it's given, and
// a miss sets it back to 0. The match is in byte offsets of s.
func htURLExec(s string, last *int) []int {
	if *last > u16Len(s) {
		*last = 0
		return nil
	}
	from := u16Byte(s, *last)
	m := htURL.FindStringIndex(s[from:])
	if m == nil {
		*last = 0
		return nil
	}
	m[0], m[1] = m[0]+from, m[1]+from
	*last = u16Len(s[:m[1]])
	return m
}

// htGetTag is _getTag: the tag up to its first space, else up to ">".
func htGetTag(t string) string {
	i := strings.IndexByte(t, ' ')
	if i < 0 {
		i = strings.IndexByte(t, '>')
	}
	if i < 1 {
		return ""
	}
	return t[1:i]
}

// u16Len is s's length in UTF-16 code units (JavaScript's length).
func u16Len(s string) int {
	n := 0
	for _, r := range s {
		n += utf16.RuneLen(r)
	}
	return n
}

// u16Byte is the byte offset of UTF-16 unit k in s (len(s) past the end).
func u16Byte(s string, k int) int {
	n := 0
	for i, r := range s {
		if n >= k {
			return i
		}
		n += utf16.RuneLen(r)
	}
	return len(s)
}

// u16Prefix is s.substring(0, k): the first k UTF-16 units (none for k
// < 1). A cut between a surrogate pair's halves leaves the lone high
// surrogate, which the page draws as U+FFFD.
func u16Prefix(s string, k int) string {
	if k <= 0 {
		return ""
	}
	n := 0
	for i, r := range s {
		if n >= k {
			return s[:i]
		}
		if w := utf16.RuneLen(r); n+w > k {
			return s[:i] + "�"
		} else {
			n += w
		}
	}
	return s
}
