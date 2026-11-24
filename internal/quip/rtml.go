package quip

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// RTML is Quip's rich-text markup in a text section's content.text and the
// title's content.title (docs/CANVASES.md "RTML"). Parsing follows
// W77Y.parse as Slack's fce5.getTextAndMarks drives it: the string goes
// into an inert document's body as <div>…</div> (u21E.htmlToRootNode), so
// the HTML parser has its say first (entities, misnesting, implied ends),
// and a tree walker then reads text, <br>, controls and marks out of it.
// Writing is the editor's getCompactRtml. The parity snapshots in
// testdata/parity hold both to what Slack's own modules give.

// Mark is a mark of fce5.getTextAndMarks: a tag over [Start, End) of the
// text, as byte offsets (Slack counts UTF-16 units over the same string).
type Mark struct {
	Name string // a, annotation, b, i, u, del, code, f, sub, sup
	// Value is Slack's mark value: for "a" the JSON fce5 writes
	// ({"href":…,"nolink":true,"slackFileId":…}), for "annotation" its id,
	// for a formatting tag its attributes as name="value" pairs. Flag is
	// set instead for a formatting tag without attributes (Slack's true).
	Value      string
	Flag       bool
	Start, End int
}

// rtmlTags is W77Y's tag set (u): what the walker reads; SPACER, P and
// INPUT are dropped with their contents, anything else is looked through.
var rtmlTags = map[string]bool{
	"A": true, "B": true, "I": true, "U": true, "DEL": true, "CODE": true, "F": true, "SUB": true, "SUP": true,
	"BR": true, "CL": true, "CLI": true, "CONTROL": true, "ANNOTATION": true,
}

// ParseRTML is fce5.getTextAndMarks. The text keeps what RTML writes for
// characters (escapeText's &amp; &lt; &gt;), each line break as "<br>"
// and each control as <control id="…"></control>; marks are listed as
// they close. ok is false where Slack's parse throws (a tag nested in the
// same tag: its mark bookkeeping loses the outer one), and then, as in
// Slack, the text is rtml itself and there are no marks.
//
// A fragment Clack's HTML parser refuses (over 512 elements deep: Chromium
// would hang the rest under the 512th) is a parse Slack's doesn't make:
// ok is false and the text is rtml, never an empty section.
func ParseRTML(rtml string) (text string, marks []Mark, ok bool) {
	root := rtmlRoot(rtml)
	p := &textAndMarks{opens: map[string]openMark{}}
	if root == nil || !walkRTML(root, p) {
		return rtml, nil, false
	}
	return p.buf.String(), p.marks, true
}

// rtmlHandler is what W77Y.parse tells its handler (class A): text, line
// breaks, controls, and where annotations, links and formatting start and
// end. markEnd reports false where Slack's handler throws, which ends the
// parse.
type rtmlHandler interface {
	text(s string)
	lineBreak()
	control(id string)
	annotationStart(id string)
	linkStart(href string, nolink bool, fileID string)
	formattingStart(name, attrs string, flag bool)
	markEnd(name string) bool
}

// rtmlRoot is htmlToRootNode: body.innerHTML = "<div>" + rtml + "</div>",
// then body's first child.
func rtmlRoot(rtml string) *html.Node {
	body := &html.Node{Type: html.ElementNode, Data: "body", DataAtom: atom.Body}
	// an inert document (createHTMLDocument) has no scripting, so
	// <noscript> holds markup, not text
	nodes, err := html.ParseFragmentWithOptions(strings.NewReader("<div>"+rtml+"</div>"), body,
		html.ParseOptionEnableScripting(false))
	if err != nil || len(nodes) == 0 {
		return nil
	}
	for _, n := range nodes {
		body.AppendChild(n)
	}
	return nodes[0]
}

type openMark struct {
	start int
	value string
	flag  bool
}

// textAndMarks is fce5's handler (class a).
type textAndMarks struct {
	buf   strings.Builder
	opens map[string]openMark
	marks []Mark
}

func (p *textAndMarks) text(s string) { p.buf.WriteString(EscapeText(s, false)) }
func (p *textAndMarks) lineBreak()    { p.buf.WriteString("<br>") }
func (p *textAndMarks) control(id string) {
	p.buf.WriteString(`<control id="` + id + `"></control>`)
}
func (p *textAndMarks) annotationStart(id string) { p.markStart("annotation", id, false) }
func (p *textAndMarks) linkStart(href string, nolink bool, fileID string) {
	p.markStart("a", linkValue(href, nolink, fileID), false)
}
func (p *textAndMarks) formattingStart(name, attrs string, flag bool) { p.markStart(name, attrs, flag) }

func (p *textAndMarks) markStart(name, value string, flag bool) {
	// fmyk.assert(no open mark of this name) only throws under Jest: the
	// new start replaces the old one, whose end then finds nothing
	p.opens[name] = openMark{start: p.buf.Len(), value: value, flag: flag}
}

// markEnd reports false where Slack's markEnd_ throws (destructuring the
// missing open mark).
func (p *textAndMarks) markEnd(name string) bool {
	o, ok := p.opens[name]
	if !ok {
		return false
	}
	p.marks = append(p.marks, Mark{Name: name, Value: o.value, Flag: o.flag, Start: o.start, End: p.buf.Len()})
	delete(p.opens, name)
	return true
}

// TreeWalker filter results.
const (
	filterAccept = 1
	filterReject = 2
	filterSkip   = 3
)

// nodeName is the DOM's nodeName for the nodes W77Y looks at: an HTML
// element's upper-cased tag name, any other element's own.
func nodeName(n *html.Node) string {
	if n.Type != html.ElementNode {
		return ""
	}
	if n.Namespace == "" {
		return strings.ToUpper(n.Data)
	}
	return n.Data
}

// rtmlFilter is W77Y's nodeFilter, under whatToShow 5 (elements and
// text; anything else is skipped, as a TreeWalker skips what it doesn't
// show).
func rtmlFilter(n *html.Node) int {
	switch n.Type {
	case html.TextNode:
		return filterAccept
	case html.ElementNode:
		name := nodeName(n)
		switch {
		case rtmlTags[name]:
			return filterAccept
		case name == "SPACER" || name == "P" || name == "INPUT":
			return filterReject
		}
	}
	return filterSkip
}

// treeWalker is the DOM's TreeWalker (nextNode, nextSibling, parentNode)
// over root with rtmlFilter.
type treeWalker struct{ root, cur *html.Node }

func (w *treeWalker) nextNode() *html.Node {
	node, result := w.cur, filterAccept
	for {
		for result != filterReject && node.FirstChild != nil {
			node = node.FirstChild
			if result = rtmlFilter(node); result == filterAccept {
				w.cur = node
				return node
			}
		}
		var sibling *html.Node
		for t := node; t != nil; t = t.Parent {
			if t == w.root {
				return nil
			}
			if sibling = t.NextSibling; sibling != nil {
				node = sibling
				break
			}
		}
		if sibling == nil {
			return nil
		}
		if result = rtmlFilter(node); result == filterAccept {
			w.cur = node
			return node
		}
	}
}

func (w *treeWalker) nextSibling() *html.Node {
	node := w.cur
	if node == w.root {
		return nil
	}
	for {
		sibling := node.NextSibling
		for sibling != nil {
			node = sibling
			result := rtmlFilter(node)
			if result == filterAccept {
				w.cur = node
				return node
			}
			sibling = node.FirstChild
			if result == filterReject || sibling == nil {
				sibling = node.NextSibling
			}
		}
		node = node.Parent
		if node == nil || node == w.root || rtmlFilter(node) == filterAccept {
			return nil
		}
	}
}

func (w *treeWalker) parentNode() *html.Node {
	for node := w.cur; node != nil && node != w.root; {
		node = node.Parent
		if node != nil && rtmlFilter(node) == filterAccept {
			w.cur = node
			return node
		}
	}
	return nil
}

func getAttr(n *html.Node, name string) (string, bool) {
	for _, a := range n.Attr {
		if a.Namespace == "" && a.Key == name {
			return a.Val, true
		}
	}
	return "", false
}

// rtmlMemo holds what walkRTML works out per node, so a walk is linear in
// the tree's size: a crafted section (marks hundreds deep, then hundreds
// of looked-through tags, then a million lines) cost minutes when every
// node searched the open marks for each of its ancestors and every mark
// rebuilt its subtree's text.
type rtmlMemo struct {
	text    map[*html.Node]bool       // hasText
	control map[*html.Node]bool       // isControlAncestor
	near    map[*html.Node]*html.Node // a node's nearest pushed ancestor (nil: none)
	at      map[*html.Node]int        // a pushed element's index in the stack
}

func newRTMLMemo() *rtmlMemo {
	return &rtmlMemo{text: map[*html.Node]bool{}, control: map[*html.Node]bool{}, near: map[*html.Node]*html.Node{}, at: map[*html.Node]int{}}
}

// hasText is textContent(n) != "": a non-empty text node under n.
func (m *rtmlMemo) hasText(n *html.Node) bool {
	if v, ok := m.text[n]; ok {
		return v
	}
	v := false
	for c := n.FirstChild; c != nil && !v; c = c.NextSibling {
		switch c.Type {
		case html.TextNode:
			v = c.Data != ""
		case html.ElementNode:
			v = m.hasText(c)
		}
	}
	m.text[n] = v
	return v
}

// isControlAncestor is W77Y's: every child that isn't an empty text node
// is a control, or a tag of the set that is itself one.
func (m *rtmlMemo) isControlAncestor(n *html.Node) bool {
	if v, ok := m.control[n]; ok {
		return v
	}
	v := isControlAncestorOf(n, m.isControlAncestor)
	m.control[n] = v
	return v
}

func isControlAncestorOf(n *html.Node, kid func(*html.Node) bool) bool {
	some := false
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.TextNode && c.Data == "" {
			continue
		}
		some = true
		if c.Type != html.ElementNode {
			return false
		}
		name := nodeName(c)
		if name != "CONTROL" && !(rtmlTags[name] && kid(c)) {
			return false
		}
	}
	return some
}

// nearest is the nearest ancestor of e under root that the walk pushed.
// It's fixed once e is reached: the walk reaches ancestors first, and an
// element stays on the stack until the walk leaves its subtree, so the
// stack is always the pushed ancestors of where the walk is.
func (m *rtmlMemo) nearest(e, root *html.Node) *html.Node {
	p := e.Parent
	if p == nil || p == root {
		return nil
	}
	if _, ok := m.at[p]; ok {
		return p
	}
	if v, ok := m.near[p]; ok {
		return v
	}
	v := m.nearest(p, root)
	m.near[p] = v
	return v
}

type stackEntry struct{ n *html.Node }

// walkRTML is W77Y.parse with a handler; false where Slack's throws.
// Neither of Clack's handlers asks for a control's children (fce5's
// control() returns true, lz is called without the flag that would), so
// they're never walked.
func walkRTML(root *html.Node, p rtmlHandler) bool {
	w := &treeWalker{root: root, cur: root}
	m := newRTMLMemo()
	var stack []stackEntry
	push := func(e *html.Node) {
		m.at[e] = len(stack)
		stack = append(stack, stackEntry{n: e})
	}
	adjust := func(e *html.Node) bool {
		// pop the marks e isn't inside: those above its nearest pushed
		// ancestor (W77Y.adjustStack_ compares e's ancestors to the stack)
		n := 0
		if e != nil {
			if a := m.nearest(e, root); a != nil {
				n = m.at[a] + 1
			}
		}
		for len(stack) > n {
			top := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			switch name := strings.ToLower(nodeName(top.n)); name {
			case "annotation":
				if !p.markEnd("annotation") {
					return false
				}
			case "a":
				if !p.markEnd("a") {
					return false
				}
			case "b", "i", "u", "del", "code", "f", "sub", "sup", "cl", "cli":
				if !p.markEnd(name) {
					return false
				}
			}
		}
		return true
	}
	skipKids := false
	for {
		var e *html.Node
		if skipKids {
			for {
				if e = w.nextSibling(); e == nil {
					if e = w.parentNode(); e == nil {
						break
					}
				} else {
					break
				}
			}
		} else {
			e = w.nextNode()
		}
		skipKids = false
		if e == nil {
			break
		}
		if !adjust(e) {
			return false
		}
		if e.Type == html.TextNode {
			p.text(e.Data)
			continue
		}
		if e.Type != html.ElementNode {
			continue
		}
		name := nodeName(e)
		switch name {
		case "BR":
			p.lineBreak()
			continue
		case "CONTROL", "ANNOTATION":
			id, _ := getAttr(e, "data-id")
			if id == "" {
				id, _ = getAttr(e, "id")
			}
			if name == "CONTROL" {
				p.control(id)
				skipKids = true
			} else {
				p.annotationStart(id)
				push(e)
			}
			continue
		}
		if !m.hasText(e) && !m.isControlAncestor(e) {
			continue
		}
		switch name {
		case "A":
			href, _ := getAttr(e, "href")
			nolink := false
			fileID := ""
			if e.Namespace == "" { // isAnchorElement: dataset only on an HTML <a>
				v, _ := getAttr(e, "data-nolink")
				nolink = v == "true"
				fileID, _ = getAttr(e, "data-slack-file-id")
			}
			p.linkStart(href, nolink, fileID)
			push(e)
		case "B", "I", "U", "DEL", "CODE", "F", "SUB", "SUP":
			// the DOM keeps an attribute's first occurrence; x/net/html
			// keeps them all
			var attrs []string
			seen := map[string]bool{}
			for _, a := range e.Attr {
				if k := attrName(a); !seen[k] {
					seen[k] = true
					attrs = append(attrs, k+`="`+a.Val+`"`)
				}
			}
			p.formattingStart(strings.ToLower(name), strings.Join(attrs, " "), len(attrs) == 0)
			push(e)
		}
	}
	return adjust(nil)
}

// attrName is an attribute's DOM name (prefix:local for a namespaced one).
func attrName(a html.Attribute) string {
	if a.Namespace != "" {
		return a.Namespace + ":" + a.Key
	}
	return a.Key
}

// linkValue is the "a" mark's value: JSON.stringify({href, nolink: true
// when set, slackFileId when set}).
func linkValue(href string, nolink bool, fileID string) string {
	var b strings.Builder
	b.WriteString(`{"href":`)
	b.WriteString(jsonQuote(href))
	if nolink {
		b.WriteString(`,"nolink":true`)
	}
	if fileID != "" {
		b.WriteString(`,"slackFileId":`)
		b.WriteString(jsonQuote(fileID))
	}
	b.WriteByte('}')
	return b.String()
}

// jsonQuote is JSON.stringify of a string: only ", \ and controls below
// U+0020 are escaped (U+2028, <, > and & stay as they are).
func jsonQuote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				b.WriteString(`\u00`)
				b.WriteString(strconv.FormatInt(int64(r)>>4, 16))
				b.WriteString(strconv.FormatInt(int64(r)&15, 16))
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// EscapeText is W77Y.escapeText: tab, CR and LF become spaces, the other
// controls (U+0000-U+001F, U+007F-U+009F) go, and unless keepEntities, &,
// < and > are written as entities. Quotes stay.
func EscapeText(s string, keepEntities bool) string {
	special := false
	for _, r := range s {
		if r == '&' || r == '<' || r == '>' || r < 0x20 || (r >= 0x7f && r <= 0x9f) {
			special = true
			break
		}
	}
	if !special {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\t' || r == '\n' || r == '\r':
			b.WriteByte(' ')
		case r < 0x20 || (r >= 0x7f && r <= 0x9f):
		case !keepEntities && r == '&':
			b.WriteString("&amp;")
		case !keepEntities && r == '<':
			b.WriteString("&lt;")
		case !keepEntities && r == '>':
			b.WriteString("&gt;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// safeSchemes is gOda.kT (http, https and the app schemes Quip links)
// plus W77Y's ftp, mailto and tel.
var safeSchemes = []string{
	"http", "https", "jamfselfservice", "keybase", "message", "notes", "slack", "slackmdm", "slackintune",
	"slackdebug", "slackbeta", "slackprototype", "smb", "xmpp", "vnc", "goodtask3", "sfanalytics", "adir",
	"alchemy", "core", "efin", "enchante", "file", "fusionx", "hdfs", "hello", "ilog", "its-conductor",
	"livability", "nfa", "phantom", "x-phantom", "rdar", "repair", "rxcloud", "snr", "snrx", "swbd",
	"thegrove", "umbrella", "unib", "xcode", "webcal", "music", "spotify", "ftp", "mailto", "tel",
}

// IsSafeURI is W77Y.isSafeUri: the link starts with one of the safe
// schemes and ":" (case-sensitive, as Slack's RegExp is); with allowAll,
// any scheme but javascript and vbscript.
func IsSafeURI(u string, allowAll bool) bool {
	for _, s := range safeSchemes {
		if strings.HasPrefix(u, s+":") {
			return true
		}
	}
	if !allowAll {
		return false
	}
	scheme, ok := uriScheme(u)
	if !ok {
		return false
	}
	switch strings.ToLower(scheme) {
	case "javascript", "vbscript":
		return false
	}
	return true
}

// uriScheme is /^([A-Za-z][A-Za-z0-9+.-]*):/.
func uriScheme(u string) (string, bool) {
	for i := 0; i < len(u); i++ {
		c := u[i]
		switch {
		case c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z':
		case i > 0 && (c >= '0' && c <= '9' || c == '+' || c == '.' || c == '-'):
		case i > 0 && c == ':':
			return u[:i], true
		default:
			return "", false
		}
	}
	return "", false
}

// CompactRTML is the editor's getCompactRtml: text (as ParseRTML gives
// it) with the marks applied, in descending density (annotation, then a,
// then f, then the rest; a stable sort, so ties keep their order), each
// nested inside the ones applied before it. ok is false for a mark that
// isn't within the text, ends before it starts, or cuts a character.
func CompactRTML(text string, marks []Mark) (rtml string, ok bool) {
	for _, m := range marks {
		if m.Start < 0 || m.Start > m.End || m.End > len(text) || !boundary(text, m.Start) || !boundary(text, m.End) {
			return "", false
		}
	}
	root := newCompactNode("", "", false, []compactPart{&compactText{text: text, start: 0}})
	order := make([]Mark, len(marks))
	copy(order, marks)
	stableSortDesc(order, markDensity)
	for _, m := range order {
		root.applyMark(m)
	}
	return root.rtml(), true
}

func boundary(s string, i int) bool { return i == len(s) || utf8.RuneStart(s[i]) }

func markDensity(m Mark) int {
	switch m.Name {
	case "annotation":
		return 3
	case "a":
		return 2
	case "f":
		return 1
	}
	return 0
}

// stableSortDesc is dHCX.sortBy(marks, key, true) under a stable sort.
func stableSortDesc(ms []Mark, key func(Mark) int) {
	for i := 1; i < len(ms); i++ {
		for j := i; j > 0 && key(ms[j-1]) < key(ms[j]); j-- {
			ms[j-1], ms[j] = ms[j], ms[j-1]
		}
	}
}

// compactPart is a node of getCompactRtml's tree: text (ca) or a tag (cc).
type compactPart interface {
	partStart() int
	partEnd() int
	handleMark(m Mark) (contained, added []compactPart, detach bool)
	rtml() string
}

type compactText struct {
	text  string
	start int
}

func (t *compactText) partStart() int { return t.start }
func (t *compactText) partEnd() int   { return t.start + len(t.text) }
func (t *compactText) rtml() string   { return t.text }

func (t *compactText) handleMark(m Mark) (contained, added []compactPart, detach bool) {
	if m.End <= t.start || t.partEnd() <= m.Start {
		return nil, nil, false
	}
	if t.start < m.Start {
		added = append(added, &compactText{text: t.text[:m.Start-t.start], start: t.start})
	}
	if m.End < t.partEnd() {
		added = append(added, &compactText{text: t.text[m.End-t.start:], start: m.End})
	}
	from, to := max(t.start, m.Start), min(t.partEnd(), m.End)
	return []compactPart{&compactText{text: t.text[from-t.start : to-t.start], start: from}}, added, true
}

// compactNode is cc. Its children are a Map keyed by start in Slack, so
// a child set at a start another child has replaces it (in place), and
// the map's insertion order decides which child findNodeContaining_ sees
// first.
type compactNode struct {
	name       string
	attrs      string
	hasAttrs   bool
	keys       []int
	kids       map[int]compactPart
	start, end int
}

func newCompactNode(name, attrs string, hasAttrs bool, parts []compactPart) *compactNode {
	n := &compactNode{name: name, attrs: attrs, hasAttrs: hasAttrs, kids: map[int]compactPart{}}
	for i, p := range parts {
		n.set(p)
		if i == 0 || p.partStart() < n.start {
			n.start = p.partStart()
		}
		if i == 0 || p.partEnd() > n.end {
			n.end = p.partEnd()
		}
	}
	return n
}

func (n *compactNode) set(p compactPart) {
	k := p.partStart()
	if _, ok := n.kids[k]; !ok {
		n.keys = append(n.keys, k)
	}
	n.kids[k] = p
}

func (n *compactNode) delete(k int) {
	if _, ok := n.kids[k]; !ok {
		return
	}
	delete(n.kids, k)
	for i, x := range n.keys {
		if x == k {
			n.keys = append(n.keys[:i], n.keys[i+1:]...)
			break
		}
	}
}

func (n *compactNode) partStart() int { return n.start }
func (n *compactNode) partEnd() int   { return n.end }

func (n *compactNode) handleMark(m Mark) (contained, added []compactPart, detach bool) {
	if n.end <= m.Start || m.End <= n.start {
		return nil, nil, false
	}
	if m.Start <= n.start && n.end <= m.End {
		return []compactPart{n}, nil, true
	}
	n.applyMark(Mark{Name: m.Name, Value: m.Value, Flag: m.Flag, Start: max(m.Start, n.start), End: min(m.End, n.end)})
	return nil, nil, false
}

func (n *compactNode) findNodeContaining(m Mark) *compactNode {
	for _, k := range n.keys {
		if c, ok := n.kids[k].(*compactNode); ok && c.start <= m.Start && m.End <= c.end {
			return c
		}
	}
	return nil
}

func (n *compactNode) applyMark(m Mark) {
	if c := n.findNodeContaining(m); c != nil {
		c.applyMark(m)
		return
	}
	var contained, added []compactPart
	snapshot := make([]compactPart, 0, len(n.keys))
	for _, k := range n.keys {
		snapshot = append(snapshot, n.kids[k])
	}
	for _, c := range snapshot {
		in, add, detach := c.handleMark(m)
		contained = append(contained, in...)
		added = append(added, add...)
		if detach {
			n.delete(c.partStart())
		}
	}
	for _, a := range added {
		n.set(a)
	}
	if len(contained) > 0 {
		attrs, has := markAttributes(m)
		n.set(newCompactNode(m.Name, attrs, has, contained))
	}
}

func (n *compactNode) rtml() string {
	keys := make([]int, len(n.keys))
	copy(keys, n.keys)
	// dHCX.sortBy by start, ascending (starts are unique map keys)
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j-1] > keys[j]; j-- {
			keys[j-1], keys[j] = keys[j], keys[j-1]
		}
	}
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(n.kids[k].rtml())
	}
	if n.name == "" {
		return b.String()
	}
	open := n.name
	if n.hasAttrs {
		open += " " + n.attrs
	}
	return "<" + open + ">" + b.String() + "</" + n.name + ">"
}

// markAttributes is getAttributes: a link's href (and data-nolink,
// data-slack-file-id) from its JSON, an annotation's id, f's attributes as
// they are; nothing for the other tags or a value that isn't a string.
func markAttributes(m Mark) (string, bool) {
	if m.Flag {
		return "", false
	}
	switch m.Name {
	case "a":
		l, ok := parseLinkValue(m.Value)
		if !ok {
			return quotedAttribute("href", m.Value), true
		}
		out := []string{quotedAttribute("href", l.href)}
		if l.nolink {
			out = append(out, `data-nolink="true"`)
		}
		if l.fileID != "" {
			out = append(out, quotedAttribute("data-slack-file-id", l.fileID))
		}
		return strings.Join(out, " "), true
	case "annotation":
		return `id="` + m.Value + `"`, true
	case "f":
		return m.Value, true
	}
	return "", false
}

func quotedAttribute(name, v string) string {
	return name + `="` + strings.ReplaceAll(v, `"`, "&quot;") + `"`
}

type linkMark struct {
	href   string
	nolink bool
	fileID string
}

// parseLinkValue reads the JSON linkValue writes (the only form a link
// mark takes in Clack).
func parseLinkValue(v string) (linkMark, bool) {
	var l linkMark
	r := jsonReader{s: v}
	if !r.lit("{") {
		return l, false
	}
	for first := true; ; first = false {
		if r.lit("}") {
			return l, r.i == len(r.s)
		}
		if !first && !r.lit(",") {
			return l, false
		}
		k, ok := r.str()
		if !ok || !r.lit(":") {
			return l, false
		}
		switch k {
		case "href":
			if l.href, ok = r.str(); !ok {
				return l, false
			}
		case "slackFileId":
			if l.fileID, ok = r.str(); !ok {
				return l, false
			}
		case "nolink":
			if !r.lit("true") {
				return l, false
			}
			l.nolink = true
		default:
			return l, false
		}
	}
}

type jsonReader struct {
	s string
	i int
}

func (r *jsonReader) lit(t string) bool {
	if strings.HasPrefix(r.s[r.i:], t) {
		r.i += len(t)
		return true
	}
	return false
}

// str reads a JSON string as jsonQuote writes it.
func (r *jsonReader) str() (string, bool) {
	if !r.lit(`"`) {
		return "", false
	}
	var b strings.Builder
	for r.i < len(r.s) {
		c := r.s[r.i]
		switch {
		case c == '"':
			r.i++
			return b.String(), true
		case c == '\\':
			if r.i+1 >= len(r.s) {
				return "", false
			}
			e := r.s[r.i+1]
			r.i += 2
			switch e {
			case '"', '\\', '/':
				b.WriteByte(e)
			case 'b':
				b.WriteByte('\b')
			case 'f':
				b.WriteByte('\f')
			case 'n':
				b.WriteByte('\n')
			case 'r':
				b.WriteByte('\r')
			case 't':
				b.WriteByte('\t')
			case 'u':
				if r.i+4 > len(r.s) {
					return "", false
				}
				v, err := strconv.ParseUint(r.s[r.i:r.i+4], 16, 16)
				if err != nil {
					return "", false
				}
				r.i += 4
				b.WriteRune(rune(v))
			default:
				return "", false
			}
		default:
			_, n := utf8.DecodeRuneInString(r.s[r.i:])
			b.WriteString(r.s[r.i : r.i+n])
			r.i += n
		}
	}
	return "", false
}
