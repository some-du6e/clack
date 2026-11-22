package richtext

import (
	"strings"
	"sync"

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
// nesting and output are capped, and past a cap the rest is dropped. The
// work is linear in the HTML: a run of text joins the span before it
// through a builder, every span, line break and list item counts toward
// emailMaxParts, and the open elements' kinds are counted, not searched.
// End tags HTML lets an email leave out (a <p> before the next block, an
// <li> before the next item, a <head> before the body) end where HTML
// ends them, so unclosed elements don't pile up to the nesting cap; past
// the cap, an element that hides its content (a style sheet, a script)
// still hides it.

const (
	emailMaxHTML   = 1 << 20 // bytes of HTML read
	emailMaxDepth  = 200     // open elements tracked
	emailMaxRunes  = 200000  // text kept
	emailMaxBlocks = 2000
	emailMaxParts  = 100000 // spans, line breaks and list items kept
)

// EmailBlocksOf is EmailBlocks for an email file's HTML, converted once:
// views lay an email out again at every width, theme or message change.
// The last few emails' blocks are kept, by their HTML. The blocks are
// shared: callers read them.
func EmailBlocksOf(src string) []Block {
	if src == "" {
		return nil
	}
	emailMemo.mu.Lock()
	for _, e := range emailMemo.ent {
		if len(e.src) == len(src) && e.src == src {
			emailMemo.mu.Unlock()
			return e.out
		}
	}
	emailMemo.mu.Unlock()
	out := EmailBlocks(src)
	emailMemo.mu.Lock()
	emailMemo.ent[emailMemo.next] = emailMemoEnt{src: src, out: out}
	emailMemo.next = (emailMemo.next + 1) % len(emailMemo.ent)
	emailMemo.mu.Unlock()
	return out
}

type emailMemoEnt struct {
	src string
	out []Block
}

var emailMemo struct {
	mu   sync.Mutex
	ent  [8]emailMemoEnt
	next int
}

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
		if c.full() {
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
	cur   []Span          // the paragraph being built
	tail  strings.Builder // text the last span of cur gets (seal)
	space bool            // a collapsed space is pending before the next text
	runes int
	parts int // spans, breaks and list items made
	// what the open elements are: skipping, preformatted, quoted, lists,
	// and of the kinds an implied end tag closes (impliedEnds)
	skipN, preN, quoteN, listN      int
	headN, pN, liN, dN, rowN, cellN int
	// deep: past the nesting cap, an element hiding its content (deepN
	// open of its kind) keeps its text out until its end tag
	deep  atom.Atom
	deepN int

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

func (c *emailConv) skipping() bool { return c.skipN > 0 || c.deepN > 0 }
func (c *emailConv) inPre() bool    { return c.preN > 0 }
func (c *emailConv) inQuote() bool  { return c.quoteN > 0 }

// full reports a cap reached: the rest is dropped.
func (c *emailConv) full() bool {
	return c.runes > emailMaxRunes || len(c.out) > emailMaxBlocks || c.parts > emailMaxParts
}

func (c *emailConv) push(e emailElem) {
	c.stack = append(c.stack, e)
	c.count(e, 1)
}

// popTo closes the open elements from i up.
func (c *emailConv) popTo(i int) {
	for j := len(c.stack) - 1; j >= i; j-- {
		c.count(c.stack[j], -1)
	}
	c.stack = c.stack[:i]
}

func (c *emailConv) count(e emailElem, d int) {
	if e.skip {
		c.skipN += d
	}
	if e.pre {
		c.preN += d
	}
	if e.quote {
		c.quoteN += d
	}
	if e.list > 0 {
		c.listN += d
	}
	switch e.a {
	case atom.Head:
		c.headN += d
	case atom.P:
		c.pN += d
	case atom.Li:
		c.liN += d
	case atom.Dd, atom.Dt:
		c.dN += d
	case atom.Tr:
		c.rowN += d
	case atom.Td, atom.Th:
		c.cellN += d
	}
}

// open is the index of the innermost open element of kind a, looking no
// further out than an element of a kind in stop (-1: none open there).
func (c *emailConv) open(a atom.Atom, stop ...atom.Atom) int {
	for i := len(c.stack) - 1; i >= 0; i-- {
		switch k := c.stack[i].a; {
		case k == a:
			return i
		case slicesContain(stop, k):
			return -1
		}
	}
	return -1
}

func slicesContain(as []atom.Atom, a atom.Atom) bool {
	for _, x := range as {
		if x == a {
			return true
		}
	}
	return false
}

// buttonScope ends the search for a <p> to close (HTML's "has a p element
// in button scope"); listScope and defScope an item's.
var (
	buttonScope = []atom.Atom{atom.Html, atom.Table, atom.Td, atom.Th, atom.Caption, atom.Marquee, atom.Object, atom.Template, atom.Applet, atom.Button}
	listScope   = append([]atom.Atom{atom.Ul, atom.Ol}, buttonScope...)
	defScope    = append([]atom.Atom{atom.Dl}, buttonScope...)
)

// impliedEnds closes what HTML closes before an element of kind a starts:
// an open <p> before a block (a <p> ends at the next <p>, <div>, list,
// heading, table...), an open item before the next <li>, <dd> or <dt>, a
// row's cells before its next cell or row, and a <head> before the
// body's first element.
func (c *emailConv) impliedEnds(a atom.Atom) {
	if c.headN > 0 {
		switch a {
		case atom.Title, atom.Meta, atom.Link, atom.Style, atom.Script, atom.Base, atom.Noscript, atom.Template:
		default:
			c.end(atom.Head)
		}
	}
	switch a {
	case atom.Address, atom.Article, atom.Aside, atom.Blockquote, atom.Center, atom.Details, atom.Div, atom.Dl,
		atom.Fieldset, atom.Figure, atom.Footer, atom.Form, atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6,
		atom.Header, atom.Hr, atom.Main, atom.Menu, atom.Nav, atom.Ol, atom.P, atom.Pre, atom.Section, atom.Table,
		atom.Ul, atom.Li, atom.Dd, atom.Dt:
		if c.pN > 0 && c.open(atom.P, buttonScope...) >= 0 {
			c.end(atom.P)
		}
	}
	switch a {
	case atom.Li:
		if c.liN > 0 && c.open(atom.Li, listScope...) >= 0 {
			c.end(atom.Li)
		}
	case atom.Dd, atom.Dt:
		for _, k := range []atom.Atom{atom.Dd, atom.Dt} {
			if c.dN > 0 && c.open(k, defScope...) >= 0 {
				c.end(k)
			}
		}
	case atom.Tr:
		if c.rowN > 0 && c.open(atom.Tr, atom.Table, atom.Tbody, atom.Thead, atom.Tfoot) >= 0 {
			c.end(atom.Tr)
		}
	case atom.Td, atom.Th:
		for _, k := range []atom.Atom{atom.Td, atom.Th} {
			if c.cellN > 0 && c.open(k, atom.Tr, atom.Table) >= 0 {
				c.end(k)
			}
		}
	}
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
	if c.deepN > 0 { // past the cap, inside an element hiding its content
		if a == c.deep && !self {
			c.deepN++
		}
		return
	}
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
	c.impliedEnds(a)
	if len(c.stack) >= emailMaxDepth {
		// too deep: its text stays and its markup goes, the matching end
		// tag ignored (end looks the atom up); an element that hides its
		// content still hides it
		_, hidden := attrs["hidden"]
		if skipAtom(a) || hidden || hiddenStyle(attrs["style"]) {
			c.deep, c.deepN = a, 1
		}
		return
	}
	e := emailElem{a: a, st: c.top().st, skip: skipAtom(a)}
	switch a {
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
		// the scheme in any case, as a browser reads it (HTTPS://…)
		if u := SafeURL(attrs["href"]); u != "" {
			if l := strings.ToLower(u); strings.HasPrefix(l, "http") || strings.HasPrefix(l, "mailto:") {
				e.link = u
			}
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
			if c.listN == 0 {
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
	c.push(e)
}

// skipAtom reports an element whose content an email doesn't show.
func skipAtom(a atom.Atom) bool {
	switch a {
	case atom.Script, atom.Style, atom.Head, atom.Title, atom.Noscript, atom.Template, atom.Svg, atom.Math, atom.Object, atom.Iframe, atom.Select, atom.Button, atom.Textarea:
		return true
	}
	return false
}

func (c *emailConv) end(a atom.Atom) {
	if c.deepN > 0 {
		if a == c.deep {
			c.deepN--
		}
		return
	}
	i := len(c.stack) - 1
	for i >= 0 && c.stack[i].a != a {
		i--
	}
	if i < 0 {
		return // never opened (or past the depth cap)
	}
	if c.skipping() {
		c.popTo(i)
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
	c.popTo(i)
	if (a == atom.Ul || a == atom.Ol) && c.listN == 0 {
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
			if c.full() {
				return
			}
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
					c.tail.WriteByte(' ') // the earlier text's run
					c.runes++
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
	if c.full() {
		return
	}
	c.runes += len(sp.Text)
	// one span per run of the same look: its text grows in tail
	if n := len(c.cur); n > 0 {
		p := &c.cur[n-1]
		if p.Kind == sp.Kind && p.Style == sp.Style && p.URL == sp.URL && sp.Kind != Break {
			c.tail.WriteString(sp.Text)
			return
		}
	}
	c.seal()
	c.parts++
	c.cur = append(c.cur, sp)
}

// seal gives the last span the text its run gathered.
func (c *emailConv) seal() {
	if c.tail.Len() > 0 {
		c.cur[len(c.cur)-1].Text += c.tail.String()
		c.tail.Reset()
	}
}

func (c *emailConv) brk() {
	c.space = false
	if c.full() {
		return
	}
	c.seal()
	c.parts++
	c.cur = append(c.cur, Span{Kind: Break})
}

// flushPara ends the paragraph being built as a block of its own (a list
// item's text goes to endItem).
func (c *emailConv) flushPara() {
	c.space = false
	c.seal()
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
	c.seal()
	sp := trimBreaks(c.cur)
	c.cur = nil
	if len(sp) > 0 {
		c.parts++
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
