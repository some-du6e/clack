// SPDX-License-Identifier: Unlicense OR MIT

package widget

import (
	"runtime"
	"unicode"
)

// Clack patch 48 (CLACK_PATCHES.md §48): what an Editor's double and
// triple clicks select, as Blink's SelectionController does in an
// <input> or a <textarea>.

// clackSelectTrailingSpace is Blink's selectTrailingWhitespaceEnabled
// (third_party/blink/renderer/core/frame/settings.json5: on on Windows
// only): a double click's word takes the whitespace after it.
var clackSelectTrailingSpace = runtime.GOOS == "windows"

// clackWordRange is what a double click at rune pos of rs selects: the
// word there, a run of whitespace, or another character (a grapheme), as
// Blink's word granularity takes them from ICU's word breaks (UAX #29,
// roughly: letters, digits, marks and connectors make words, joined over
// one apostrophe, period or colon between letters, and one period,
// apostrophe, comma or semicolon between digits; each punctuation or
// symbol is a word of its own).
func clackWordRange(rs []rune, pos int) (lo, hi int) {
	n := len(rs)
	if n == 0 {
		return 0, 0
	}
	pos = min(max(pos, 0), n-1)
	if rs[pos] == '\n' && pos > 0 && rs[pos-1] != '\n' {
		pos-- // the end of a line: its last word
	}
	switch r := rs[pos]; {
	case clackWordRune(r):
		lo, hi = pos, pos+1
		for {
			switch {
			case lo > 0 && clackWordRune(rs[lo-1]):
				lo--
				continue
			case lo > 1 && clackJoins(rs[lo-2], rs[lo-1], rs[lo]):
				lo -= 2
				continue
			}
			break
		}
		for {
			switch {
			case hi < n && clackWordRune(rs[hi]):
				hi++
				continue
			case hi+1 < n && clackJoins(rs[hi-1], rs[hi], rs[hi+1]):
				hi += 2
				continue
			}
			break
		}
		if clackSelectTrailingSpace {
			for hi < n && clackSpace(rs[hi]) {
				hi++
			}
		}
		return lo, hi
	case clackSpace(r):
		lo, hi = pos, pos+1
		for lo > 0 && clackSpace(rs[lo-1]) {
			lo--
		}
		for hi < n && clackSpace(rs[hi]) {
			hi++
		}
		return lo, hi
	default:
		lo, hi = pos, pos+1
		for lo > 0 && clackExtends(rs[lo-1], rs[lo]) {
			lo--
		}
		for hi < n && clackExtends(rs[hi-1], rs[hi]) {
			hi++
		}
		return lo, hi
	}
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

func clackWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Mc, r) ||
		unicode.Is(unicode.Me, r) || unicode.Is(unicode.Pc, r)
}

func clackSpace(r rune) bool { return r != '\n' && unicode.IsSpace(r) }

// clackJoins reports whether m between a and b keeps them one word
// (UAX #29 WB6/7, WB11/12).
func clackJoins(a, m, b rune) bool {
	letters := unicode.IsLetter(a) && unicode.IsLetter(b)
	digits := unicode.IsDigit(a) && unicode.IsDigit(b)
	switch m {
	case '\'', '.', 0x2018, 0x2019, 0x2024, 0xfe52, 0xff07, 0xff0e: // MidNumLet, Single_Quote
		return letters || digits
	case ':', 0xb7, 0x387, 0x5f4, 0x2027, 0xfe13, 0xfe55, 0xff1a: // MidLetter
		return letters
	case ',', ';', 0x37e, 0x589, 0x60c, 0x60d, 0x66c, 0x7f8, 0x2044, 0xfe10, 0xfe14, 0xfe50, 0xfe54, 0xff0c, 0xff1b: // MidNum
		return digits
	}
	return false
}

// clackExtends reports whether r continues the grapheme of prev: marks,
// variation selectors, ZWJ sequences, skin tones, tags and flag pairs.
func clackExtends(prev, r rune) bool {
	switch {
	case r == 0x200d || prev == 0x200d:
		return true
	case r >= 0xfe00 && r <= 0xfe0f, r >= 0x1f3fb && r <= 0x1f3ff, r >= 0xe0020 && r <= 0xe007f:
		return true
	case unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || unicode.Is(unicode.Mc, r):
		return true
	case r >= 0x1f1e6 && r <= 0x1f1ff && prev >= 0x1f1e6 && prev <= 0x1f1ff:
		return true
	}
	return false
}

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

// clackExtend is a drag after a double or triple press: the granule under
// the pointer (the caret MoveCoord just set) and the one the press
// selected, together, the caret at the far end from that first granule
// (Blink's ExtendSelectionAsDirectional).
func (e *Editor) clackExtend() {
	pos, _ := e.text.Selection()
	rs := []rune(e.Text())
	lo, hi := clackGranRange(rs, pos, e.clackGran)
	if lo < e.clackLo {
		e.text.SetCaret(lo, e.clackHi)
		return
	}
	e.text.SetCaret(max(hi, e.clackHi), e.clackLo)
}
