package richtext

import (
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// An email's body. Slack draws it in a sandboxed iframe of the email's
// own HTML (EmailIframe, wwJm: url_private?iframe=1), which Clack has no
// browser engine for. EmailBlocks reads the same HTML into Clack's
// blocks instead: its text with the inline formatting an email uses
// (bold, italic, underline, strike, code, links), paragraphs and line
// breaks, headings (bold), lists, quotes, preformatted text and tables
// (a row's cells side by side, a cell's paragraphs under each other).
// Images, styles, scripts and hidden elements leave nothing; links are
// http(s) and mailto only (SafeURL). The HTML is untrusted: its size,
// nesting and output are capped, and past a cap the rest is dropped.

const (
	emailMaxHTML   = 1 << 20 // bytes of HTML read
	emailMaxDepth  = 200     // open elements tracked
	emailMaxRunes  = 200000  // text kept
	emailMaxBlocks = 2000
)

// EmailBlocks is src, an email's HTML, as blocks.
func EmailBlocks(src string) []Block {
	if len(src) > emailMaxHTML {
		src = src[:emailMaxHTML]
	}
	c := emailConv{}
	z := html.NewTokenizer(strings.NewReader(src))
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			c.finish()
			return c.out
		case html.TextToken:
			c.text(string(z.Text()))
		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := z.TagName()
			var attrs map[string]string
			for hasAttr {
				var k, v []byte
				k, v, hasAttr = z.TagAttr()
				if attrs == nil {
					attrs = map[string]string{}
				}
				attrs[string(k)] = string(v)
			}
			c.start(atom.Lookup(name), attrs, tt == html.SelfClosingTagToken)
		case html.EndTagToken:
			name, _ := z.TagName()
			c.end(atom.Lookup(name))
		}
		if c.runes > emailMaxRunes || len(c.out) > emailMaxBlocks {
			c.finish()
			return c.out
		}
	}
}

type emailElem struct {
	a     atom.Atom
	st    Style
	link  string
	skip  bool
	pre   bool
	quote bool
	list  int // 1 bulleted, 2 numbered
}

type emailConv struct {
	stack []emailElem
	out   []Block
	cur   []Span // the paragraph being built
	space bool   // a collapsed space is pending before the next text
	runes int

	// lists: the items of the list open at the top, and the item being
	// built (into cur)
	items   [][]Span
	ordered bool
	inItem  bool
	cell    bool // a table cell's text since the row began (cells joined by "  ")
}

func (c *emailConv) top() emailElem {
	if len(c.stack) == 0 {
		return emailElem{}
	}
	return c.stack[len(c.stack)-1]
}

func (c *emailConv) skipping() bool {
	for _, e := range c.stack {
		if e.skip {
			return true
		}
	}
	return false
}

func (c *emailConv) inPre() bool {
	for _, e := range c.stack {
		if e.pre {
			return true
		}
	}
	return false
}

func (c *emailConv) inQuote() bool {
	for _, e := range c.stack {
		if e.quote {
			return true
		}
	}
	return false
}

func hiddenStyle(css string) bool {
	css = strings.ToLower(strings.ReplaceAll(css, " ", ""))
	return strings.Contains(css, "display:none") || strings.Contains(css, "visibility:hidden") ||
		strings.Contains(css, "max-height:0") && strings.Contains(css, "overflow:hidden")
}

// blockAtom reports whether a is an element that starts and ends a line.
func blockAtom(a atom.Atom) bool {
	switch a {
	case atom.P, atom.Div, atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6, atom.Blockquote, atom.Pre,
		atom.Table, atom.Tbody, atom.Thead, atom.Tfoot, atom.Tr, atom.Ul, atom.Ol, atom.Li, atom.Hr,
		atom.Section, atom.Article, atom.Header, atom.Footer, atom.Center, atom.Address, atom.Dl, atom.Dt, atom.Dd, atom.Form:
		return true
	}
	return false
}

func (c *emailConv) start(a atom.Atom, attrs map[string]string, self bool) {
	switch a {
	case atom.Br:
		if !c.skipping() {
			c.brk()
		}
		return
	case atom.Img, atom.Input, atom.Meta, atom.Link, atom.Base, atom.Col, atom.Area, atom.Wbr, atom.Source, atom.Track, atom.Embed, atom.Param:
		return // void: no text
	case atom.Hr:
		if !c.skipping() {
			c.flush()
		}
		return
	}
	if self {
		return
	}
	if len(c.stack) >= emailMaxDepth {
		// too deep: its text stays, its markup goes; the matching end tag
		// is ignored (end looks the atom up)
		return
	}
	e := emailElem{a: a, st: c.top().st}
	switch a {
	case atom.Script, atom.Style, atom.Head, atom.Title, atom.Noscript, atom.Template, atom.Svg, atom.Math, atom.Object, atom.Iframe, atom.Select, atom.Button, atom.Textarea:
		e.skip = true
	case atom.B, atom.Strong:
		e.st |= Bold
	case atom.I, atom.Em, atom.Cite:
		e.st |= Italic
	case atom.U, atom.Ins:
		e.st |= Underline
	case atom.S, atom.Strike, atom.Del:
		e.st |= Strike
	case atom.Code, atom.Tt, atom.Kbd, atom.Samp:
		e.st |= Code
	case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6, atom.Th:
		e.st |= Bold
	case atom.A:
		if u := SafeURL(attrs["href"]); u != "" && (strings.HasPrefix(u, "http") || strings.HasPrefix(u, "mailto:")) {
			e.link = u
		}
	case atom.Pre:
		e.pre = true
	case atom.Blockquote:
		e.quote = true
	case atom.Ul:
		e.list = 1
	case atom.Ol:
		e.list = 2
	}
	_, hidden := attrs["hidden"]
	if hiddenStyle(attrs["style"]) || hidden || strings.EqualFold(attrs["aria-hidden"], "true") && a != atom.A {
		e.skip = true
	}
	if !e.skip && !c.skipping() {
		switch {
		case a == atom.Ul || a == atom.Ol:
			if c.inItem {
				c.endItem() // a list in an item: its items follow the item's
			} else {
				c.flushPara()
			}
			if c.listDepth() == 0 {
				c.ordered = a == atom.Ol
			}
		case a == atom.Li:
			c.endItem()
			c.flushPara()
			c.inItem = true
		case a == atom.Td || a == atom.Th:
			if c.cell && len(c.cur) > 0 {
				c.add(Span{Kind: Text, Text: "  "})
			}
			c.cell = true
		case blockAtom(a):
			c.flush()
		}
	}
	c.stack = append(c.stack, e)
}

func (c *emailConv) end(a atom.Atom) {
	i := len(c.stack) - 1
	for i >= 0 && c.stack[i].a != a {
		i--
	}
	if i < 0 {
		return // never opened (or past the depth cap)
	}
	if c.skipping() {
		c.stack = c.stack[:i]
		return
	}
	// a block ends with its element still open (a quote's, a pre's text
	// is theirs), a list once it's closed
	switch {
	case a == atom.Li:
		c.endItem()
	case a == atom.Ul || a == atom.Ol:
		c.endItem()
	case a == atom.Tr:
		c.flush()
		c.cell = false
	case a == atom.Td || a == atom.Th:
	case blockAtom(a):
		c.flush()
	}
	c.stack = c.stack[:i]
	if (a == atom.Ul || a == atom.Ol) && c.listDepth() == 0 {
		c.flushList()
	}
}

func (c *emailConv) text(s string) {
	if c.skipping() || s == "" {
		return
	}
	el := c.top()
	if c.inPre() {
		lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
		for i, ln := range lines {
			if i > 0 {
				c.brk()
			}
			if ln != "" {
				c.add(emailSpan(ln, el))
			}
		}
		return
	}
	// white space collapses to one space, which goes with the text it
	// was in (the run before a link, not the link)
	var b strings.Builder
	before := c.space // a space an earlier text left: that text's run gets it
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f' {
			c.space = true
			continue
		}
		if c.space {
			n := len(c.cur)
			switch {
			case before && b.Len() == 0:
				if n > 0 && c.cur[n-1].Kind != Break {
					c.cur[n-1].Text += " "
				}
			case b.Len() > 0 || n > 0 && c.cur[n-1].Kind != Break:
				b.WriteByte(' ')
			}
			c.space = false
		}
		before = false
		if r == '\u00a0' {
			r = ' '
		}
		b.WriteRune(r)
	}
	if b.Len() > 0 {
		c.add(emailSpan(b.String(), el))
	}
}

func emailSpan(s string, el emailElem) Span {
	sp := Span{Kind: Text, Text: s, Style: el.st}
	if el.link != "" {
		sp.Kind, sp.URL = Link, el.link
	}
	return sp
}

func (c *emailConv) add(sp Span) {
	c.runes += len(sp.Text)
	// one span per run of the same look
	if n := len(c.cur); n > 0 {
		p := &c.cur[n-1]
		if p.Kind == sp.Kind && p.Style == sp.Style && p.URL == sp.URL && sp.Kind != Break {
			p.Text += sp.Text
			return
		}
	}
	c.cur = append(c.cur, sp)
}

func (c *emailConv) brk() {
	c.space = false
	c.cur = append(c.cur, Span{Kind: Break})
}

// listDepth is how many lists are open.
func (c *emailConv) listDepth() int {
	n := 0
	for _, e := range c.stack {
		if e.list > 0 {
			n++
		}
	}
	return n
}

// flushPara ends the paragraph being built as a block of its own (a list
// item's text goes to endItem).
func (c *emailConv) flushPara() {
	c.space = false
	sp := trimBreaks(c.cur)
	c.cur = nil
	if len(sp) == 0 {
		return
	}
	if len(c.items) > 0 { // text after a list's items, the list still open
		c.flushList()
	}
	kind := Paragraph
	switch {
	case c.inPre():
		kind = Preformatted
	case c.inQuote():
		kind = Quote
	}
	c.out = append(c.out, Block{Kind: kind, Spans: sp})
}

func (c *emailConv) endItem() {
	if !c.inItem {
		return
	}
	sp := trimBreaks(c.cur)
	c.cur = nil
	if len(sp) > 0 {
		c.items = append(c.items, sp)
	}
	c.inItem = false
}

func (c *emailConv) flushList() {
	if len(c.items) > 0 {
		c.out = append(c.out, Block{Kind: List, Items: c.items, Ordered: c.ordered})
	}
	c.items = nil
}

// flush ends a line: inside a list item a paragraph's end is a break in
// the item, elsewhere it ends the paragraph.
func (c *emailConv) flush() {
	if c.inItem {
		if len(c.cur) > 0 && c.cur[len(c.cur)-1].Kind != Break {
			c.brk()
		}
		return
	}
	c.flushPara()
}

// finish ends whatever is open.
func (c *emailConv) finish() {
	c.endItem()
	c.flushPara()
	c.flushList()
}

func trimBreaks(sp []Span) []Span {
	for len(sp) > 0 && sp[0].Kind == Break {
		sp = sp[1:]
	}
	for len(sp) > 0 && sp[len(sp)-1].Kind == Break {
		sp = sp[:len(sp)-1]
	}
	if len(sp) == 1 && sp[0].Kind == Text && strings.TrimSpace(sp[0].Text) == "" {
		return nil
	}
	return sp
}
