package composer

import (
	"unicode"
)

// Live markdown, the way Slack's composer does it: typing the closing
// marker of *bold*, _italic_, ~strike~ or `code` styles the inner text and
// drops the markers; typing "> ", "- ", "* ", "• ", "1. " or "```" at the
// start of a paragraph turns the line into a quote, list or code block.
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
// typed, if that rune closes a marker pair. It returns the new caret (the
// end of the styled text) and the style it applied.
//
// Rules: the opening marker is the nearest same marker on the line before
// the closing one; it must start the line or follow whitespace or
// punctuation; the inner text is non-empty and neither starts nor ends with
// whitespace. Nothing happens inside code blocks or inside inline code.
func autoformatInline(d *Doc, caret int) (newCaret int, st Style, ok bool) {
	k := caret - 1
	if k < 1 || k >= d.Len() || d.Attrs[k].Atom != nil {
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
	ls := d.LineStart(line)
	if k-1 < ls || edSpace(d.Runes[k-1]) || d.Runes[k-1] == marker {
		return caret, 0, false
	}
	j := -1
	for i := k - 2; i >= ls; i-- {
		if d.Runes[i] == marker && d.Attrs[i].Atom == nil {
			j = i
			break
		}
	}
	if j < 0 || d.Attrs[j].Style&Code != 0 || edSpace(d.Runes[j+1]) {
		return caret, 0, false
	}
	if j > ls {
		prev := d.Runes[j-1]
		if d.Attrs[j-1].Atom == nil && !edSpace(prev) && edWordRune(prev) {
			return caret, 0, false
		}
	}
	d.Delete(k, k+1)
	d.Delete(j, j+1)
	d.SetStyle(j, k-1, st, true)
	return k - 1, st, true
}

// afBlockPrefix reports the block kind a typed line prefix stands for.
func afBlockPrefix(rs []rune) (BlockKind, bool) {
	switch string(rs) {
	case "> ":
		return Quote, true
	case "- ", "* ", "• ":
		return Bullet, true
	case "```":
		return CodeBlock, true
	}
	// "<digits>. "
	n := len(rs)
	if n >= 3 && rs[n-1] == ' ' && rs[n-2] == '.' {
		for _, r := range rs[:n-2] {
			if r < '0' || r > '9' {
				return Para, false
			}
		}
		return Ordered, true
	}
	return Para, false
}

// autoformatBlock converts the caret's paragraph to a quote, list or code
// block when the text between the line start and the caret is one of the
// markdown block prefixes. The prefix is removed; it returns the new caret.
func autoformatBlock(d *Doc, caret int) (newCaret int, k BlockKind, ok bool) {
	caret = d.clamp(caret)
	line := d.LineOf(caret)
	if d.Lines[line].Kind != Para {
		return caret, Para, false
	}
	ls := d.LineStart(line)
	if caret-ls < 2 || caret-ls > 8 {
		return caret, Para, false
	}
	for i := ls; i < caret; i++ {
		if d.Attrs[i].Atom != nil || d.Attrs[i].Style&Code != 0 {
			return caret, Para, false
		}
	}
	k, ok = afBlockPrefix(d.Runes[ls:caret])
	if !ok {
		return caret, Para, false
	}
	d.Delete(ls, caret)
	d.Lines[line] = Line{Kind: k}
	return ls, k, true
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
	for i := caret - 3; i < caret; i++ {
		if d.Runes[i] != '`' || d.Attrs[i].Atom != nil || d.Attrs[i].Style&Code != 0 {
			return caret, false
		}
	}
	// A fourth backtick before is someone typing ```` (or ``` inside a
	// longer run): leave it.
	if d.Runes[caret-4] == '`' {
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
