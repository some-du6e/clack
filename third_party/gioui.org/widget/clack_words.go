// SPDX-License-Identifier: Unlicense OR MIT

package widget

import (
	"image"
	"runtime"
	"unicode"

	"golang.org/x/image/math/fixed"
)

// Clack patch 48 (CLACK_PATCHES.md §48): what an Editor's double and
// triple clicks select, as Blink's SelectionController does in an
// <input> or a <textarea>.

// clackSelectTrailingSpace is Blink's selectTrailingWhitespaceEnabled
// (third_party/blink/renderer/core/frame/settings.json5: on on Windows
// only): a double click's word takes the whitespace after it.
var clackSelectTrailingSpace = runtime.GOOS == "windows"

// clackWordRange is what a double click at rune pos of rs selects: the
// word there, a run of spaces, or a punctuation mark or symbol (with the
// marks on it), as Blink's word granularity takes them from ICU's word
// breaks (ClackWordRange); on Windows a word takes the whitespace after
// it (clackSelectTrailingSpace).
func clackWordRange(rs []rune, pos int) (lo, hi int) {
	return ClackWordRange(rs, pos, clackSelectTrailingSpace)
}

// ClackWordRange is what a double click at rune pos of rs selects, as
// Blink's word granularity (position_units_word.cc) takes it from ICU's
// word break iterator with Chromium's rules (clackWordSegment): the
// segment there, after the line's last word for a press on its line
// break; with trailingSpace (Blink's selectTrailingWhitespaceEnabled,
// Windows) a word takes the whitespace after it. Clack patch 48: Clack's
// rich text (internal/ui/richview) selects by it too.
func ClackWordRange(rs []rune, pos int, trailingSpace bool) (lo, hi int) {
	n := len(rs)
	if n == 0 {
		return 0, 0
	}
	pos = min(max(pos, 0), n-1)
	if rs[pos] == '\n' && pos > 0 && rs[pos-1] != '\n' {
		pos-- // the end of a line: its last word
	}
	lo, hi = clackWordSegment(rs, pos)
	if trailingSpace && clackIsWord(rs[lo:hi]) {
		for hi < n && clackSpace(rs[hi]) {
			hi++
		}
	}
	return lo, hi
}

// clackWordSegment is the word break segment of rs that holds rune pos,
// by Chromium's ICU word rules (third_party/icu,
// source/data/brkitr/rules/word.txt: UAX #29 with ICU's and Chromium's
// changes). Letters, digits, marks and connectors make words; one
// apostrophe, curly quote, middle dot or other MidLetter or MidNumLet mark
// between letters keeps them one word, and one period, comma, semicolon,
// apostrophe or other MidNum mark between digits; a period between
// letters doesn't (Chromium's patches/wordbrk.patch moves the full stops
// from MidNumLet to MidNum "so that FQDN labels can be split at '.'"), nor
// does a colon (ICU takes it out of MidLetter). Kana and ideographs make
// runs of their own (ICU's dictionaries split those further; Clack has
// none), spaces (WSegSpace) runs of their own, and every other character a
// segment of its own, with the marks and format characters on it.
func clackWordSegment(rs []rune, pos int) (lo, hi int) {
	n := len(rs)
	if n == 0 {
		return 0, 0
	}
	pos = min(max(pos, 0), n-1)
	cls := make([]clackWB, n)
	for i, r := range rs {
		cls[i] = clackWBOf(r)
	}
	lo = pos
	for lo > 0 && !clackWordBreak(cls, lo) {
		lo--
	}
	hi = pos + 1
	for hi < n && !clackWordBreak(cls, hi) {
		hi++
	}
	return lo, hi
}

// clackWB is a rune's Word_Break class (UAX #29), as Chromium's ICU word
// rules group them.
type clackWB uint8

const (
	wbOther clackWB = iota
	wbCR
	wbLF
	wbNewline
	wbExtend
	wbZWJ
	wbFormat
	wbRegionalIndicator
	wbKatakana
	wbHiragana
	wbIdeographic
	wbHangul // Hangul syllables (ICU's $HangulSyllable, out of ALetter)
	wbHebrew
	wbALetter // ICU's $ALetterPlus: with the complex-context scripts
	wbNumeric
	wbExtendNumLet
	wbSingleQuote
	wbDoubleQuote
	wbMidLetter
	wbMidNumLet
	wbMidNum
	wbWSegSpace
	wbExtPict
)

func clackWBOf(r rune) clackWB {
	switch r {
	case '\r':
		return wbCR
	case '\n':
		return wbLF
	case 0x0b, 0x0c, 0x85, 0x2028, 0x2029:
		return wbNewline
	case 0x200d:
		return wbZWJ
	case 0x200c, 0xff9e, 0xff9f: // ZWNJ, the halfwidth sound marks (Grapheme_Extend)
		return wbExtend
	case 0x200b: // ZWSP: not Format
		return wbOther
	case '\'':
		return wbSingleQuote
	case '"':
		return wbDoubleQuote
	// Word_Break=MidLetter less the colons (U+003A, U+FE55, U+FF1A), as
	// ICU's rules take them out
	case 0xb7, 0x387, 0x55f, 0x5f4, 0x2027, 0xfe13:
		return wbMidLetter
	// MidNumLet less the full stops (U+002E, U+FF0E): Chromium's
	// wordbrk.patch
	case 0x2018, 0x2019, 0x2024, 0xfe52, 0xff07:
		return wbMidNumLet
	// MidNum with the full stops
	case '.', 0xff0e, ',', ';', 0x37e, 0x589, 0x60c, 0x60d, 0x66c, 0x7f8, 0x2044, 0xfe10, 0xfe14, 0xfe50, 0xfe54, 0xff0c, 0xff1b:
		return wbMidNum
	case 0x202f: // NARROW NO-BREAK SPACE
		return wbExtendNumLet
	case ' ', 0x1680, 0x205f, 0x3000:
		return wbWSegSpace
	case 0x66b: // ARABIC DECIMAL SEPARATOR
		return wbNumeric
	case 0x3031, 0x3032, 0x3033, 0x3034, 0x3035, 0x309b, 0x309c, 0x30a0, 0x30fc, 0xff70:
		return wbKatakana
	}
	switch {
	case r >= 0x2000 && r <= 0x200a && r != 0x2007:
		return wbWSegSpace
	case r >= 0x1f1e6 && r <= 0x1f1ff:
		return wbRegionalIndicator
	case r >= 0x1f3fb && r <= 0x1f3ff, r >= 0xe0020 && r <= 0xe007f: // emoji modifiers, tags
		return wbExtend
	case unicode.In(r, unicode.Mn, unicode.Me, unicode.Mc):
		return wbExtend
	case unicode.Is(unicode.Cf, r) && !clackPrepend(r):
		return wbFormat
	case unicode.Is(unicode.Pc, r):
		return wbExtendNumLet
	case unicode.Is(unicode.Katakana, r):
		return wbKatakana
	case unicode.Is(unicode.Hiragana, r):
		return wbHiragana
	case r >= 0xac00 && r <= 0xd7a3:
		return wbHangul
	case unicode.Is(unicode.Ideographic, r), unicode.Is(unicode.Han, r):
		return wbIdeographic
	case unicode.Is(unicode.Nd, r):
		return wbNumeric
	case unicode.Is(unicode.Hebrew, r) && unicode.IsLetter(r):
		return wbHebrew
	case unicode.IsLetter(r), unicode.Is(unicode.Nl, r):
		return wbALetter
	case clackExtPict(r):
		return wbExtPict
	}
	return wbOther
}

// clackPrepend reports the format characters that are
// Grapheme_Cluster_Break=Prepend (UAX #29 leaves them out of Format).
func clackPrepend(r rune) bool {
	return r >= 0x600 && r <= 0x605 || r == 0x6dd || r == 0x70f || r == 0x890 || r == 0x891 || r == 0x8e2 || r == 0x110bd || r == 0x110cd
}

// clackExtPict is roughly Extended_Pictographic: the emoji and pictographs
// a ZWJ joins (WB3c).
func clackExtPict(r rune) bool {
	switch {
	case r >= 0x1f000 && r <= 0x1fffd, r >= 0x2600 && r <= 0x27bf, r >= 0x2190 && r <= 0x2bff:
		return true
	}
	switch r {
	case 0xa9, 0xae, 0x203c, 0x2049, 0x2122, 0x2139, 0x3030, 0x303d, 0x3297, 0x3299:
		return true
	}
	return false
}

func (c clackWB) exFm() bool { return c == wbExtend || c == wbFormat || c == wbZWJ }

func (c clackWB) ahLetter() bool { return c == wbALetter || c == wbHebrew }

func (c clackWB) midNumLetQ() bool { return c == wbMidNumLet || c == wbSingleQuote }

func (c clackWB) kanaKanji() bool { return c == wbKatakana || c == wbHiragana || c == wbIdeographic }

// clackWordBreak reports whether a word break comes before cls[i]
// (0 < i < len(cls)): UAX #29's rules WB3 to WB16 with ICU's own (Hangul
// syllables and kana and ideographs kept together for the dictionaries).
func clackWordBreak(cls []clackWB, i int) bool {
	b, c := cls[i-1], cls[i]
	switch {
	case b == wbCR && c == wbLF: // WB3
		return false
	case b == wbCR || b == wbLF || b == wbNewline, c == wbCR || c == wbLF || c == wbNewline: // WB3a, WB3b
		return true
	case b == wbZWJ && c == wbExtPict: // WB3c
		return false
	case b == wbWSegSpace && c == wbWSegSpace: // WB3d
		return false
	case c.exFm(): // WB4: marks and format characters go with what they follow
		return false
	}
	// WB4: the rules from here see past marks and format characters
	p := i - 1
	for p >= 0 && cls[p].exFm() {
		p--
	}
	if p < 0 {
		return true
	}
	bp := cls[p]
	if bp == wbCR || bp == wbLF || bp == wbNewline {
		return true // marks right after a line break made a segment of their own
	}
	pp := p - 1
	for pp >= 0 && cls[pp].exFm() {
		pp--
	}
	before := wbOther
	if pp >= 0 {
		before = cls[pp]
	}
	nx := i + 1
	for nx < len(cls) && cls[nx].exFm() {
		nx++
	}
	after := wbOther
	if nx < len(cls) {
		after = cls[nx]
	}
	switch {
	case bp.ahLetter() && c.ahLetter(): // WB5
		return false
	case bp.ahLetter() && (c == wbMidLetter || c.midNumLetQ()) && after.ahLetter(): // WB6
		return false
	case before.ahLetter() && (bp == wbMidLetter || bp.midNumLetQ()) && c.ahLetter(): // WB7
		return false
	case bp == wbHebrew && c == wbSingleQuote: // WB7a
		return false
	case bp == wbHebrew && c == wbDoubleQuote && after == wbHebrew: // WB7b
		return false
	case before == wbHebrew && bp == wbDoubleQuote && c == wbHebrew: // WB7c
		return false
	case bp == wbNumeric && c == wbNumeric, bp.ahLetter() && c == wbNumeric, bp == wbNumeric && c.ahLetter(): // WB8, WB9, WB10
		return false
	case before == wbNumeric && (bp == wbMidNum || bp.midNumLetQ()) && c == wbNumeric: // WB11
		return false
	case bp == wbNumeric && (c == wbMidNum || c.midNumLetQ()) && after == wbNumeric: // WB12
		return false
	case bp == wbKatakana && c == wbKatakana: // WB13
		return false
	case (bp.ahLetter() || bp == wbNumeric || bp == wbKatakana || bp == wbExtendNumLet) && c == wbExtendNumLet: // WB13a
		return false
	case bp == wbExtendNumLet && (c.ahLetter() || c == wbNumeric || c == wbKatakana): // WB13b
		return false
	case bp == wbRegionalIndicator && c == wbRegionalIndicator: // WB15, WB16: pairs
		k := 0
		for j := p; j >= 0 && (cls[j] == wbRegionalIndicator || cls[j].exFm()); j-- {
			if cls[j] == wbRegionalIndicator {
				k++
			}
		}
		return k%2 == 0
	case bp == wbHangul && c == wbHangul, bp.kanaKanji() && c.kanaKanji(): // ICU's: for its dictionaries
		return false
	}
	return true // WB999
}

// clackIsWord reports whether a segment is a word (ICU's rule status
// isn't UBRK_WORD_NONE): it holds a letter, digit, kana or ideograph.
func clackIsWord(seg []rune) bool {
	for _, r := range seg {
		switch clackWBOf(r) {
		case wbALetter, wbHebrew, wbNumeric, wbKatakana, wbHiragana, wbIdeographic, wbHangul, wbExtendNumLet:
			return true
		}
	}
	return false
}

// clackWordPos is the rune whose word a double click at p takes, the press
// having put the caret at caret: after the last word of a line that wraps
// (the caret at the line's end, or past it, where Gio puts the caret at
// the next line's start, the same rune offset), that word's last rune, as
// Blink's ChooseWordSide picks kPreviousWordIfOnBoundary at the end of a
// line that isn't its paragraph's end ("If the caret is after the last
// word in a soft-wrapped line ... select that last word",
// selection_adjuster.cc); else caret.
func (e *Editor) clackWordPos(p image.Point, caret int, rs []rune) int {
	t := &e.text
	x := fixed.I(p.X + t.scrollOff.X)
	y := p.Y + t.scrollOff.Y
	at, eol := t.closestToXY(x, y)
	line := at.lineCol.line
	if eol {
		line-- // past the clicked line's end: Gio returns the next line's first position
	}
	if line < 0 || line+1 >= len(t.index.lines) {
		return caret // the last line: its paragraph's end
	}
	first, last := -1, -1
	for _, q := range t.index.positions {
		if q.lineCol.line > line {
			break
		}
		if q.lineCol.line == line {
			if first < 0 {
				first = q.runes
			}
			last = q.runes
		}
	}
	if last <= first || last >= len(rs) || (!eol && caret != last) {
		return caret
	}
	if rs[last] == '\n' {
		return last // a line break ends the line: its paragraph's end, its last word (ClackWordRange)
	}
	// the line's last position is before the spaces it wraps after
	q := last
	for q > first && clackSpace(rs[q-1]) {
		q--
	}
	if q == first {
		return caret
	}
	return q - 1
}

// clackParagraphRange is what a triple click at rune pos of rs selects:
// the paragraph, from the line break before it to the one after it,
// that break included (Blink's kParagraph: "Include the paragraph break").
func clackParagraphRange(rs []rune, pos int) (lo, hi int) {
	n := len(rs)
	pos = min(max(pos, 0), n)
	lo, hi = pos, pos
	for lo > 0 && rs[lo-1] != '\n' {
		lo--
	}
	for hi < n && rs[hi] != '\n' {
		hi++
	}
	if hi < n {
		hi++
	}
	return lo, hi
}

func clackSpace(r rune) bool { return r != '\n' && unicode.IsSpace(r) }

const (
	clackGranChar uint8 = iota
	clackGranWord
	clackGranParagraph
)

func clackGranRange(rs []rune, pos int, gran uint8) (lo, hi int) {
	if gran == clackGranParagraph {
		return clackParagraphRange(rs, pos)
	}
	return clackWordRange(rs, pos)
}

// clackExtend is a drag (or the release) after a double or triple press
// at p: the granule under the pointer (the caret MoveCoord just set; a
// word by the same side rule as the press's, clackWordPos, so a release
// past a wrapped line's end keeps that line's last word) and the one the
// press selected, together, the caret at the far end from that first
// granule (Blink's ExtendSelectionAsDirectional).
func (e *Editor) clackExtend(p image.Point) {
	pos, _ := e.text.Selection()
	rs := []rune(e.Text())
	if e.clackGran == clackGranWord {
		pos = e.clackWordPos(p, pos, rs)
	}
	lo, hi := clackGranRange(rs, pos, e.clackGran)
	if lo < e.clackLo {
		e.text.SetCaret(lo, e.clackHi)
		return
	}
	e.text.SetCaret(max(hi, e.clackHi), e.clackLo)
}
