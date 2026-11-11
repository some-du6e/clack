package popups

// The search box's contents as Slack's omniswitcher keeps them: its
// display query ops, plain text and tokens (slacksearchfilter filters,
// slackmention mentions). The quick filter row, the date pickers and a
// token's close icon edit the box through these (OmniswitcherContent's
// onFilterInsert, rs63 aP, build 4d0c78085232).

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// srPiece is one op of the box: plain text, or a token whose label is
// text.
type srPiece struct {
	text string
	tok  *srToken
}

// srBox is the box's text with its tokens.
type srBox struct {
	text string
	toks []srToken
}

// box is the box now.
func (p *SearchPopover) box() srBox {
	return srBox{text: p.ed.Text(), toks: append([]srToken(nil), p.tokens...)}
}

// pieces splits b at its tokens (tokens that no longer match the text are
// plain text).
func (b srBox) pieces() []srPiece {
	rs := []rune(b.text)
	var out []srPiece
	at := 0
	for i := range b.toks {
		t := b.toks[i]
		if t.start < at || t.end() > len(rs) || string(rs[t.start:t.end()]) != t.label {
			continue
		}
		if t.start > at {
			out = append(out, srPiece{text: string(rs[at:t.start])})
		}
		tok := t
		out = append(out, srPiece{text: t.label, tok: &tok})
		at = t.end()
	}
	if at < len(rs) {
		out = append(out, srPiece{text: string(rs[at:])})
	}
	return out
}

// srJoin is the box the pieces make: their text in order, the tokens at
// their offsets.
func srJoin(ps []srPiece) srBox {
	var b strings.Builder
	var toks []srToken
	n := 0
	for _, pc := range ps {
		if pc.text == "" {
			continue
		}
		if pc.tok != nil {
			t := *pc.tok
			t.start, t.label = n, pc.text
			toks = append(toks, t)
		}
		b.WriteString(pc.text)
		n += utf8.RuneCountInString(pc.text)
	}
	return srBox{text: b.String(), toks: toks}
}

// srTrimEnd is the ops with the trailing whitespace of their text removed
// (getOpsTrimmed with trimEnd); tokens end it as they are.
func srTrimEnd(ps []srPiece) []srPiece {
	out := append([]srPiece(nil), ps...)
	for len(out) > 0 {
		last := &out[len(out)-1]
		if last.tok != nil {
			break
		}
		t := strings.TrimRightFunc(last.text, unicode.IsSpace)
		if t != "" {
			last.text = t
			break
		}
		out = out[:len(out)-1]
	}
	return out
}

// srBlank reports a box with no text but whitespace (isDeltaEmpty with
// trimWhitespace).
func srBlank(ps []srPiece) bool {
	for _, pc := range ps {
		if strings.TrimSpace(pc.text) != "" {
			return false
		}
	}
	return true
}

// srText is a plain text piece, srTok a token's, srSpace one space.
func srText(s string) srPiece      { return srPiece{text: s} }
func srTokPiece(t srToken) srPiece { return srPiece{text: t.label, tok: &t} }
func srSpace() srPiece             { return srPiece{text: " "} }

// srRunes is the pieces' length in runes.
func srRunes(ps []srPiece) int { return utf8.RuneCountInString(srJoin(ps).text) }

// filterInsert is onFilterInsert (aP). With replace the box becomes ins
// (nothing when that is only whitespace). Otherwise ins goes at the end,
// after the box's text with its trailing whitespace trimmed and one
// space, none when the box was empty. incomplete is Texty's
// triggerAutocomplete: the caret goes cursorOffset runes into ins (the
// end of the box when cursorOffset < 0) and the completers run there, as
// if the user had typed it.
func (p *SearchPopover) filterInsert(ins []srPiece, replace, incomplete bool, cursorOffset int) {
	var all []srPiece
	caret := -1
	if replace {
		if !srBlank(ins) {
			all = ins
		}
	} else {
		cur := srTrimEnd(p.box().pieces())
		if len(cur) > 0 {
			cur = append(cur, srSpace())
		}
		if cursorOffset >= 0 {
			caret = srRunes(cur) + cursorOffset
		}
		all = append(cur, ins...)
	}
	p.setBox(srJoin(all), caret)
	if incomplete {
		p.acArmed = true
		p.ac.dismissed = ""
	}
}

// setBox puts b in the box, the caret at caret (runes; the end when < 0),
// and runs the searches for it (an({textyOps}), then al(): the box keeps
// the focus).
func (p *SearchPopover) setBox(b srBox, caret int) {
	p.ed.SetText(b.text)
	n := utf8.RuneCountInString(b.text)
	if caret < 0 || caret > n {
		caret = n
	}
	p.ed.SetCaret(caret, caret)
	p.tokens, p.query = b.toks, b.text
	p.refocus = true
	if p.picked {
		p.unpick()
	}
	if strings.TrimSpace(p.query) != "" {
		p.histMode = false
	}
	p.queryChanged(time.Now())
	p.h.Invalidate()
}

// srTrailing is getTrailingText: the last op's text, "" when it is a
// token (getTextOpInsertText leaves out filters, mentions and slugs).
func srTrailing(ps []srPiece) string {
	if len(ps) == 0 || ps[len(ps)-1].tok != nil {
		return ""
	}
	return strings.TrimRight(ps[len(ps)-1].text, "\n")
}
