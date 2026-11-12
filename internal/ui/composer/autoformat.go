package composer

import (
	"unicode"
)

// Live markdown, the way Slack's composer does it (boot-shared fffk:
// SlackMarkdown, the keyboard module's "list autofill", XmNU/OF9+ for the
// patterns): typing the marker that closes (or opens) *bold*, _italic_,
// ~strike~ or `code` styles the inner text and drops the markers; ">" at
// the start of a line makes it a quote at once; "- ", "* ", "• ", "1. "
// or "1) " at the start of a line (after any spaces) a list; "```" a code
// block; "# ", "## " or "### " a heading and "--- " a divider (the
// composer supports portable content, so convertHeaders with
// maxHeaderLevel 3 and convertHorizontalRule are on).
//
// These are pure functions over a *Doc and a caret so they can be tested
// without a window. The editor pushes an undo step holding the literal
// text before calling them, so Ctrl+Z right after an autoformat brings the
// typed markers back.

// afMarkerStyle maps an inline markdown marker to its style.
func afMarkerStyle(r rune) (Style, bool) {
	switch r {
	case '*':
		return Bold, true
	case '_':
		return Italic, true
	case '~':
		return Strike, true
	case '`':
		return Code, true
	}
	return 0, false
}

// edSpace reports whether r is horizontal whitespace.
func edSpace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\u00a0' || r == '\u3000'
}

// edWordRune reports whether r is part of a word for word-wise motion and
// for markdown marker boundaries.
func edWordRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.Is(unicode.Mn, r)
}

// autoformatInline applies an inline style after the rune at caret-1 was
// typed, if that rune closes (or opens) a marker pair. It returns the new
// caret (the end of the styled text) and the style it applied.
//
// Slack's buildLocalMarkdownChangeDelta: the pair is the typed marker and
// the nearest same marker before it on the line, or, when there is none
// before, the nearest one after it. generateMarkdownRegex decides: the
// opening marker starts the line or follows whitespace, punctuation
// (\p{P}), "+", "|" or "="; the inner text is non-empty, starts and ends
// with neither whitespace nor the marker and holds no marker; the closing
// marker ends the line or is followed by whitespace, punctuation, "+",
// "|" or "=" (for ` also a word character). Nothing happens on code block
// lines or inside inline code.
func autoformatInline(d *Doc, caret int) (newCaret int, st Style, ok bool) {
	k := caret - 1
	if k < 0 || k >= d.Len() || d.Attrs[k].Atom != nil {
		return caret, 0, false
	}
	marker := d.Runes[k]
	st, ok = afMarkerStyle(marker)
	if !ok {
		return caret, 0, false
	}
	if d.Attrs[k].Style&Code != 0 {
		return caret, 0, false
	}
	line := d.LineOf(k)
	if d.Lines[line].Kind == CodeBlock {
		return caret, 0, false
	}
	ls, le := d.LineStart(line), d.LineEnd(line)
	open, closing := -1, -1
	for i := k - 1; i >= ls; i-- {
		if d.Runes[i] == marker && d.Attrs[i].Atom == nil {
			open, closing = i, k
			break
		}
	}
	if open < 0 {
		for i := k + 1; i < le; i++ {
			if d.Runes[i] == marker && d.Attrs[i].Atom == nil {
				open, closing = k, i
				break
			}
		}
	}
	if open < 0 || !afPair(d, ls, le, open, closing, marker) {
		return caret, 0, false
	}
	d.Delete(closing, closing+1)
	d.Delete(open, open+1)
	d.SetStyle(open, closing-1, st, true)
	if closing == k {
		return closing - 1, st, true
	}
	return caret - 1, st, true // the opening marker went: the caret moves back with the text
}

// afPair checks generateMarkdownRegex for markers at open < closing on
// the line [ls, le).
func afPair(d *Doc, ls, le, open, closing int, marker rune) bool {
	if closing-open < 2 {
		return false // nothing inside
	}
	if d.Attrs[open].Style&Code != 0 || d.Attrs[closing].Style&Code != 0 {
		return false
	}
	first, last := d.Runes[open+1], d.Runes[closing-1]
	if edSpace(first) || first == marker || edSpace(last) || last == marker {
		return false
	}
	if open > ls && !afBoundary(d, open-1, false) {
		return false
	}
	if closing+1 < le && !afBoundary(d, closing+1, marker == '`') {
		return false
	}
	return true
}

// afBoundary is OF9+'s marker neighbour classes: whitespace, \p{P}, "+",
// "|", "="; word also takes \w (the character after a closing `). An
// atom (emoji or mention pill) is none of them.
func afBoundary(d *Doc, i int, word bool) bool {
	if d.Attrs[i].Atom != nil {
		return false
	}
	r := d.Runes[i]
	switch {
	case unicode.IsSpace(r), unicode.IsPunct(r), r == '+', r == '|', r == '=':
		return true
	case word:
		return r == '_' || r < 0x80 && (unicode.IsLetter(r) || unicode.IsDigit(r))
	}
	return false
}

// afBlockPrefix reports the block kind a typed line prefix stands for:
// the keyboard module's "list autofill" prefix
// /^\s*?(1(\.|\)|-|\*|\u2022|\[ ?\]|\[x\])$/ before the typed space
// (the check-list forms are canvases' only), and "```".
func afBlockPrefix(rs []rune) (BlockKind, bool) {
	if s := string(rs); s == "```" || s == "｀｀｀" {
		return CodeBlock, true
	}
	n := len(rs)
	if n < 2 || rs[n-1] != ' ' {
		return Para, false
	}
	i := 0
	for i < n-1 && unicode.IsSpace(rs[i]) {
		i++
	}
	switch string(rs[i : n-1]) {
	case "-", "*", "•":
		return Bullet, true
	case "1.", "1)":
		return Ordered, true
	}
	return Para, false
}

// autoformatBlock converts the caret's paragraph to a list or code block
// when the text between the line start and the caret is one of the
// markdown block prefixes. The prefix is removed; it returns the new caret.
//
// List autofill also runs on a quote line (its binding only excludes
// list, code and code-block lines) and then sets border "1" with the list
// (`l = s?.blockquote ? "1" : "0"`, convertShortcutToBlockFormat): the
// quote line becomes a list item inside the quote (Line.Quoted).
func autoformatBlock(d *Doc, caret int) (newCaret int, k BlockKind, ok bool) {
	caret = d.clamp(caret)
	line := d.LineOf(caret)
	quoted := d.Lines[line].Kind == Quote
	if d.Lines[line].Kind != Para && !quoted {
		return caret, Para, false
	}
	ls := d.LineStart(line)
	if caret-ls < 2 || caret-ls > 16 {
		return caret, Para, false
	}
	for i := ls; i < caret; i++ {
		if d.Attrs[i].Atom != nil || d.Attrs[i].Style&Code != 0 {
			return caret, Para, false
		}
	}
	k, ok = afBlockPrefix(d.Runes[ls:caret])
	if !ok || quoted && k == CodeBlock {
		return caret, Para, false
	}
	d.Delete(ls, caret)
	d.Lines[line] = Line{Kind: k, Quoted: quoted}
	return ls, k, true
}

// autoformatQuote is buildLocalBlockQuoteMarkdownChangeDelta: ">" (or
// "＞") typed at the start of a paragraph turns the line into a quote at
// once and the marker goes; the rest of the line stays. It returns the
// new caret.
func autoformatQuote(d *Doc, caret int) (newCaret int, ok bool) {
	k := caret - 1
	if k < 0 || k >= d.Len() || d.Attrs[k].Atom != nil || d.Attrs[k].Style&Code != 0 {
		return caret, false
	}
	if r := d.Runes[k]; r != '>' && r != '＞' {
		return caret, false
	}
	line := d.LineOf(k)
	if d.LineStart(line) != k || d.Lines[line].Kind != Para {
		return caret, false
	}
	d.Delete(k, caret)
	d.Lines[line] = Line{Kind: Quote}
	return k, true
}

// autoformatHeading is SlackMarkdown's formatHeader: "#", "##" or "###"
// then a space at the start of a line make it a Big, Medium or Small
// heading (prefix ^#{1,3}$; not in a heading, a code block or inline
// code). The marks and the space go. It returns the new caret.
func autoformatHeading(d *Doc, caret int) (newCaret int, k BlockKind, ok bool) {
	caret = d.clamp(caret)
	line := d.LineOf(caret)
	switch kind := d.Lines[line].Kind; {
	case kind == CodeBlock, kind == Divider, isHeading(kind):
		return caret, Para, false
	}
	ls := d.LineStart(line)
	n := caret - ls - 1 // the #s before the typed space
	if n < 1 || n > 3 || d.Runes[caret-1] != ' ' {
		return caret, Para, false
	}
	for i := ls; i < caret-1; i++ {
		if d.Runes[i] != '#' || d.Attrs[i].Atom != nil || d.Attrs[i].Style&Code != 0 {
			return caret, Para, false
		}
	}
	k = [...]BlockKind{Header1, Header2, Header3}[n-1]
	d.Delete(ls, caret)
	d.Lines[line] = Line{Kind: k}
	return ls, k, true
}

// afHorizontalRule reports whether the space just typed at caret-1
// follows "---" at the start of the line (formatHorizontalRule's binding:
// prefix ^---$, not in code); the editor then puts a divider there.
func afHorizontalRule(d *Doc, caret int) bool {
	caret = d.clamp(caret)
	line := d.LineOf(caret)
	if k := d.Lines[line].Kind; k == CodeBlock || k == Divider {
		return false
	}
	ls := d.LineStart(line)
	if caret-ls != 4 || d.Runes[caret-1] != ' ' {
		return false
	}
	for i := ls; i < ls+3; i++ {
		if d.Runes[i] != '-' || d.Attrs[i].Atom != nil || d.Attrs[i].Style&Code != 0 {
			return false
		}
	}
	return true
}

// autoLinkBefore is SlackLinkModule.maybeLinkify, run on a typed space or
// Enter: the word before pos (after the line's last space) becomes a link
// if it is a URL (or holds one) that isn't already a link, inside inline
// code or on a code block line. It reports whether it linked.
func autoLinkBefore(d *Doc, pos int) bool {
	pos = d.clamp(pos)
	line := d.LineOf(pos)
	if d.Lines[line].Kind == CodeBlock {
		return false
	}
	ls := d.LineStart(line)
	start := pos
	for start > ls && d.Runes[start-1] != ' ' {
		start--
	}
	if start == pos {
		return false
	}
	for i := start; i < pos; i++ {
		if d.Attrs[i].Atom != nil {
			return false
		}
	}
	ms := rtFindURLs(d.Runes[start:pos])
	if len(ms) == 0 {
		return false
	}
	m := ms[len(ms)-1]
	a, b := start+m.start, start+m.end
	for i := a; i < b; i++ {
		if d.Attrs[i].Link != "" || d.Attrs[i].Style&Code != 0 {
			return false
		}
	}
	d.SetLink(a, b, m.url)
	return true
}

// autoformatFence handles "```" typed after text on a line, as Slack's
// composer does: "hello ```" ends the paragraph at "hello" and opens a code
// block on a new line below, caret inside it. Text after the caret moves
// into the code block. (A fence at the start of a paragraph is
// autoformatBlock's.) It returns the new caret.
func autoformatFence(d *Doc, caret int) (newCaret int, ok bool) {
	caret = d.clamp(caret)
	line := d.LineOf(caret)
	ls := d.LineStart(line)
	if caret-ls < 4 {
		return caret, false
	}
	switch d.Lines[line].Kind {
	case CodeBlock, Divider:
		return caret, false
	}
	// "```" or "｀｀｀" (buildLocalCodeBlockMarkdownChangeDelta takes both)
	tick := d.Runes[caret-1]
	if tick != '`' && tick != '｀' {
		return caret, false
	}
	for i := caret - 3; i < caret; i++ {
		if d.Runes[i] != tick || d.Attrs[i].Atom != nil || d.Attrs[i].Style&Code != 0 {
			return caret, false
		}
	}
	// A fourth backtick before is someone typing ```` (or ``` inside a
	// longer run): leave it.
	if d.Runes[caret-4] == tick {
		return caret, false
	}
	start := caret - 3
	d.Delete(start, caret)
	// "hello ```" keeps "hello": the space before the fence goes too.
	cut := start
	for cut > ls && edSpace(d.Runes[cut-1]) && d.Attrs[cut-1].Atom == nil {
		cut--
	}
	d.Delete(cut, start)
	if cut == ls {
		// Only whitespace before the fence: the line itself becomes code.
		d.Lines[line] = Line{Kind: CodeBlock}
		return ls, true
	}
	d.Insert(cut, "\n", Attr{})
	d.Lines[line+1] = Line{Kind: CodeBlock}
	// The code block starts plain: drop inline styles carried over from
	// the text after the caret.
	end := d.LineEnd(line + 1)
	d.SetStyle(cut+1, end, Bold|Italic|Underline|Strike|Code, false)
	d.SetLink(cut+1, end, "")
	return cut + 1, true
}

// autoformatFenceInCode is buildLocalCodeBlockMarkdownChangeDelta's
// "remove" action (boot-shared KT9Q): "```" (or "｀｀｀") typed on a code
// block line, ending it (the next character is the line's newline), goes,
// and a plain line is added after the line (retain(r).delete(3).retain(1)
// .insert("\n")). The text change queue applies that as n.diff(r) with
// updateContents(…, "user"), and Quill's shiftRange then puts the caret at
// the start of the new line (checked with quill-delta: transformPosition
// of the caret through the diff). Not on inline code (c?.code). It
// returns the new caret.
func autoformatFenceInCode(d *Doc, caret int) (newCaret int, ok bool) {
	caret = d.clamp(caret)
	line := d.LineOf(caret)
	if d.Lines[line].Kind != CodeBlock || caret != d.LineEnd(line) {
		return caret, false
	}
	r := caret - 3
	if r < d.LineStart(line) {
		return caret, false
	}
	tick := d.Runes[caret-1]
	if tick != '`' && tick != '｀' {
		return caret, false
	}
	for i := r; i < caret; i++ {
		if d.Runes[i] != tick || d.Attrs[i].Atom != nil {
			return caret, false
		}
	}
	if d.Attrs[caret-1].Style&Code != 0 {
		return caret, false
	}
	d.Delete(r, caret)
	pos := d.Insert(r, "\n", Attr{})
	d.Lines[line+1] = Line{}
	return pos, true
}

// afWideMarker reports whether r is one of XmNU's full-width markers:
// ＊ ＿ ｀ and ＞.
func afWideMarker(r rune) bool { return r == '＊' || r == '＿' || r == '｀' || r == '＞' }

// afWideAt is XmNU's hJ for the full-width markers: ＊ bold (H_), ＿
// italic (QX, generateMarkdownRegex("＿")) and ｀ code (j5), as
// mdconvert ports them.
func afWideAt(r rune) (func(s []rune, i int) (mdMatch, bool), Style, bool) {
	switch r {
	case '＊':
		return mdCJKBoldAt, Bold, true
	case '＿':
		return mdMarkAt('＿'), Italic, true
	case '｀':
		return mdCJKCodeAt, Code, true
	}
	return nil, 0, false
}

// afRegexText is getTextForRegex (N7Ca) for the line [ls, le): an atom
// and inline code are "!", and so is link text unless the line holds
// markdown link syntax (zI).
func afRegexText(d *Doc, ls, le int) []rune {
	plain := make([]rune, 0, le-ls)
	for i := ls; i < le; i++ {
		if d.Attrs[i].Atom != nil {
			plain = append(plain, ' ')
		} else {
			plain = append(plain, d.Runes[i])
		}
	}
	links := !mdHasLinkSyntax(plain)
	out := make([]rune, le-ls)
	for i := ls; i < le; i++ {
		a := d.Attrs[i]
		switch {
		case a.Atom != nil, a.Style&Code != 0, links && a.Link != "":
			out[i-ls] = '!'
		default:
			out[i-ls] = d.Runes[i]
		}
	}
	return out
}

// autoformatWide is buildLocalMarkdownChangeDelta (boot-shared KT9Q) for
// the full-width markers ＊ ＿ ｀: on the typed marker's line (not a code
// block line), findPotentialMatchBounds takes the nearest same marker
// before it, else after it, and the text from one character before the
// first to two after the last (clamped to the line); the marker's hJ
// pattern runs over that, and its last match makes the delta
// retain(line + matchStart + len(group 1) − 1).delete(1)
// .retain(len(group 2), {format}).delete(1), so the offsets count from
// matchStart, as Slack's do. It returns the new caret and the style.
func autoformatWide(d *Doc, caret int) (newCaret int, st Style, ok bool) {
	n := caret - 1
	if n < 0 || n >= d.Len() || d.Attrs[n].Atom != nil {
		return caret, 0, false
	}
	at, st, ok := afWideAt(d.Runes[n])
	if !ok {
		return caret, 0, false
	}
	line := d.LineOf(n)
	if d.Lines[line].Kind == CodeBlock {
		return caret, 0, false
	}
	ls, le := d.LineStart(line), d.LineEnd(line)
	text := afRegexText(d, ls, le)
	pos, mark := n-ls, d.Runes[n]
	other := -1
	for i := pos - 1; i >= 0; i-- {
		if text[i] == mark {
			other = i
			break
		}
	}
	if other < 0 {
		for i := pos + 1; i < len(text); i++ {
			if text[i] == mark {
				other = i
				break
			}
		}
	}
	if other < 0 {
		return caret, 0, false
	}
	lo, hi := min(pos, other), max(pos, other)
	ms := lo
	if lo > 0 {
		ms = lo - 1
	}
	me := min(hi+2, len(text))
	ml := mdAll(text[ms:me], at)
	if len(ml) == 0 {
		return caret, 0, false
	}
	m := ml[len(ml)-1]
	open := ls + ms + (m.in - m.start) - 1
	inner := m.close - m.in
	closing := open + 1 + inner
	if open < ls || closing >= le {
		return caret, 0, false
	}
	d.Delete(closing, closing+1)
	d.Delete(open, open+1)
	d.SetStyle(open, open+inner, st, true)
	c := caret
	if c > closing {
		c--
	}
	if c > open {
		c--
	}
	return c, st, true
}
