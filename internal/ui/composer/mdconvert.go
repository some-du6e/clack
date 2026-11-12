package composer

import (
	"sort"
	"strings"
	"unicode"
)

// Mod+Shift+F, the keyboard module's "convert markdown" (boot-shared fffk
// convertMarkdown, only with the WYSIWYG composer): markdown typed as
// plain text in the message, or in the selection, becomes formatting.
// Slack runs it on Quill deltas, so this file ports it on a small
// quill-delta (compose, slice, eachLine, transformPosition) over Quill's
// view of the Doc, then reads the result back:
//
//  1. Yu2X buildChangeDeltaToFixEmbedsInTripleBackticks: inside ```
//     fences emoji become their :name: text, other embeds go and every
//     inline format (mentions and links included) comes off.
//  2. nTS+ buildMarkdownChangeDelta(contents, the character before the
//     selection, false, true), three passes over the text that
//     getTextForRegex (N7Ca) gives (embeds, inline code and, unless the
//     text holds a [text](url), links masked as "!"):
//     - buildMarkdownChangeDeltaForLists: "- ", "* ", "1. " lines (OF9+
//     AY) become list items, nested by their indent; ordered items only
//     when the text has two in a row (Dk) and each level starts at "1.";
//     - buildMarkdownChangeDeltaForCodeBlocks: ``` … ``` (u_) and
//     ｀｀｀ … ｀｀｀ (Xr) become code blocks on their own lines;
//     - buildMarkdownChangeDeltaForSingleLineFormatting: ">" and ">>>"
//     quotes, then per line inline code (q6, j5), [text](url) links (p7),
//     bold (wq, H_), italic (dc, QX) and strike (d9); markers go.
//  3. stripFormattingFromDelta (4NfZ): in inline code and code blocks,
//     emoji, mentions and links turn into plain text.
//
// Each step is applied the way Quill's updateContents does it (false
// attributes remove a format) and the selection moves like Quill's for
// an API change (transformPosition with priority). A list line's
// "border" reads back as a list inside a quote (Line.Quoted); Clack's Doc
// has no quoted code blocks and no list start numbers ("offset"), so
// those are dropped when reading back.
// Links are found with the composer's own URL finder (http(s)://, www.)
// plus e-mail addresses; Slack's detectLinks also takes bare domains and
// tel: numbers.

// ---- a small quill-delta ----

// qAttrs are an op's attributes. Values are bool, string, int or a
// qAtomRef; false is Quill's "remove this format", kept until a delta is
// applied (AttributeMap.compose keeps it, Quill's formatting drops it).
type qAttrs map[string]any

type qKind uint8

const (
	qIns qKind = iota // a text insert
	qEmb              // an embed insert (length 1)
	qRet              // a retain
	qDel              // a delete
)

// qEmbed is an embed: an emoji, or Slack's slackhorizontalrule (a divider).
type qEmbed struct {
	atom *Atom
	hr   bool
}

// qAtomRef marks the label text of a mention or message slug (Slack's
// mention and slug ops are text ops); seq tells two atoms that share a
// pointer apart.
type qAtomRef struct {
	a   *Atom
	seq int
}

type qOp struct {
	k     qKind
	text  []rune
	emb   *qEmbed
	n     int
	attrs qAttrs
}

func (o qOp) length() int {
	switch o.k {
	case qIns:
		return len(o.text)
	case qEmb:
		return 1
	}
	return o.n
}

func (o qOp) isInsert() bool { return o.k == qIns || o.k == qEmb }

type qDelta struct{ ops []qOp }

const qInf = 1 << 40

func attrsEqual(a, b qAttrs) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		w, ok := b[k]
		if !ok || v != w {
			return false
		}
	}
	return true
}

func (a qAttrs) clone() qAttrs {
	if len(a) == 0 {
		return nil
	}
	out := make(qAttrs, len(a))
	for k, v := range a {
		out[k] = v
	}
	return out
}

// attrsCompose is AttributeMap.compose: b's values win, a's fill in.
func attrsCompose(a, b qAttrs) qAttrs {
	out := qAttrs{}
	for k, v := range b {
		if v != nil {
			out[k] = v
		}
	}
	for k, v := range a {
		if _, ok := b[k]; !ok && v != nil {
			out[k] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func opsEqual(a, b qOp) bool {
	if a.k != b.k || !attrsEqual(a.attrs, b.attrs) {
		return false
	}
	switch a.k {
	case qIns:
		return string(a.text) == string(b.text)
	case qEmb:
		return a.emb == b.emb
	}
	return a.n == b.n
}

// push is Delta.push: merges with the last op, inserting before a delete.
func (d *qDelta) push(op qOp) {
	op.attrs = op.attrs.clone()
	if op.k == qIns {
		op.text = append([]rune(nil), op.text...)
	}
	index := len(d.ops)
	if index > 0 {
		last := d.ops[index-1]
		if op.k == qDel && last.k == qDel {
			d.ops[index-1] = qOp{k: qDel, n: last.n + op.n}
			return
		}
		if last.k == qDel && op.isInsert() {
			index--
			if index == 0 {
				d.ops = append([]qOp{op}, d.ops...)
				return
			}
			last = d.ops[index-1]
		}
		if attrsEqual(op.attrs, last.attrs) {
			switch {
			case op.k == qIns && last.k == qIns:
				d.ops[index-1] = qOp{k: qIns, text: append(append([]rune(nil), last.text...), op.text...), attrs: op.attrs}
				return
			case op.k == qRet && last.k == qRet:
				d.ops[index-1] = qOp{k: qRet, n: last.n + op.n, attrs: op.attrs}
				return
			}
		}
	}
	if index == len(d.ops) {
		d.ops = append(d.ops, op)
		return
	}
	d.ops = append(d.ops, qOp{})
	copy(d.ops[index+1:], d.ops[index:])
	d.ops[index] = op
}

func (d qDelta) retain(n int, at ...qAttrs) qDelta {
	if n <= 0 {
		return d
	}
	op := qOp{k: qRet, n: n}
	if len(at) > 0 && len(at[0]) > 0 {
		op.attrs = at[0]
	}
	d.push(op)
	return d
}

func (d qDelta) insert(s string, at qAttrs) qDelta {
	return d.insertRunes([]rune(s), at)
}

func (d qDelta) insertRunes(rs []rune, at qAttrs) qDelta {
	if len(rs) == 0 {
		return d
	}
	op := qOp{k: qIns, text: rs}
	if len(at) > 0 {
		op.attrs = at
	}
	d.push(op)
	return d
}

func (d qDelta) delete(n int) qDelta {
	if n <= 0 {
		return d
	}
	d.push(qOp{k: qDel, n: n})
	return d
}

func (d qDelta) length() int {
	n := 0
	for _, o := range d.ops {
		n += o.length()
	}
	return n
}

// chop drops a trailing plain retain.
func (d qDelta) chop() qDelta {
	if n := len(d.ops); n > 0 && d.ops[n-1].k == qRet && d.ops[n-1].attrs == nil {
		d.ops = d.ops[:n-1]
	}
	return d
}

func (d qDelta) concat(o qDelta) qDelta {
	out := qDelta{ops: append([]qOp(nil), d.ops...)}
	if len(o.ops) > 0 {
		out.push(o.ops[0])
		out.ops = append(out.ops, o.ops[1:]...)
	}
	return out
}

// qIter is quill-delta's OpIterator.
type qIter struct {
	ops           []qOp
	index, offset int
}

func (it *qIter) hasNext() bool { return it.peekLength() < qInf }

func (it *qIter) peekLength() int {
	if it.index < len(it.ops) {
		return it.ops[it.index].length() - it.offset
	}
	return qInf
}

// peekType is the next op's kind; an exhausted iterator retains.
func (it *qIter) peekType() qKind {
	if it.index < len(it.ops) {
		if k := it.ops[it.index].k; k == qEmb {
			return qIns
		} else {
			return k
		}
	}
	return qRet
}

func (it *qIter) next(n int) qOp {
	if n <= 0 {
		n = qInf
	}
	if it.index >= len(it.ops) {
		return qOp{k: qRet, n: qInf}
	}
	op := it.ops[it.index]
	off := it.offset
	ln := op.length()
	if n >= ln-off {
		n = ln - off
		it.index++
		it.offset = 0
	} else {
		it.offset += n
	}
	switch op.k {
	case qDel:
		return qOp{k: qDel, n: n}
	case qRet:
		return qOp{k: qRet, n: n, attrs: op.attrs}
	case qIns:
		return qOp{k: qIns, text: op.text[off : off+n], attrs: op.attrs}
	}
	return qOp{k: qEmb, emb: op.emb, attrs: op.attrs}
}

func (it *qIter) rest() []qOp {
	if !it.hasNext() {
		return nil
	}
	if it.offset == 0 {
		return it.ops[it.index:]
	}
	off, idx := it.offset, it.index
	nx := it.next(qInf)
	rest := append([]qOp{nx}, it.ops[it.index:]...)
	it.offset, it.index = off, idx
	return rest
}

// compose is Delta.compose.
func (d qDelta) compose(other qDelta) qDelta {
	a, b := &qIter{ops: d.ops}, &qIter{ops: other.ops}
	var out qDelta
	if len(other.ops) > 0 && other.ops[0].k == qRet && other.ops[0].attrs == nil {
		left := other.ops[0].n
		for a.peekType() == qIns && a.peekLength() <= left {
			left -= a.peekLength()
			out.ops = append(out.ops, a.next(qInf))
		}
		if other.ops[0].n-left > 0 {
			b.next(other.ops[0].n - left)
		}
	}
	for a.hasNext() || b.hasNext() {
		switch {
		case b.peekType() == qIns:
			out.push(b.next(qInf))
		case a.peekType() == qDel:
			out.push(a.next(qInf))
		default:
			n := min(a.peekLength(), b.peekLength())
			ao, bo := a.next(n), b.next(n)
			if bo.k == qRet {
				var no qOp
				if ao.k == qRet {
					no = qOp{k: qRet, n: n}
				} else {
					no = qOp{k: ao.k, text: ao.text, emb: ao.emb}
				}
				no.attrs = attrsCompose(ao.attrs, bo.attrs)
				out.push(no)
				if !b.hasNext() && opsEqual(out.ops[len(out.ops)-1], no) {
					return out.concat(qDelta{ops: a.rest()}).chop()
				}
			} else if bo.k == qDel && ao.k == qRet {
				out.push(bo)
			}
		}
	}
	return out.chop()
}

// slice is Delta.slice(start, end).
func (d qDelta) slice(start, end int) qDelta {
	var out qDelta
	it := &qIter{ops: d.ops}
	index := 0
	for index < end && it.hasNext() {
		var op qOp
		if index < start {
			op = it.next(start - index)
		} else {
			op = it.next(end - index)
			out.ops = append(out.ops, op)
		}
		index += op.length()
	}
	return out
}

// eachLine is Delta.eachLine: each line's ops (without the "\n") and the
// newline's attributes; a last line without "\n" gets none.
func (d qDelta) eachLine(fn func(line qDelta, at qAttrs)) {
	it := &qIter{ops: d.ops}
	var line qDelta
	for it.hasNext() {
		if it.peekType() != qIns {
			return
		}
		op := it.ops[it.index]
		start := op.length() - it.peekLength()
		idx := -1
		if op.k == qIns {
			for i := start; i < len(op.text); i++ {
				if op.text[i] == '\n' {
					idx = i - start
					break
				}
			}
		}
		switch {
		case idx < 0:
			line.push(it.next(qInf))
		case idx > 0:
			line.push(it.next(idx))
		default:
			fn(line, it.next(1).attrs)
			line = qDelta{}
		}
	}
	if line.length() > 0 {
		fn(line, nil)
	}
}

// hasTrailingNewline is 8TuA.
func (d qDelta) hasTrailingNewline() bool {
	if len(d.ops) == 0 {
		return false
	}
	last := d.ops[len(d.ops)-1]
	return last.k == qIns && len(last.text) > 0 && last.text[len(last.text)-1] == '\n'
}

// transformPosition is Delta.transformPosition.
func (d qDelta) transformPosition(index int, priority bool) int {
	it := &qIter{ops: d.ops}
	offset := 0
	for it.hasNext() && offset <= index {
		n := it.peekLength()
		k := it.peekType()
		it.next(qInf)
		if k == qDel {
			index -= min(n, index-offset)
			continue
		} else if k == qIns && (offset < index || !priority) {
			index += n
		}
		offset += n
	}
	return index
}

// qNormalize is the document Quill holds after a delta is applied: false
// formats and a zero indent gone, equal neighbours merged.
func qNormalize(d qDelta) qDelta {
	var out qDelta
	for _, op := range d.ops {
		if !op.isInsert() {
			continue
		}
		var at qAttrs
		for k, v := range op.attrs {
			if v == nil || v == false || k == "indent" && v == 0 {
				continue
			}
			if at == nil {
				at = qAttrs{}
			}
			at[k] = v
		}
		op.attrs = at
		out.push(op)
	}
	return out
}

func truthy(v any) bool {
	switch v := v.(type) {
	case nil:
		return false
	case bool:
		return v
	case string:
		return v != ""
	case int:
		return v != 0
	}
	return true
}

func attrInt(v any) int {
	switch v := v.(type) {
	case int:
		return v
	case bool:
		if v {
			return 1
		}
	case string:
		if v == "1" {
			return 1
		}
	}
	return 0
}

// ---- the Doc as Quill sees it ----

// qFromDoc is Quill's document for d (getContents()): a mention, channel,
// user group or broadcast is its label with slackmention, a message slug
// its label with slackslug, an emoji or a divider an embed, each line's
// format on its "\n", and a "\n" at the end. pos maps every Doc position
// to its Quill index.
func qFromDoc(d *Doc) (qDelta, []int) {
	var q qDelta
	pos := make([]int, d.Len()+1)
	at := 0
	line := 0
	seq := 0
	lineOp := func(l Line) {
		q = q.insert("\n", qLineAttrs(l))
		at++
	}
	if len(d.Lines) > 0 && d.Lines[0].Kind == Divider {
		q.push(qOp{k: qEmb, emb: &qEmbed{hr: true}})
		at++
	}
	for i, r := range d.Runes {
		pos[i] = at
		if r == '\n' {
			lineOp(d.Lines[line])
			line++
			if line < len(d.Lines) && d.Lines[line].Kind == Divider {
				q.push(qOp{k: qEmb, emb: &qEmbed{hr: true}})
				at++
			}
			continue
		}
		a := d.Attrs[i]
		if a.Atom != nil {
			if a.Atom.Kind == AtomEmoji {
				q.push(qOp{k: qEmb, emb: &qEmbed{atom: a.Atom}})
				at++
				continue
			}
			label := []rune(a.Atom.Text)
			if a.Atom.Kind == AtomBroadcast && len(label) == 0 {
				label = []rune("@" + a.Atom.ID)
			}
			if len(label) == 0 {
				label = []rune{'!'}
			}
			key := "slackmention"
			if a.Atom.Kind == AtomMessage {
				key = "slackslug"
			}
			seq++
			q = q.insertRunes(label, qAttrs{key: qAtomRef{a: a.Atom, seq: seq}})
			at += len(label)
			continue
		}
		q = q.insertRunes([]rune{r}, qStyleAttrs(a))
		at++
	}
	pos[d.Len()] = at
	if line < len(d.Lines) {
		lineOp(d.Lines[line])
	} else {
		lineOp(Line{})
	}
	return q, pos
}

func qLineAttrs(l Line) qAttrs {
	switch l.Kind {
	case Bullet, Ordered:
		at := qAttrs{"list": "bullet"}
		if l.Kind == Ordered {
			at["list"] = "ordered"
		}
		if l.Indent > 0 {
			at["indent"] = l.Indent
		}
		if l.Quoted {
			at["border"] = 1
		}
		return at
	case Quote:
		return qAttrs{"blockquote": true}
	case CodeBlock:
		return qAttrs{"code-block": true}
	case Header1, Header2, Header3:
		return qAttrs{"header": int(l.Kind-Header1) + 1}
	}
	return nil
}

func qStyleAttrs(a Attr) qAttrs {
	var at qAttrs
	set := func(k string, v any) {
		if at == nil {
			at = qAttrs{}
		}
		at[k] = v
	}
	for _, s := range [...]struct {
		st Style
		k  string
	}{{Bold, "bold"}, {Italic, "italic"}, {Underline, "underline"}, {Strike, "strike"}, {Code, "code"}} {
		if a.Style&s.st != 0 {
			set(s.k, true)
		}
	}
	if a.Link != "" {
		set("link", a.Link)
	}
	return at
}

// qToDoc reads a normalized Quill document back into a Doc: labels back
// into their atoms, embeds into emoji and dividers. back maps every Quill
// index to a Doc position.
func qToDoc(q qDelta) (*Doc, []int) {
	d := &Doc{}
	back := make([]int, q.length()+1)
	at := 0
	var cur Line
	divider := false
	var lastRef qAtomRef
	closeLine := func(l Line, final bool) {
		if divider {
			l = Line{Kind: Divider}
		}
		d.Lines = append(d.Lines, l)
		if !final {
			d.Runes = append(d.Runes, '\n')
			d.Attrs = append(d.Attrs, Attr{})
		}
		divider = false
		lastRef = qAtomRef{}
	}
	for oi, op := range q.ops {
		switch op.k {
		case qEmb:
			back[at] = d.Len()
			at++
			if op.emb.hr {
				divider = true
				continue
			}
			if op.emb.atom != nil {
				d.Runes = append(d.Runes, ObjRune)
				d.Attrs = append(d.Attrs, Attr{Atom: op.emb.atom})
			}
			lastRef = qAtomRef{}
		case qIns:
			for i, r := range op.text {
				back[at] = d.Len()
				at++
				if r == '\n' {
					final := oi == len(q.ops)-1 && i == len(op.text)-1
					closeLine(qLine(op.attrs), final)
					cur = Line{}
					continue
				}
				ref, isRef := op.attrs["slackmention"].(qAtomRef)
				if !isRef {
					ref, isRef = op.attrs["slackslug"].(qAtomRef)
				}
				if isRef && ref.a != nil {
					if ref == lastRef && d.Len() > 0 && d.Attrs[d.Len()-1].Atom == ref.a {
						back[at-1] = d.Len() // inside the label: after the atom
						continue
					}
					d.Runes = append(d.Runes, ObjRune)
					d.Attrs = append(d.Attrs, Attr{Atom: ref.a})
					lastRef = ref
					continue
				}
				lastRef = qAtomRef{}
				d.Runes = append(d.Runes, r)
				d.Attrs = append(d.Attrs, qAttr(op.attrs))
			}
		}
	}
	back[at] = d.Len()
	if len(d.Lines) == len(strings.Split(string(d.Runes), "\n"))-1 {
		closeLine(cur, true) // no closing "\n": the last line is a paragraph
	}
	if len(d.Lines) == 0 {
		d.Lines = []Line{{}}
	}
	return d, back
}

func qLine(at qAttrs) Line {
	if truthy(at["code-block"]) {
		return Line{Kind: CodeBlock}
	}
	switch at["list"] {
	case "bullet", "ordered":
		k := Bullet
		if at["list"] == "ordered" {
			k = Ordered
		}
		return Line{Kind: k, Indent: min(max(attrInt(at["indent"]), 0), edMaxIndent), Quoted: attrInt(at["border"]) >= 1}
	}
	if truthy(at["blockquote"]) {
		return Line{Kind: Quote}
	}
	switch attrInt(at["header"]) {
	case 1:
		return Line{Kind: Header1}
	case 2:
		return Line{Kind: Header2}
	case 3:
		return Line{Kind: Header3}
	}
	return Line{}
}

func qAttr(at qAttrs) Attr {
	var a Attr
	for _, s := range [...]struct {
		st Style
		k  string
	}{{Bold, "bold"}, {Italic, "italic"}, {Underline, "underline"}, {Strike, "strike"}, {Code, "code"}} {
		if truthy(at[s.k]) {
			a.Style |= s.st
		}
	}
	if u, ok := at["link"].(string); ok {
		a.Link = u
	}
	return a
}

func docsEqual(a, b *Doc) bool {
	if string(a.Runes) != string(b.Runes) || len(a.Lines) != len(b.Lines) || len(a.Attrs) != len(b.Attrs) {
		return false
	}
	for i := range a.Lines {
		if a.Lines[i] != b.Lines[i] {
			return false
		}
	}
	for i := range a.Attrs {
		if a.Attrs[i] != b.Attrs[i] {
			return false
		}
	}
	return true
}

// ---- getTextForRegex (N7Ca over GHlp) ----

// qText is getTextFromOps: embeds as embed, and links and inline code
// masked with it when asked.
func qText(ops []qOp, embed rune, links, code bool) []rune {
	var out []rune
	for _, op := range ops {
		switch op.k {
		case qEmb:
			out = append(out, embed)
		case qIns:
			if links && truthy(op.attrs["link"]) || code && truthy(op.attrs["code"]) {
				for range op.text {
					out = append(out, embed)
				}
				continue
			}
			out = append(out, op.text...)
		}
	}
	return out
}

// qTextForRegex is getTextForRegex: embeds and inline code as "!", and
// links too unless the text holds markdown link syntax (zI).
func qTextForRegex(ops []qOp) []rune {
	return qText(ops, '!', !mdHasLinkSyntax(qText(ops, ' ', false, false)), true)
}

// ---- OF9+ / XmNU patterns (JavaScript regex semantics, "u" flag) ----

// jsSpace is JavaScript's \s.
func jsSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', '\u00a0', '\u1680', '\u2028', '\u2029', '\u202f', '\u205f', '\u3000', '\ufeff':
		return true
	}
	return r >= '\u2000' && r <= '\u200a'
}

// jsWord is \w.
func jsWord(r rune) bool {
	return r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
}

// jsLineTerm is what "." doesn't match.
func jsLineTerm(r rune) bool { return r == '\n' || r == '\r' || r == '\u2028' || r == '\u2029' }

func jsDigit(r rune) bool { return r >= '0' && r <= '9' }

func mdQuoteRune(r rune) bool { return r == '>' || r == '＞' }

// mdBoundary is o's one-character alternatives: \s, \p{P}, +, |, =.
func mdBoundary(r rune) bool {
	return jsSpace(r) || unicode.IsPunct(r) || r == '+' || r == '|' || r == '='
}

// mdPrefixO lists, in the alternation's order, where o =
// (?:^|[>＞]{1,3}?\s|\s|\p{P}|\+|\||=|$) ends when it starts at i (no
// "m" flag: ^ and $ are the string's ends).
func mdPrefixO(s []rune, i int, out []int) []int {
	out = out[:0]
	n := len(s)
	if i == 0 {
		out = append(out, i)
	}
	for k := 1; k <= 3 && i+k < n; k++ {
		if !mdQuoteRune(s[i+k-1]) {
			break
		}
		if jsSpace(s[i+k]) {
			out = append(out, i+k+1)
		}
	}
	if i < n && mdBoundary(s[i]) {
		out = append(out, i+1)
	}
	if i == n {
		out = append(out, i)
	}
	return out
}

// mdAfterO is the lookahead (?=o) at p.
func mdAfterO(s []rune, p int) bool {
	if p == 0 || p >= len(s) {
		return true
	}
	if mdBoundary(s[p]) {
		return true
	}
	for k := 1; k <= 3 && p+k < len(s); k++ {
		if !mdQuoteRune(s[p+k-1]) {
			break
		}
		if jsSpace(s[p+k]) {
			return true
		}
	}
	return false
}

// mdAfterL is (?=l), l = (?:^|\s|\p{P}|\+|\||=|\w|$), after a closing `.
func mdAfterL(s []rune, p int) bool {
	return p == 0 || p >= len(s) || mdBoundary(s[p]) || jsWord(s[p])
}

// mdMatch is an inline match: group 1 (prefix and opening marker) is
// [start, in), group 2 (the text) [in, close); the match ends at end.
type mdMatch struct{ start, in, close, end int }

// mdAll runs a pattern like String.replace with the "g" flag: matches
// left to right, each search from the previous match's end.
func mdAll(s []rune, at func(s []rune, i int) (mdMatch, bool)) []mdMatch {
	var out []mdMatch
	for i := 0; i <= len(s); i++ {
		if m, ok := at(s, i); ok {
			out = append(out, m)
			i = m.end - 1
		}
	}
	return out
}

// mdMarkAt is generateMarkdownRegex(mark) at i:
// (o mark)((?:[^mark\s])|(?:[^mark\s][^mark\n]*[^mark\s]))(?:mark)(?=o),
// with l instead of o after a backtick.
func mdMarkAt(mark rune) func(s []rune, i int) (mdMatch, bool) {
	var buf []int
	return func(s []rune, i int) (mdMatch, bool) {
		buf = mdPrefixO(s, i, buf)
		for _, j := range buf {
			if j >= len(s) || s[j] != mark {
				continue
			}
			in := j + 1
			if in >= len(s) || s[in] == mark || jsSpace(s[in]) {
				continue
			}
			q := -1
			for k := in + 1; k < len(s); k++ {
				if s[k] == mark {
					q = k
					break
				}
				if s[k] == '\n' {
					break
				}
			}
			if q < 0 || jsSpace(s[q-1]) {
				continue
			}
			ok := mdAfterO(s, q+1)
			if mark == '`' {
				ok = mdAfterL(s, q+1)
			}
			if ok {
				return mdMatch{start: i, in: in, close: q, end: q + 1}, true
			}
		}
		return mdMatch{}, false
	}
}

func mdLN(r rune) bool { return unicode.IsLetter(r) || unicode.IsNumber(r) }

// mdCJK is the character ranges H_ lets follow a closing ＊.
func mdCJK(r rune) bool {
	for _, rg := range [...][2]rune{
		{0x3040, 0x309F}, {0x30A0, 0x30FF}, {0x31F0, 0x31FF}, {0xFF00, 0xFFEF}, {0xAC00, 0xD7AF},
		{0x1100, 0x11FF}, {0x3130, 0x318F}, {0xA960, 0xA97F}, {0xD7B0, 0xD7FF}, {0x3400, 0x4DBF},
		{0x4E00, 0x62FF}, {0x6300, 0x77FF}, {0x7800, 0x8CFF}, {0x8D00, 0x9FFF}, {0x2E80, 0x2EFF},
		{0x2F00, 0x2FDF}, {0x2FF0, 0x2FFF}, {0x3001, 0x303F}, {0x31C0, 0x31EF}, {0x3200, 0x32FF},
		{0x3300, 0x33FF}, {0xF900, 0xFAFF}, {0xFE30, 0xFE4F},
	} {
		if r >= rg[0] && r <= rg[1] {
			return true
		}
	}
	return false
}

// mdCJKBoldAt is H_, full-width ＊ bold:
// ((?:^|(?=(?!＊)(?:[^\p{L}\p{N}]＊|\P{N}＊\p{L}\P{N}))[^\r])＊)(?=\S)
// ((?:(?!＊)[^\r\n])*?(?=(?=[\p{L}\p{N}])\P{N}＊\P{N}|[^\s＊]＊(?:[^\p{L}\p{N}]|CJK|$)).)
// (?:＊(?:[^＊]|$))
func mdCJKBoldAt(s []rune, i int) (mdMatch, bool) {
	const st = '＊'
	n := len(s)
	var ins []int
	if i == 0 && n > 0 && s[0] == st {
		ins = append(ins, 1)
	}
	if i+1 < n && s[i] != st && s[i] != '\r' && s[i+1] == st {
		x := s[i]
		if !mdLN(x) || !unicode.IsNumber(x) && i+3 < n && unicode.IsLetter(s[i+2]) && !unicode.IsNumber(s[i+3]) {
			ins = append(ins, i+2)
		}
	}
	for _, in := range ins {
		if in >= n || jsSpace(s[in]) {
			continue
		}
		for t := in; t < n; t++ {
			if t > in {
				if c := s[t-1]; c == st || c == '\r' || c == '\n' {
					break
				}
			}
			r := s[t]
			if jsLineTerm(r) || t+1 >= n || s[t+1] != st {
				continue
			}
			ok := unicode.IsLetter(r) && t+2 < n && !unicode.IsNumber(s[t+2])
			if !ok && !jsSpace(r) && r != st {
				ok = t+2 == n || !mdLN(s[t+2]) || mdCJK(s[t+2])
			}
			if !ok || t+2 < n && s[t+2] == st {
				continue
			}
			end := t + 2
			if t+2 < n {
				end = t + 3
			}
			return mdMatch{start: i, in: in, close: t + 1, end: end}, true
		}
	}
	return mdMatch{}, false
}

// mdCJKCodeAt is j5, full-width ｀ code:
// ((?:^|\s|\p{P}|\p{L})｀)(?=\S)((?:(?!｀)[^\r\n])*?(?=[^｀\s]).)(?:｀(?:[^｀]|$))
func mdCJKCodeAt(s []rune, i int) (mdMatch, bool) {
	const bt = '｀'
	n := len(s)
	var ins []int
	if i == 0 && n > 0 && s[0] == bt {
		ins = append(ins, 1)
	}
	if i+1 < n && s[i+1] == bt && (jsSpace(s[i]) || unicode.IsPunct(s[i]) || unicode.IsLetter(s[i])) {
		ins = append(ins, i+2)
	}
	for _, in := range ins {
		if in >= n || jsSpace(s[in]) {
			continue
		}
		for t := in; t < n; t++ {
			if t > in {
				if c := s[t-1]; c == bt || c == '\r' || c == '\n' {
					break
				}
			}
			r := s[t]
			if r == bt || jsSpace(r) || jsLineTerm(r) || t+1 >= n || s[t+1] != bt {
				continue
			}
			if t+2 < n && s[t+2] == bt {
				continue
			}
			end := t + 2
			if t+2 < n {
				end = t + 3
			}
			return mdMatch{start: i, in: in, close: t + 1, end: end}, true
		}
	}
	return mdMatch{}, false
}

// mdLink is a p7 match: [text](url).
type mdLink struct{ start, text, textEnd, url, urlEnd, end int }

// mdLinkAt is p7, \[(?!\s*?\])(?<text>.[^[]*?)\]\((?<url>(?:(?!\]\()[^\s])*)\).
func mdLinkAt(s []rune, i int) (mdLink, bool) {
	n := len(s)
	if i >= n || s[i] != '[' || !mdLinkOpen(s, i) {
		return mdLink{}, false
	}
	t0 := i + 1
	if t0 >= n || jsLineTerm(s[t0]) {
		return mdLink{}, false
	}
	for te := t0 + 1; te+1 < n; te++ { // text = s[t0:te], lazily
		if s[te-1] == '[' && te-1 > t0 {
			break
		}
		if s[te] != ']' || s[te+1] != '(' {
			continue
		}
		u0 := te + 2
		ue := u0
		for ue < n && !jsSpace(s[ue]) && !(s[ue] == ']' && ue+1 < n && s[ue+1] == '(') {
			ue++
		}
		for e := ue; e >= u0; e-- { // greedy, backing off to a ")"
			if e < n && s[e] == ')' {
				return mdLink{start: i, text: t0, textEnd: te, url: u0, urlEnd: e, end: e + 1}, true
			}
		}
	}
	return mdLink{}, false
}

// mdLinkOpen is (?!\s*?\]) after the "[" at i.
func mdLinkOpen(s []rune, i int) bool {
	for k := i + 1; k < len(s); k++ {
		if s[k] == ']' {
			return false
		}
		if !jsSpace(s[k]) {
			return true
		}
	}
	return true
}

// mdHasLinkSyntax is zI's test, \[(?!\s*?\])(?<text>.[^[]*?)\]\((?<url>.+?)\).
func mdHasLinkSyntax(s []rune) bool {
	n := len(s)
	for i := 0; i < n; i++ {
		if s[i] != '[' || !mdLinkOpen(s, i) || i+1 >= n || jsLineTerm(s[i+1]) {
			continue
		}
		for te := i + 2; te+1 < n; te++ {
			if s[te-1] == '[' && te-1 > i+1 {
				break
			}
			if s[te] != ']' || s[te+1] != '(' {
				continue
			}
			for e := te + 3; e < n && !jsLineTerm(s[e-1]); e++ {
				if s[e] == ')' {
					return true
				}
			}
		}
	}
	return false
}

// mdFence is a code block match: group 1 [start, open), group 2 [open,
// close) (body false: the group didn't take part), group 3 [close, end).
type mdFence struct {
	start, open, close, end int
	body                    bool
}

// mdFWFence lists where [>＞]?\s?｀｀｀ can end from i, in order.
func mdFWFence(s []rune, i int, out []int) []int {
	n := len(s)
	ticks := func(p int) bool { return p+3 <= n && s[p] == '｀' && s[p+1] == '｀' && s[p+2] == '｀' }
	for _, q := range [...]bool{true, false} {
		p := i
		if q {
			if p >= n || !mdQuoteRune(s[p]) {
				continue
			}
			p++
		}
		if p < n && jsSpace(s[p]) && ticks(p+1) {
			out = append(out, p+4)
		}
		if ticks(p) {
			out = append(out, p+3)
		}
	}
	return out
}

func mdTicksAt(s []rune, p int) bool {
	return p+3 <= len(s) && s[p] == '`' && s[p+1] == '`' && s[p+2] == '`'
}

// mdFences is u_ (full: ((o)```|[>＞]?\s?｀｀｀)([\s\S]*?)?(```|[>＞]?\s?｀｀｀)(?=o))
// or Xr (((?:^|\s|\p{P}|\p{L})｀｀｀)([\s\S]*?)?(｀｀｀)(?=$|\s|\p{P}|\p{L})) over s.
func mdFences(s []rune, fullwidth bool) []mdFence {
	n := len(s)
	// where a fence (``` or ｀｀｀) starts: cheap filters for starts and ends
	fence := make([]bool, n+1)
	any := false
	for p := 0; p+3 <= n; p++ {
		if mdTicksAt(s, p) && !fullwidth || s[p] == '｀' && s[p+1] == '｀' && s[p+2] == '｀' {
			fence[p] = true
			any = true
		}
	}
	if !any {
		return nil
	}
	near := func(i, w int) bool {
		for p := i; p <= i+w && p < n; p++ {
			if fence[p] {
				return true
			}
		}
		return false
	}
	var buf []int
	opens := func(i int) []int {
		var out []int
		if fullwidth {
			if i == 0 && fence[0] && s[0] == '｀' {
				out = append(out, 3)
			}
			if i+1 < n && fence[i+1] && s[i+1] == '｀' {
				if r := s[i]; jsSpace(r) || unicode.IsPunct(r) || unicode.IsLetter(r) {
					out = append(out, i+4)
				}
			}
			return out
		}
		buf = mdPrefixO(s, i, buf)
		for _, j := range buf {
			if mdTicksAt(s, j) {
				out = append(out, j+3)
			}
		}
		return mdFWFence(s, i, out)
	}
	closes := func(b int) []int {
		if fullwidth {
			if b+3 <= n && s[b] == '｀' && s[b+1] == '｀' && s[b+2] == '｀' {
				return []int{b + 3}
			}
			return nil
		}
		var out []int
		if mdTicksAt(s, b) {
			out = append(out, b+3)
		}
		return mdFWFence(s, b, out)
	}
	after := func(e int) bool {
		if fullwidth {
			return e >= n || jsSpace(s[e]) || unicode.IsPunct(s[e]) || unicode.IsLetter(s[e])
		}
		return mdAfterO(s, e)
	}
	// candidate starts of group 3, in order
	var cands []int
	for p := 0; p < n; p++ {
		if near(p, 2) {
			cands = append(cands, p)
		}
	}
	var out []mdFence
	for i := 0; i < n; i++ {
		if !near(i, 4) {
			continue
		}
		found := false
		for _, a := range opens(i) {
			k := sort.SearchInts(cands, a+1)
			for ; k < len(cands) && !found; k++ {
				b := cands[k]
				for _, e := range closes(b) {
					if after(e) {
						out = append(out, mdFence{start: i, open: a, close: b, end: e, body: true})
						found = true
						break
					}
				}
			}
			if !found {
				for _, e := range closes(a) {
					if after(e) {
						out = append(out, mdFence{start: i, open: a, close: a, end: e})
						found = true
						break
					}
				}
			}
			if found {
				break
			}
		}
		if found {
			i = out[len(out)-1].end - 1
		}
	}
	return out
}

// mdQuotePrefixes lists where (?:[>＞]\n?(?:$| |(?=[^_])))? can end from
// i, in backtracking order ("m" flag: $ before a line end), the empty
// match last.
func mdQuotePrefixes(s []rune, i int) []int {
	var out []int
	n := len(s)
	if i < n && mdQuoteRune(s[i]) {
		for _, p := range [...]int{i + 2, i + 1} {
			if p == i+2 && (i+1 >= n || s[i+1] != '\n') {
				continue
			}
			if p >= n || jsLineTerm(s[p]) {
				out = append(out, p)
			}
			if p < n && s[p] == ' ' {
				out = append(out, p+1)
			}
			if p < n && s[p] != '_' {
				out = append(out, p)
			}
		}
	}
	return append(out, i)
}

// mdListM is an AY match: groups t [start,q), indent [q,ind), symbol
// [ind,sym), spaces [sym,sp), text [sp,rest), newline [rest,end).
type mdListM struct{ start, q, ind, sym, sp, rest, end int }

// mdLists is AY over s,
// /^((?:[>＞]\n?(?:$| |(?=[^_])))?)([\t| ]*)(\*|-|\d+\.)( +)(.*)(\n?)/gm.
func mdLists(s []rune) []mdListM {
	var out []mdListM
	n := len(s)
	for i := 0; i <= n; i++ {
		if i > 0 && !jsLineTerm(s[i-1]) {
			continue
		}
		for _, q := range mdQuotePrefixes(s, i) {
			ind := q
			for ind < n && (s[ind] == '\t' || s[ind] == '|' || s[ind] == ' ') {
				ind++
			}
			sym := ind
			switch {
			case sym < n && (s[sym] == '*' || s[sym] == '-'):
				sym++
			case sym < n && jsDigit(s[sym]):
				for sym < n && jsDigit(s[sym]) {
					sym++
				}
				if sym >= n || s[sym] != '.' {
					continue
				}
				sym++
			default:
				continue
			}
			sp := sym
			for sp < n && s[sp] == ' ' {
				sp++
			}
			if sp == sym {
				continue
			}
			rest := sp
			for rest < n && !jsLineTerm(s[rest]) {
				rest++
			}
			end := rest
			if end < n && s[end] == '\n' {
				end++
			}
			out = append(out, mdListM{start: i, q: q, ind: ind, sym: sym, sp: sp, rest: rest, end: end})
			i = end - 1
			if end == i+1 && end > 0 && s[end-1] != '\n' {
				i = end
			}
			break
		}
	}
	return out
}

// mdOrderedNext is ((?:[>＞]\n?(?:$| |(?=[^_])))?)([\t| ]*)(\d\.([ \t]).) at p.
func mdOrderedNext(s []rune, p int) bool {
	n := len(s)
	for _, q := range mdQuotePrefixes(s, p) {
		k := q
		for k < n && (s[k] == '\t' || s[k] == '|' || s[k] == ' ') {
			k++
		}
		if k+3 < n && jsDigit(s[k]) && s[k+1] == '.' && (s[k+2] == ' ' || s[k+2] == '\t') && !jsLineTerm(s[k+3]) {
			return true
		}
	}
	return false
}

// mdOrderedLine reports \d\.([ \t]).+\n at p (one digit), returning the
// position after the newline.
func mdOrderedLine(s []rune, p int) (int, bool) {
	n := len(s)
	if p+3 >= n || !jsDigit(s[p]) || s[p+1] != '.' || s[p+2] != ' ' && s[p+2] != '\t' {
		return 0, false
	}
	e := p + 3
	for e < n && !jsLineTerm(s[e]) {
		e++
	}
	if e == p+3 || e >= n || s[e] != '\n' {
		return 0, false
	}
	return e + 1, true
}

// mdDk is Dk's test: two ordered items in a row anywhere.
func mdDk(s []rune) bool {
	for i := range s {
		if nl, ok := mdOrderedLine(s, i); ok && mdOrderedNext(s, nl) {
			return true
		}
	}
	return false
}

// mdIK is IK's test, anchored at the start of s: an ordered item (after a
// quote marker and indent) followed by another.
func mdIK(s []rune) bool {
	for _, q := range mdQuotePrefixes(s, 0) {
		k := q
		for k < len(s) && (s[k] == '\t' || s[k] == '|' || s[k] == ' ') {
			k++
		}
		if nl, ok := mdOrderedLine(s, k); ok && mdOrderedNext(s, nl) {
			return true
		}
	}
	return false
}

// mdQuoteBlock is f.match(qv) || f.match(K): the first ">>>" (or
// "＞＞＞") at a line start ("m" flag); ok false when there is none.
func mdQuoteBlock(s []rune) (idx int, ok bool) {
	for _, q := range [...]rune{'>', '＞'} {
		for i := 0; i+3 <= len(s); i++ {
			if (i == 0 || jsLineTerm(s[i-1])) && s[i] == q && s[i+1] == q && s[i+2] == q {
				return i, true
			}
		}
	}
	return 0, false
}

// mdQvLine is S.match(qv) || S.match(K) on one line: ">>>" or ">>> ".
func mdQvLine(s []rune) []rune {
	for _, q := range [...]rune{'>', '＞'} {
		if len(s) >= 3 && s[0] == q && s[1] == q && s[2] == q {
			if len(s) > 3 && s[3] == ' ' {
				return s[:4]
			}
			return s[:3]
		}
	}
	return nil
}

// mdZd is zd, /^[>＞]($| |(?=[^_]))/: the matched marker (">" or "> ").
func mdZd(s []rune) []rune {
	if len(s) == 0 || !mdQuoteRune(s[0]) {
		return nil
	}
	switch {
	case len(s) == 1:
		return s[:1]
	case s[1] == ' ':
		return s[:2]
	case s[1] != '_':
		return s[:1]
	}
	return nil
}

// mdIndexOf is String.indexOf in runes.
func mdIndexOf(s, sub []rune) int {
	b := strings.Index(string(s), string(sub))
	if b < 0 {
		return -1
	}
	return len([]rune(string(s)[:b]))
}

func hasSuffixNL(s []rune) bool { return len(s) > 0 && s[len(s)-1] == '\n' }

// ---- the three passes (nTS+) ----

// mdListsDelta is buildMarkdownChangeDeltaForLists.
func mdListsDelta(e qDelta, inQuote bool) (qDelta, bool) {
	a := inQuote
	var r qDelta
	s := -1
	var o []rune
	l := 0
	type indent struct{ w, level int }
	var c []indent
	d := false
	u := map[int]int{}
	f := qTextForRegex(e.ops)
	y := mdFences(f, false)
	var b []int
	vIdx, v := mdQuoteBlock(f)
	if v {
		d, a = true, true
	}
	k := mdDk(f)
	for _, mt := range mdLists(f) {
		t, ind, sym, h := f[mt.start:mt.q], f[mt.q:mt.ind], f[mt.ind:mt.sym], f[mt.sp:mt.rest]
		whole := f[mt.start:mt.end]
		C := mt.start
		x := mt.sp
		ordered := sym[0] != '*' && sym[0] != '-'
		inCode := false
		for _, fm := range y {
			if C > fm.start && x+len(h) <= fm.end {
				inCode = true
				break
			}
		}
		if inCode {
			continue
		}
		S := 0
		for _, ch := range ind {
			if ch == '\t' {
				S += 4 - S%4
			} else {
				S++
			}
		}
		var T []indent
		for _, it := range c {
			if it.w <= S {
				T = append(T, it)
			}
		}
		if len(T) == 0 || S > T[len(T)-1].w+1 {
			lvl := S / 4
			if len(T) > 0 {
				lvl = max(lvl, T[len(T)-1].level+1)
			}
			T = append(T, indent{S, lvl})
		}
		j := min(4, T[len(T)-1].level)
		at := mdIndexOf(f, whole)
		N := f[max(at, 0):]
		E := f[max(at-2, 0):max(at, 0)]
		if ordered {
			_, has := u[j]
			if !k || j == 0 && !has && string(sym) == "1." && !mdIK(N) {
				continue
			}
			if strings.Contains(string(E), "\n\n") && string(sym) != "1." {
				u = map[int]int{}
				continue
			}
			if !has && string(sym) != "1." {
				continue
			}
		}
		c = T
		list := "bullet"
		if ordered {
			list = "ordered"
		}
		M := qAttrs{"list": list, "indent": j}
		if (len(t) > 0 || d) && (!v || C > vIdx) {
			M["border"] = 1
		}
		R := l+len(o) == C
		L := s != -1 && s < j
		if !R || L {
			u[j] = 0
		}
		if s != -1 && R && j != s && u[j] > 0 {
			M["offset"] = u[j]
		}
		u[j]++
		s, o, l = j, whole, C
		var P qDelta
		if mt.end > mt.rest {
			P = P.retain(C+len(whole)-1).retain(1, M)
		} else {
			P = P.retain(C+len(whole)).insert("\n", M)
		}
		r = r.compose(P)
		for q := C; q < x; q++ {
			b = append(b, q)
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(b)))
	for _, q := range b {
		r = r.compose(qDelta{}.retain(q).delete(1))
	}
	return r, a
}

// mdDeleteTicks is deleteBackticksAndWhitespace.
func mdDeleteTicks(s []rune, d qDelta) qDelta {
	for _, r := range s {
		if r == '`' || r == '｀' || jsSpace(r) || mdQuoteRune(r) {
			d = d.delete(1)
		} else {
			d = d.retain(1)
		}
	}
	return d
}

// mdFencesDelta is buildMarkdownChangeDeltaForCodeBlocks.
func mdFencesDelta(e qDelta, before qDelta, inQuote bool) (qDelta, bool) {
	n := inQuote
	var out qDelta
	f := qTextForRegex(e.ops)
	g := false
	yIdx, y := mdQuoteBlock(f)
	bIdx := 0
	for p := range f {
		if mdTicksAt(f, p) || p+3 <= len(f) && f[p] == '｀' && f[p+1] == '｀' && f[p+2] == '｀' {
			bIdx = p
			break
		}
	}
	if y && yIdx < bIdx {
		g = true
	} else {
		n = false
	}
	v := qTextForRegex(before.ops)
	type fm struct {
		pre, body, suf []rune
		off, btStart   int
		btEnd          int
	}
	var k []fm
	for _, fw := range [...]bool{false, true} {
		for _, m := range mdFences(f, fw) {
			if !m.body || m.close == m.open {
				continue
			}
			pre, body := f[m.start:m.open], f[m.open:m.close]
			suf := append([]rune(nil), f[m.close:m.end]...)
			if m.end < len(f) && jsSpace(f[m.end]) {
				suf = append(suf, f[m.end])
			}
			k = append(k, fm{pre: pre, body: body, suf: suf, off: m.start, btStart: m.start + len(pre) - 3, btEnd: m.start + len(pre) + len(body) + 3})
		}
	}
	sort.SliceStable(k, func(i, j int) bool { return k[i].off < k[j].off })
	var C []fm
	for _, t := range k {
		if len(C) == 0 || C[len(C)-1].btEnd <= t.btStart {
			C = append(C, t)
		}
	}
	for i, j := 0, len(C)-1; i < j; i, j = i+1, j-1 {
		C[i], C[j] = C[j], C[i]
	}
	for si, it := range C {
		o := qDelta{}.retain(it.off)
		border := mdZd(it.pre) != nil || g || n
		lineAt := func() qAttrs {
			at := qAttrs{"code-block": true}
			if border {
				at["border"] = 1
			}
			return at
		}
		nl := len(it.pre) > 0 && it.pre[0] == '\n'
		if nl {
			o = o.retain(1)
			o = mdDeleteTicks(it.pre[1:], o)
		} else {
			o = mdDeleteTicks(it.pre, o)
		}
		if !nl && (it.off > 0 || len(v) > 0 && !hasSuffixNL(v)) {
			o = o.insert("\n", nil)
		}
		if si+1 < len(C) && it.off == C[si+1].btEnd {
			o = o.insert("\n", nil)
		}
		lines := strings.Split(string(it.body), "\n")
		for t, ln := range lines {
			lr := len([]rune(ln))
			o = o.retain(lr)
			if t == 0 && lr == 0 {
				o = o.delete(1)
			} else if t != len(lines)-1 {
				o = o.retain(1, lineAt())
			}
		}
		if lines[len(lines)-1] != "" {
			o = o.insert("\n", lineAt())
		}
		o = mdDeleteTicks(it.suf, o)
		out = out.compose(o)
	}
	return out, n
}

func mdBlockAttrs(at qAttrs) bool {
	_, c := at["code-block"]
	_, q := at["blockquote"]
	_, l := at["list"]
	return c || q || l
}

type mdInlineKind struct {
	name string
	at   func(s []rune, i int) (mdMatch, bool)
}

// mdInlineKinds is w in nTS+, in the order the pass runs them: code
// first, then link (p7, handled apart), bold, italic, strike.
var mdInlineKinds = []mdInlineKind{
	{"code", mdMarkAt('`')}, {"code", mdCJKCodeAt},
	{"bold", mdMarkAt('*')}, {"bold", mdCJKBoldAt},
	{"italic", mdMarkAt('_')}, {"italic", mdMarkAt('＿')},
	{"strike", mdMarkAt('~')},
}

// mdInlineDelta is buildMarkdownChangeDeltaForSingleLineFormatting.
func mdInlineDelta(e qDelta, before *qDelta, inQuote bool) (qDelta, bool) {
	n := inQuote
	var r []rune
	if before != nil {
		r = qTextForRegex(before.ops)
	}
	var s qDelta
	o := 0
	var l []int
	type lineT struct {
		ops   []qOp
		attrs qAttrs
	}
	var d []lineT
	e.eachLine(func(line qDelta, at qAttrs) { d = append(d, lineT{line.ops, at}) })
	if len(d) == 0 {
		return s, n
	}
	trailingNL := e.hasTrailingNewline()
	m := false
	for li := range d {
		a := d[li].attrs
		f, x := false, false
		S := qTextForRegex(d[li].ops)
		skip := false
		switch {
		case mdBlockAttrs(a):
			m = false
		case m:
			f = true
			if z := mdZd(S); z != nil && mdQvLine(S) == nil {
				x = true
				for t := range z {
					l = append(l, o+t)
				}
				o += len(z)
				S = S[len(z):]
			}
		case li != 0 || before == nil || len(r) == 0 || hasSuffixNL(r):
			ev := mdQvLine(S)
			t := mdZd(S)
			av := ev
			if av == nil {
				av = t
			}
			if li == 0 && t != nil {
				if w := string(S); w == ">" || w == "＞" || w == "> " || w == "＞ " {
					skip = true // returns from the line's callback before o moves on
					break
				}
			}
			if ev == nil && n && t != nil {
				x = true
			}
			if av != nil {
				f = true
				for q := range av {
					l = append(l, o+q)
				}
				o += len(av)
				S = S[len(av):]
				if ev != nil {
					m, n = true, true
					if len(S) == 0 {
						s = s.compose(qDelta{}.retain(o + len(S)).delete(1))
						skip = true
						break
					}
				}
				if z := mdZd(S); z != nil {
					x = true
					for q := range z {
						l = append(l, o+q)
					}
					o += len(z)
					S = S[len(z):]
				}
			}
		}
		if skip {
			continue
		}
		var prevB, nextB int
		if li > 0 {
			prevB = attrInt(d[li-1].attrs["border"])
		}
		nextQuote := false
		if li+1 < len(d) {
			nextB = attrInt(d[li+1].attrs["border"])
			if ops := d[li+1].ops; len(ops) > 0 && ops[0].k == qIns && mdZd(ops[0].text) != nil {
				nextQuote = true
			}
		}
		empty := len(a) == 0
		if (prevB >= 1 && nextB >= 1 || prevB >= 1 && nextQuote && n) && len(S) > 0 && empty {
			f = true
		}
		if prevB >= 1 && empty && li+1 != len(d)-1 && n {
			f = true
		}
		if f {
			at := qAttrs{"blockquote": true}
			if x {
				at["border"] = 1
			}
			q := qDelta{}.retain(o + len(S))
			if trailingNL || li != len(d)-1 {
				q = q.retain(1, at)
			} else {
				q = q.insert("\n", at)
			}
			s = s.compose(q)
		}
		if len(S) > 0 && !truthy(a["code-block"]) {
			var codeR, linkR [][2]int
			inCode := func(p, q int) bool {
				for _, c := range codeR {
					if p >= c[0] && p <= c[1] || q >= c[0] && q <= c[1] {
						return true
					}
				}
				return false
			}
			apply := func(k mdInlineKind) {
				for _, mt := range mdAll(S, k.at) {
					dd := o + mt.in
					uu := mt.close - mt.in
					mm := dd + uu
					inLink := false
					for _, lr := range linkR {
						if dd >= lr[0] && mm <= lr[1] {
							inLink = true
							break
						}
					}
					if inLink || k.name == "italic" && string(S[mt.in:mt.close]) == "(ツ)" {
						continue
					}
					if k.name == "code" {
						codeR = append(codeR, [2]int{dd, mm})
					} else if inCode(dd, mm) {
						continue
					}
					s = s.compose(qDelta{}.retain(dd).retain(uu, qAttrs{k.name: true}))
					l = append(l, dd-1, dd+uu)
				}
			}
			apply(mdInlineKinds[0])
			apply(mdInlineKinds[1])
			// link (p7)
			for i := 0; i < len(S); i++ {
				lk, ok := mdLinkAt(S, i)
				if !ok {
					continue
				}
				i = lk.end - 1
				dd, uu := o+lk.start, o+lk.end
				if inCode(dd, uu) {
					continue
				}
				m := mdUnbalanced(S[lk.start:lk.end])
				p := mdUnbalanced(S[lk.url:lk.urlEnd])
				links := mdDetectLinks(p)
				if len(links) != 1 || !mdValidLink(links[0].text, p) {
					continue
				}
				g := o + lk.start + 1
				fl := lk.textEnd - lk.text
				y := g + fl
				A := o + lk.start + len(m)
				s = s.compose(qDelta{}.retain(g).retain(fl, qAttrs{"link": links[0].url}))
				l = append(l, g-1)
				for q := y; q < A; q++ {
					l = append(l, q)
				}
				linkR = append(linkR, [2]int{g, A})
			}
			for _, k := range mdInlineKinds[2:] {
				apply(k)
			}
		}
		o += len(S) + 1
	}
	sort.Sort(sort.Reverse(sort.IntSlice(l)))
	for _, q := range l {
		s = s.compose(qDelta{}.retain(q).delete(1))
	}
	return s, n
}

// mdUnbalanced is ignoreUnbalancedParentheses (MFFw): up to the first ")"
// that closes more than were opened.
func mdUnbalanced(s []rune) []rune {
	open, shut := 0, 0
	for i, r := range s {
		if r == '(' || r == ')' {
			if r == '(' {
				open++
			} else {
				shut++
			}
			if shut > open {
				return s[:i]
			}
		}
	}
	return s
}

type mdFound struct{ text, url string }

// mdDetectLinks stands in for detectLinks (Q2rQ): the composer's URL
// finder, and an e-mail address (with or without mailto:) as one
// mailto: link.
func mdDetectLinks(p []rune) []mdFound {
	str := string(p)
	if em := strings.TrimPrefix(str, "mailto:"); mdEmail(em) {
		return []mdFound{{text: em, url: "mailto:" + em}}
	}
	var out []mdFound
	for _, m := range rtFindURLs(p) {
		out = append(out, mdFound{text: string(p[m.start:m.end]), url: m.url})
	}
	return out
}

// mdEmail is a plain address, local@domain.tld.
func mdEmail(s string) bool {
	at := strings.IndexByte(s, '@')
	if at <= 0 || at != strings.LastIndexByte(s, '@') {
		return false
	}
	local, dom := s[:at], s[at+1:]
	for _, r := range local {
		if !(r < 0x80 && (unicode.IsLetter(r) || unicode.IsDigit(r)) || strings.ContainsRune("._%+-", r)) {
			return false
		}
	}
	dot := strings.LastIndexByte(dom, '.')
	if dot <= 0 || len(dom)-dot-1 < 2 {
		return false
	}
	for _, r := range dom {
		if !(r < 0x80 && (unicode.IsLetter(r) || unicode.IsDigit(r)) || r == '.' || r == '-') {
			return false
		}
	}
	for _, r := range dom[dot+1:] {
		if !unicode.IsLetter(r) {
			return false
		}
	}
	return true
}

// mdValidLink is isValidLinkForMrkdwn: the whole url is the link, or a
// mailto: whose address is.
func mdValidLink(text string, p []rune) bool {
	s := string(p)
	if rest, ok := strings.CutPrefix(strings.ToLower(s), "mailto:"); ok {
		return s[len(s)-len(rest):] == text
	}
	return text == s
}

// mdStripCode is stripFormattingFromDelta with its defaults
// (removeEmbedsFromCode): in inline code and code block lines, emoji
// become their text and mentions, slugs and links plain text.
func mdStripCode(doc qDelta) qDelta {
	var y qDelta
	doc.eachLine(func(line qDelta, at qAttrs) {
		ops := append(append([]qOp(nil), line.ops...), qOp{k: qIns, text: []rune{'\n'}, attrs: at})
		codeLine := truthy(at["code-block"])
		for _, op := range ops {
			c := op.length()
			if (op.isInsert() && truthy(op.attrs["code"])) || codeLine {
				t := op.attrs.clone()
				if codeLine {
					t = nil
				}
				switch {
				case op.k == qEmb && op.emb.atom != nil:
					y = y.delete(1).insert(emojiText(op.emb.atom), t)
					continue
				case op.k == qIns && op.attrs["slackslug"] != nil:
					delete(t, "slackslug")
					y = y.delete(len(op.text)).insertRunes(op.text, t)
					continue
				case op.k == qIns && op.attrs["slackmention"] != nil:
					delete(t, "slackmention")
					y = y.delete(len(op.text)).insertRunes(op.text, t)
					continue
				case op.k == qIns && truthy(op.attrs["link"]):
					delete(t, "link")
					y = y.delete(len(op.text)).insertRunes(op.text, t)
					continue
				}
			}
			y = y.retain(c)
		}
	})
	return y
}

func emojiText(a *Atom) string {
	if a.Text != "" {
		return a.Text
	}
	return ":" + a.ID + ":"
}

// mdFixFenceEmbeds is buildChangeDeltaToFixEmbedsInTripleBackticks (Yu2X).
func mdFixFenceEmbeds(e qDelta) qDelta {
	var ranges [][2]int
	for _, m := range mdFences(qTextForRegex(e.ops), false) {
		ln := 0
		if m.body {
			ln = m.close - m.open
		}
		ranges = append(ranges, [2]int{m.open, ln})
	}
	off := qAttrs{"bold": false, "italic": false, "strike": false, "code": false, "underline": false,
		"slackmention": false, "slackslug": false, "link": false, "unlink": false}
	var t qDelta
	for i := len(ranges) - 1; i >= 0; i-- {
		d := qDelta{}.retain(ranges[i][0])
		for _, op := range e.slice(ranges[i][0], ranges[i][0]+ranges[i][1]).ops {
			if op.k == qEmb {
				d = d.delete(1)
				if op.emb.atom != nil {
					d = d.insert(emojiText(op.emb.atom), nil)
				}
				continue
			}
			d = d.retain(op.length(), off)
		}
		t = t.compose(d)
	}
	return t
}

// mdBuildChange is buildMarkdownChangeDelta(e, before, a, lists).
func mdBuildChange(e qDelta, before *qDelta, a, lists bool) qDelta {
	s := false
	o := a || lists
	var l, c qDelta
	if o {
		l, s = mdListsDelta(e, s)
		c = e.compose(l)
	}
	var bd qDelta
	if before != nil {
		bd = *before
	}
	src := e
	if o {
		src = c
	}
	d, u := mdFencesDelta(src, bd, s)
	s = u
	in := e.compose(d)
	if o {
		in = c.compose(d)
	}
	p, _ := mdInlineDelta(in, before, s)
	var all qDelta
	if o {
		all = l.compose(d).compose(p)
	} else {
		all = d.compose(p)
	}
	return all.compose(mdStripCode(e.compose(all)))
}

// mdConvert is convertMarkdown on d with the selection anchor..caret: the
// new document and selection, ok false when nothing changed.
func mdConvert(d *Doc, anchor, caret int) (*Doc, int, int, bool) {
	q, pos := qFromDoc(d)
	lo, hi := min(anchor, caret), max(anchor, caret)
	collapsed := lo == hi
	qlo, qhi := pos[lo], pos[hi]
	var lead qDelta
	var before *qDelta
	if !collapsed && qlo > 0 {
		lead = lead.retain(qlo)
		b := q.slice(qlo-1, qlo)
		before = &b
	}
	r := q
	if !collapsed {
		r = q.slice(qlo, qhi)
	}
	fix := mdFixFenceEmbeds(r)
	n := qhi - qlo
	if !collapsed {
		n = r.compose(fix).length()
	}
	ch1 := lead.concat(fix)
	q2 := qNormalize(q.compose(ch1))
	l := q2
	if !collapsed {
		l = q2.slice(qlo, qlo+n)
	}
	ch2 := lead.concat(mdBuildChange(l, before, false, true))
	q3 := qNormalize(q2.compose(ch2))
	nd, back := qToDoc(q3)
	move := func(p int) int {
		p = ch2.transformPosition(ch1.transformPosition(pos[p], true), true)
		return back[min(max(p, 0), len(back)-1)]
	}
	na, nc := move(anchor), move(caret)
	if docsEqual(d, nd) {
		return d, anchor, caret, false
	}
	return nd, nd.clamp(na), nd.clamp(nc), true
}

// ConvertMarkdown is the keyboard module's "convert markdown" binding
// (Mod+Shift+F) on the editor: the message, or the selection, converted
// as one undo step. Markup mode (plain_text_mode) has no such binding.
func (e *Editor) ConvertMarkdown() bool {
	e.init()
	if e.plainText {
		return false
	}
	nd, a, c, ok := mdConvert(e.doc, e.anchor, e.caret)
	if !ok {
		return true
	}
	e.begin(edOther, true)
	e.doc = nd
	e.anchor, e.caret = a, c
	e.pendingOK = false
	e.lastKind = edNone
	e.commit()
	return true
}
