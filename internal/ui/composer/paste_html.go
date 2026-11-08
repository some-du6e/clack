package composer

import (
	"bytes"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/net/html"
)

// Rich paste: clipboard HTML (from a browser, Notes, Word, Google Docs,
// Slack itself…) converted into the composer's Doc, the same model the
// toolbar and markdown shortcuts produce. Kept: lists (nested, bulleted
// and numbered), bold, italic, strike, underline, inline code, code
// blocks, links, quotes and line breaks. Everything else is dropped: its
// text stays, its markup goes; scripts, styles, images and hidden
// elements leave nothing.
//
// Clipboard HTML is untrusted input: links are http(s) or mailto only,
// and the input, output, nesting, CSS and URL sizes are capped. Over a
// cap the paste falls back to plain text.

const (
	phMaxHTML  = 2 << 20 // bytes of clipboard HTML we look at
	phMaxRunes = 40000   // Slack's message limit; over it, paste plain text
	phMaxDepth = 256     // open elements we track
	phMaxURL   = 4096    // link target length
	phMaxCSS   = 64 << 10
	phMaxRules = 1024
)

// phStyle is a CSS-ish inline formatting change: bits an element turns on
// and bits it turns off (Google Docs wraps everything in
// <b style="font-weight:normal">).
type phStyle struct{ set, clear Style }

func (p phStyle) apply(s Style) Style { return (s | p.set) &^ p.clear }

func (p phStyle) merge(q phStyle) phStyle {
	return phStyle{set: (p.set &^ q.clear) | q.set, clear: (p.clear &^ q.set) | q.clear}
}

// phElem is one open element.
type phElem struct {
	tag    string
	st     phStyle
	link   string // sanitized href ("" for none); only on <a>
	nolink bool   // <a> without a usable href, or a mention: text only
	skip   bool   // content is dropped (script, style, hidden…)
	list   int    // 1 bulleted, 2 numbered; 0 not a list
	ind    int    // list: nesting level; -1 = from depth
	item   bool   // <li> or a Word list paragraph
	quote  bool
	pre    bool
	ignore bool // Word's mso-list:Ignore marker span: text is the bullet
}

type phConv struct {
	b     *rtBuilder
	stack []phElem
	css   map[string]phStyle // "tag.class" and ".class"

	lineOpen bool // a line has been started
	content  bool // the current line has text
	brk      bool // a block boundary is pending
	item     bool // a list item opened and has no line yet
	itemKind BlockKind
	itemInd  int
	space    bool // collapsed whitespace pending before the next text
	spaceA   Attr // its format: where the whitespace was
	preStart bool // just inside <pre>: drop one leading newline
	marker   strings.Builder
	over     bool // output cap hit
	inCSS    bool
}

// docFromHTML converts clipboard HTML into a Doc. It returns nil when the
// HTML has no text or exceeds the caps, so the caller pastes the plain
// text instead.
func docFromHTML(src string) *Doc {
	if src == "" || len(src) > phMaxHTML {
		return nil
	}
	c := &phConv{b: rtNewBuilder(), css: map[string]phStyle{}}
	z := html.NewTokenizer(strings.NewReader(src))
	z.SetMaxBuf(phMaxHTML)
	var cssText strings.Builder
	for !c.over {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			goto done
		case html.TextToken:
			if c.inCSS {
				if cssText.Len() < phMaxCSS {
					cssText.Write(z.Text())
				}
				continue
			}
			c.text(string(z.Text()))
		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := z.TagName()
			tag := string(name)
			attrs := map[string]string{}
			for hasAttr {
				var k, v []byte
				k, v, hasAttr = z.TagAttr()
				if _, dup := attrs[string(k)]; !dup {
					attrs[string(k)] = string(v)
				}
			}
			if tag == "style" && tt == html.StartTagToken {
				c.inCSS = true
				cssText.Reset()
			}
			c.start(tag, attrs, tt == html.SelfClosingTagToken)
		case html.EndTagToken:
			name, _ := z.TagName()
			tag := string(name)
			if tag == "style" && c.inCSS {
				c.inCSS = false
				phParseCSS(cssText.String(), c.css)
				cssText.Reset()
			}
			c.end(tag)
		}
	}
done:
	if c.over {
		return nil
	}
	d := c.b.done()
	phDropAutoLinks(d)
	phStripMarkers(d)
	if d.Empty() {
		return nil
	}
	return d
}

// phVoid lists elements with no end tag.
var phVoid = map[string]bool{
	"area": true, "base": true, "br": true, "col": true, "embed": true, "hr": true, "img": true,
	"input": true, "link": true, "meta": true, "param": true, "source": true, "track": true, "wbr": true,
}

// phSkip lists elements whose content never reaches the composer.
var phSkip = map[string]bool{
	"script": true, "style": true, "title": true, "template": true, "noscript": true, "iframe": true,
	"object": true, "embed": true, "svg": true, "math": true, "textarea": true, "select": true,
	"head": true, "audio": true, "video": true, "canvas": true, "noembed": true, "noframes": true,
	"xmp": true, "plaintext": true, "button": true, "datalist": true, "dialog": true,
}

// phBlock lists elements that start and end a line.
var phBlock = map[string]bool{
	"p": true, "div": true, "section": true, "article": true, "header": true, "footer": true,
	"main": true, "nav": true, "aside": true, "address": true, "center": true, "figure": true,
	"figcaption": true, "table": true, "caption": true, "tr": true, "dl": true, "dt": true, "dd": true,
	"details": true, "summary": true, "fieldset": true, "legend": true, "form": true, "body": true,
	"html": true, "h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"ul": true, "ol": true, "li": true, "blockquote": true, "pre": true, "hr": true, "menu": true,
}

func (c *phConv) skipping() bool {
	for i := range c.stack {
		if c.stack[i].skip {
			return true
		}
	}
	return false
}

func (c *phConv) start(tag string, attrs map[string]string, selfClosing bool) {
	if tag == "br" {
		if !c.skipping() {
			c.ensureLine()
			c.newLine()
		}
		return
	}
	if tag == "img" {
		if !c.skipping() {
			if t := phImgText(attrs); t != "" {
				c.text(t)
			}
		}
		return
	}
	if tag == "li" {
		c.closeOpenItem()
	}
	if phBlock[tag] {
		c.brk = true
	}
	if (tag == "td" || tag == "th") && !c.space && c.content {
		c.space, c.spaceA = true, Attr{}
	}
	if phVoid[tag] || selfClosing {
		return
	}
	el := phElem{tag: tag, ind: -1}
	styleAttr := strings.ToLower(attrs["style"])
	css := phCSSDecls(styleAttr)
	for _, cl := range strings.Fields(attrs["class"]) {
		if s, ok := c.css[tag+"."+cl]; ok {
			el.st = el.st.merge(s)
		}
		if s, ok := c.css["."+cl]; ok {
			el.st = el.st.merge(s)
		}
	}
	el.st = el.st.merge(phTagStyle(tag)).merge(phDeclStyle(css))
	if phSkip[tag] || phHidden(attrs, css) {
		el.skip = true
	}
	switch tag {
	case "a":
		if attrs["data-stringify-type"] == "mention" {
			el.nolink = true
		} else if u := phSafeURL(attrs["href"]); u != "" {
			el.link = u
		} else {
			el.nolink = true
		}
	case "ul", "menu":
		el.list = 1
	case "ol":
		el.list = 2
	case "li":
		el.item = true
	case "blockquote":
		el.quote = true
	case "pre":
		el.pre = true
		c.preStart = true
	}
	if el.list != 0 {
		// Slack's own copy writes nesting as sibling lists with data-indent.
		if v, err := strconv.Atoi(attrs["data-indent"]); err == nil && v >= 0 {
			el.ind = min(v, edMaxIndent)
		}
	}
	// Word: <p class=MsoListParagraphCxSpFirst style='mso-list:l0 level2 lfo1'>
	// is a list item; its bullet is in a mso-list:Ignore span.
	if lvl, ok := phMsoLevel(css); ok && phBlock[tag] {
		el.item = true
		el.ind = lvl
		el.list = 1
	}
	if css["mso-list"] == "ignore" {
		el.ignore = true
		c.marker.Reset()
	}
	if len(c.stack) >= phMaxDepth {
		// Too deep to track: keep the text, ignore the markup.
		return
	}
	c.stack = append(c.stack, el)
	if el.item {
		c.item = true
		c.itemKind, c.itemInd = c.listFormat(len(c.stack) - 1)
		c.brk = true
	}
}

// closeOpenItem pops an unclosed <li> before a sibling <li>
// ("<li>a<li>b"), as browsers do.
func (c *phConv) closeOpenItem() {
	for i := len(c.stack) - 1; i >= 0; i-- {
		switch c.stack[i].tag {
		case "li":
			c.stack = c.stack[:i]
			return
		case "ul", "ol", "menu":
			return
		}
	}
}

func (c *phConv) end(tag string) {
	for i := len(c.stack) - 1; i >= 0; i-- {
		if c.stack[i].tag != tag {
			continue
		}
		el := c.stack[i]
		c.stack = c.stack[:i]
		if el.ignore {
			// Word's list marker: a number makes the item numbered.
			if phOrderedMarker(c.marker.String()) && c.item {
				c.itemKind = Ordered
			}
			c.marker.Reset()
		}
		if phBlock[tag] {
			c.brk = true
		}
		if el.pre {
			c.preStart = false
		}
		return
	}
}

// listFormat returns the list kind and indent of the item at stack[i].
func (c *phConv) listFormat(i int) (BlockKind, int) {
	el := c.stack[i]
	kind, ind, depth := Bullet, -1, -1
	if el.list == 2 {
		kind = Ordered
	}
	if el.ind >= 0 && el.list != 0 && el.item {
		// Word paragraph: its own level.
		return kind, el.ind
	}
	found := false
	for j := i - 1; j >= 0; j-- {
		if c.stack[j].list == 0 || c.stack[j].item {
			continue
		}
		depth++
		if !found {
			found = true
			if c.stack[j].list == 2 {
				kind = Ordered
			} else {
				kind = Bullet
			}
			ind = c.stack[j].ind
		}
	}
	if ind < 0 {
		ind = max(depth, 0)
	}
	return kind, min(ind, edMaxIndent)
}

// lineFormat is the block format text gets at the current position.
func (c *phConv) lineFormat() Line {
	pre, quote := false, false
	for i := range c.stack {
		pre = pre || c.stack[i].pre
		quote = quote || c.stack[i].quote
	}
	switch {
	case pre:
		return Line{Kind: CodeBlock}
	case c.item || c.inItem():
		return Line{Kind: c.itemKind, Indent: c.itemInd}
	case quote:
		return Line{Kind: Quote}
	}
	return Line{Kind: Para}
}

// inItem reports whether a list item is open (text after a nested list
// inside the same item continues it as a new line of the outer list).
func (c *phConv) inItem() bool {
	for i := len(c.stack) - 1; i >= 0; i-- {
		if c.stack[i].item {
			c.itemKind, c.itemInd = c.listFormat(i)
			return true
		}
	}
	return false
}

// ensureLine makes sure text goes on the right line: a pending block
// boundary starts a new line, unless the current one is still empty, in
// which case that line takes the new format.
func (c *phConv) ensureLine() {
	if c.lineOpen && !c.brk && !c.item {
		return
	}
	l := c.lineFormat()
	if c.lineOpen && !c.content {
		c.b.d.Lines[len(c.b.d.Lines)-1] = phNorm(l)
	} else {
		c.b.line(l)
		c.lineOpen = true
	}
	c.content, c.brk, c.item, c.space = false, false, false, false
}

// newLine is a <br> or a newline in <pre>: a new line in the same format.
func (c *phConv) newLine() {
	c.b.line(c.b.d.Lines[len(c.b.d.Lines)-1])
	c.content, c.space = false, false
	if len(c.b.d.Runes) > phMaxRunes {
		c.over = true
	}
}

func phNorm(l Line) Line {
	if !rtIsList(l.Kind) || l.Indent < 0 {
		l.Indent = 0
	}
	return l
}

// attr is the inline format text gets at the current position.
func (c *phConv) attr() (Attr, bool) {
	var s Style
	link, pre := "", false
	for i := range c.stack {
		el := &c.stack[i]
		s = el.st.apply(s)
		if el.link != "" {
			link = el.link
		} else if el.nolink {
			link = ""
		}
		pre = pre || el.pre
	}
	if pre {
		return Attr{}, true
	}
	if s&Code != 0 {
		// Slack's editor keeps inline code and links apart.
		link = ""
	}
	if link != "" {
		// Link text keeps its look, not the site's link underline.
		s &^= Underline
	}
	return Attr{Style: s, Link: link}, false
}

func (c *phConv) text(t string) {
	if c.skipping() {
		return
	}
	for i := range c.stack {
		if c.stack[i].ignore {
			if c.marker.Len() < 64 {
				c.marker.WriteString(t)
			}
			return
		}
	}
	a, pre := c.attr()
	if pre {
		if c.preStart {
			c.preStart = false
			t = strings.TrimPrefix(strings.TrimPrefix(t, "\r"), "\n")
		}
		t = strings.ReplaceAll(t, "\r\n", "\n")
		for i, part := range strings.Split(t, "\n") {
			if i > 0 {
				c.ensureLine()
				c.newLine()
			}
			if part != "" {
				c.ensureLine()
				c.emit(part, a)
			}
		}
		return
	}
	// Collapse whitespace the way a browser renders it.
	var w strings.Builder
	for _, r := range t {
		switch {
		case r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f':
			if w.Len() > 0 {
				c.ensureLine()
				c.emitSpace()
				c.emit(w.String(), a)
				w.Reset()
			}
			if !c.space && c.content {
				c.space, c.spaceA = true, a
			}
		default:
			w.WriteRune(r)
		}
	}
	if w.Len() > 0 {
		c.ensureLine()
		c.emitSpace()
		c.emit(w.String(), a)
	}
}

// emitSpace writes a pending collapsed space before new text, in the
// format of the whitespace it stands for.
func (c *phConv) emitSpace() {
	if !c.space {
		return
	}
	c.space = false
	if c.content {
		c.emit(" ", c.spaceA)
	}
}

func (c *phConv) emit(s string, a Attr) {
	d := c.b.d
	for _, r := range s {
		switch {
		case r == ' ':
			r = ' '
		case r == ObjRune || r == utf8.RuneError:
			r = ' '
		case r == '\t':
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0):
			continue
		}
		d.Runes = append(d.Runes, r)
		d.Attrs = append(d.Attrs, a)
	}
	c.content = true
	if len(d.Runes) > phMaxRunes {
		c.over = true
	}
}

// phTagStyle is the inline format a tag implies.
func phTagStyle(tag string) phStyle {
	switch tag {
	case "b", "strong", "h1", "h2", "h3", "h4", "h5", "h6", "th", "dt":
		return phStyle{set: Bold}
	case "i", "em", "cite", "var", "dfn":
		return phStyle{set: Italic}
	case "s", "del", "strike":
		return phStyle{set: Strike}
	case "u", "ins":
		return phStyle{set: Underline}
	case "code", "kbd", "samp", "tt":
		return phStyle{set: Code}
	}
	return phStyle{}
}

// phCSSDecls parses a style attribute (or rule body) into lowercase
// property → value.
func phCSSDecls(s string) map[string]string {
	if s == "" {
		return nil
	}
	if len(s) > 4096 {
		s = s[:4096]
	}
	m := map[string]string{}
	for _, decl := range strings.Split(s, ";") {
		k, v, ok := strings.Cut(decl, ":")
		if !ok {
			continue
		}
		k = strings.ToLower(strings.TrimSpace(k))
		v = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(v), "!important")))
		if k != "" {
			m[k] = strings.TrimSpace(v)
		}
	}
	return m
}

// phDeclStyle maps font-weight, font-style, text-decoration and the font
// shorthand to style changes.
func phDeclStyle(m map[string]string) phStyle {
	var p phStyle
	weight := func(v string) {
		switch {
		case v == "bold" || v == "bolder":
			p.set |= Bold
			p.clear &^= Bold
		case v == "normal" || v == "lighter":
			p.clear |= Bold
			p.set &^= Bold
		default:
			if n, err := strconv.Atoi(v); err == nil {
				if n >= 600 {
					p.set |= Bold
					p.clear &^= Bold
				} else {
					p.clear |= Bold
					p.set &^= Bold
				}
			}
		}
	}
	if f, ok := m["font"]; ok {
		for _, w := range strings.Fields(f) {
			switch w {
			case "bold", "bolder", "600", "700", "800", "900":
				weight(w)
			case "italic", "oblique":
				p.set |= Italic
			}
		}
	}
	if v, ok := m["font-weight"]; ok {
		weight(v)
	}
	if v, ok := m["font-style"]; ok {
		switch {
		case strings.HasPrefix(v, "italic") || strings.HasPrefix(v, "oblique"):
			p.set |= Italic
			p.clear &^= Italic
		case v == "normal":
			p.clear |= Italic
			p.set &^= Italic
		}
	}
	for _, k := range []string{"text-decoration", "text-decoration-line"} {
		v, ok := m[k]
		if !ok {
			continue
		}
		if strings.Contains(v, "line-through") {
			p.set |= Strike
		}
		if strings.Contains(v, "underline") {
			p.set |= Underline
		}
	}
	return p
}

// phHidden reports whether an element is invisible.
func phHidden(attrs map[string]string, css map[string]string) bool {
	if _, ok := attrs["hidden"]; ok {
		return true
	}
	return css["display"] == "none" || css["visibility"] == "hidden"
}

// phMsoLevel reads Word's "mso-list: l0 level2 lfo1" (level is 1-based).
func phMsoLevel(css map[string]string) (int, bool) {
	v, ok := css["mso-list"]
	if !ok || v == "ignore" || v == "none" {
		return 0, false
	}
	for _, f := range strings.Fields(v) {
		if n, err := strconv.Atoi(strings.TrimPrefix(f, "level")); err == nil && strings.HasPrefix(f, "level") {
			return min(max(n-1, 0), edMaxIndent), true
		}
	}
	return 0, false
}

// phOrderedMarker reports whether a list marker ("1.", "a)", "iv.") is a
// numbering rather than a bullet.
func phOrderedMarker(s string) bool {
	s = strings.TrimSpace(strings.Map(func(r rune) rune {
		if r == ' ' {
			return ' '
		}
		return r
	}, s))
	if f := strings.Fields(s); len(f) > 0 {
		s = f[0]
	}
	s = strings.TrimRight(s, ".)")
	if s == "" {
		return false
	}
	for _, r := range s {
		if !unicode.IsDigit(r) && !unicode.IsLetter(r) {
			return false
		}
	}
	return true
}

// phParseCSS collects simple class rules ("span.s1 {…}", ".x, p.y {…}")
// from a <style> block into m. Only inline formatting is kept.
func phParseCSS(s string, m map[string]phStyle) {
	if len(s) > phMaxCSS {
		s = s[:phMaxCSS]
	}
	// Drop comments (Word wraps its CSS in <!-- -->).
	for {
		i := strings.Index(s, "/*")
		if i < 0 {
			break
		}
		j := strings.Index(s[i+2:], "*/")
		if j < 0 {
			s = s[:i]
			break
		}
		s = s[:i] + s[i+2+j+2:]
	}
	s = strings.NewReplacer("<!--", "", "-->", "").Replace(s)
	for len(m) < phMaxRules {
		open := strings.IndexByte(s, '{')
		if open < 0 {
			return
		}
		cl := strings.IndexByte(s[open:], '}')
		if cl < 0 {
			return
		}
		sel, body := s[:open], s[open+1:open+cl]
		s = s[open+cl+1:]
		if strings.HasPrefix(strings.TrimSpace(sel), "@") {
			continue
		}
		st := phDeclStyle(phCSSDecls(strings.ToLower(body)))
		if st == (phStyle{}) {
			continue
		}
		for _, one := range strings.Split(sel, ",") {
			one = strings.TrimSpace(one)
			tag, class, ok := strings.Cut(one, ".")
			if !ok || class == "" || strings.ContainsAny(one, " >+~:[#*") || strings.Contains(class, ".") {
				continue
			}
			m[strings.ToLower(tag)+"."+class] = m[strings.ToLower(tag)+"."+class].merge(st)
		}
	}
}

// phImgText is the text an image stands for: an emoji shortcode or a
// unicode emoji in its alt text (Slack, GitHub and Twemoji pages). Other
// images are dropped.
func phImgText(attrs map[string]string) string {
	for _, k := range []string{"data-stringify-emoji", "data-stringify-text", "alt"} {
		v := strings.TrimSpace(attrs[k])
		if v == "" || utf8.RuneCountInString(v) > 64 {
			continue
		}
		if rtIsColonName(v) && !strings.ContainsAny(v[1:len(v)-1], " :<>\"'") {
			return v
		}
		if utf8.RuneCountInString(v) <= 8 && phAllEmoji(v) {
			return v
		}
	}
	return ""
}

func phAllEmoji(s string) bool {
	pict := false
	for _, r := range s {
		switch {
		case unicode.IsLetter(r) || unicode.IsSpace(r) || unicode.IsControl(r):
			return false
		case r == 0x200d || r == 0xa9 || r == 0xae || r == '#' || r == '*' || (r >= '0' && r <= '9'):
		case r >= 0x2190:
			pict = true
		default:
			return false
		}
	}
	return pict
}

// phSafeURL returns href if it is an absolute http(s) URL with a host or a
// mailto: address, else "".
func phSafeURL(href string) string {
	href = strings.TrimSpace(href)
	if href == "" || len(href) > phMaxURL {
		return ""
	}
	for _, r := range href {
		if r < 0x21 || r == 0x7f || r == ' ' || r == utf8.RuneError {
			return ""
		}
	}
	u, err := url.Parse(href)
	if err != nil {
		return ""
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		if u.Host == "" || u.User != nil {
			return ""
		}
	case "mailto":
		if u.Opaque == "" || strings.ContainsAny(u.Opaque, "<>\"") {
			return ""
		}
	default:
		return ""
	}
	return href
}

// phDropAutoLinks turns links whose text is their own URL back into plain
// text; the autolinker links them again on send, as for typed URLs.
func phDropAutoLinks(d *Doc) {
	for i := 0; i < len(d.Attrs); {
		u := d.Attrs[i].Link
		if u == "" {
			i++
			continue
		}
		j := i + 1
		for j < len(d.Attrs) && d.Attrs[j].Link == u {
			j++
		}
		if rtIsAutoLink(string(d.Runes[i:j]), u) {
			for k := i; k < j; k++ {
				d.Attrs[k].Link = ""
			}
		}
		i = j
	}
}

// phStripMarkers drops a bullet or number spelled out at the start of a
// list item ("•\titem", "1.\titem"): the list draws its own.
func phStripMarkers(d *Doc) {
	runes, attrs := d.Runes[:0:0], d.Attrs[:0:0]
	line, start := 0, 0
	for p := 0; p <= len(d.Runes); p++ {
		if p < len(d.Runes) && d.Runes[p] != '\n' {
			continue
		}
		cut := 0
		if rtIsList(d.Lines[line].Kind) {
			cut = phMarkerLen(d.Runes[start:p], d.Lines[line].Kind == Ordered)
		}
		runes = append(runes, d.Runes[start+cut:p]...)
		attrs = append(attrs, d.Attrs[start+cut:p]...)
		if p < len(d.Runes) {
			runes = append(runes, '\n')
			attrs = append(attrs, Attr{})
		}
		line, start = line+1, p+1
	}
	d.Runes, d.Attrs = runes, attrs
}

// phMarkerLen is the length of the leading spaces plus a spelled-out
// marker and its spaces in a list item, if text follows them.
func phMarkerLen(rs []rune, ordered bool) int {
	j := 0
	for j < len(rs) && rs[j] == ' ' {
		j++
	}
	m := j
	switch {
	case j < len(rs) && strings.ContainsRune("•◦▪‣·⁃▫○■□", rs[j]):
		m = j + 1
	case ordered:
		for m < len(rs) && m-j < 3 && rs[m] >= '0' && rs[m] <= '9' {
			m++
		}
		if m == j || m >= len(rs) || (rs[m] != '.' && rs[m] != ')') {
			m = j
		} else {
			m++
		}
	}
	if m == j || m >= len(rs) || rs[m] != ' ' {
		return j
	}
	for m < len(rs) && rs[m] == ' ' {
		m++
	}
	if m == len(rs) {
		return j
	}
	return m
}

// clipHTMLText decodes clipboard HTML bytes: UTF-8 (with or without a BOM)
// or UTF-16 (Firefox on X11 writes text/html as UTF-16).
func clipHTMLText(b []byte) string {
	switch {
	case bytes.HasPrefix(b, []byte{0xef, 0xbb, 0xbf}):
		b = b[3:]
	case bytes.HasPrefix(b, []byte{0xff, 0xfe}):
		return phUTF16(b[2:], false)
	case bytes.HasPrefix(b, []byte{0xfe, 0xff}):
		return phUTF16(b[2:], true)
	case len(b) >= 4 && b[0] != 0 && b[1] == 0 && b[2] != 0 && b[3] == 0:
		return phUTF16(b, false)
	}
	return strings.ToValidUTF8(string(b), "�")
}

func phUTF16(b []byte, be bool) string {
	u := make([]uint16, len(b)/2)
	for i := range u {
		if be {
			u[i] = uint16(b[2*i])<<8 | uint16(b[2*i+1])
		} else {
			u[i] = uint16(b[2*i+1])<<8 | uint16(b[2*i])
		}
	}
	return strings.TrimRight(string(utf16.Decode(u)), "\x00")
}

// cfHTML extracts the HTML from Windows' CF_HTML ("HTML Format") clipboard
// data: a header of byte offsets ("StartHTML:0000000105") followed by the
// document. It prefers the whole document (Word puts its list CSS in
// <head>) and falls back to the fragment, then to everything after the
// header.
func cfHTML(b []byte) []byte {
	b = bytes.TrimRight(b, "\x00")
	hdr := map[string]int{}
	end := 0
	for end < len(b) && end < 4096 {
		nl := bytes.IndexAny(b[end:], "\r\n")
		if nl < 0 {
			break
		}
		line := string(b[end : end+nl])
		k, v, ok := strings.Cut(line, ":")
		if !ok || strings.HasPrefix(line, "<") {
			break
		}
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			hdr[k] = n
		}
		end += nl
		for end < len(b) && (b[end] == '\r' || b[end] == '\n') {
			end++
		}
	}
	valid := func(s, e int) bool { return s > 0 && e > s && e <= len(b) }
	if s, e := hdr["StartHTML"], hdr["EndHTML"]; valid(s, e) {
		return b[s:e]
	}
	if s, e := hdr["StartFragment"], hdr["EndFragment"]; valid(s, e) {
		return b[s:e]
	}
	if i := bytes.IndexByte(b[end:], '<'); i >= 0 {
		return b[end+i:]
	}
	return nil
}
