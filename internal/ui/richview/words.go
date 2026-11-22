package richview

import (
	"runtime"
	"unicode"
	"unicode/utf8"
)

// What a double or triple click selects, as Blink's SelectionController
// does (third_party/blink/renderer/core/editing/selection_controller.cc):
// HandleDoubleClick's word, HandleTripleClick's paragraph.

// SelectTrailingSpace is Blink's selectTrailingWhitespaceEnabled
// (third_party/blink/renderer/core/frame/settings.json5: on on Windows
// only): a double click's word takes the whitespace after it.
var SelectTrailingSpace = runtime.GOOS == "windows"

// wordRange is what a double click at byte offset off of pl selects: the
// word there, a run of whitespace, or another character (a grapheme), as
// Blink's word granularity takes them from ICU's word breaks (UAX #29,
// roughly: letters, digits, marks and connectors make words, joined over
// one apostrophe, period or colon between letters, and one period,
// apostrophe, comma or semicolon between digits; each punctuation or
// symbol is a word of its own). On Windows a word takes the whitespace
// after it (SelectTrailingSpace).
func wordRange(pl []byte, off int) (int, int) {
	n := len(pl)
	if n == 0 {
		return 0, 0
	}
	off = min(max(off, 0), n-1)
	for off > 0 && !utf8.RuneStart(pl[off]) {
		off--
	}
	r, sz := utf8.DecodeRune(pl[off:])
	if r == '\n' && off > 0 {
		if pr, psz := utf8.DecodeLastRune(pl[:off]); pr != '\n' {
			off, r, sz = off-psz, pr, psz // the end of a line: its last word
		}
	}
	a, b := off, off+sz
	switch {
	case wordRune(r):
		for a > 0 {
			pr, psz := utf8.DecodeLastRune(pl[:a])
			if wordRune(pr) {
				a -= psz
				continue
			}
			if a-psz > 0 {
				ppr, ppsz := utf8.DecodeLastRune(pl[:a-psz])
				nr, _ := utf8.DecodeRune(pl[a:])
				if midJoins(ppr, pr, nr) {
					a -= psz + ppsz
					continue
				}
			}
			break
		}
		for b < n {
			nr, nsz := utf8.DecodeRune(pl[b:])
			if wordRune(nr) {
				b += nsz
				continue
			}
			if b+nsz < n {
				nnr, nnsz := utf8.DecodeRune(pl[b+nsz:])
				pr, _ := utf8.DecodeLastRune(pl[:b])
				if midJoins(pr, nr, nnr) {
					b += nsz + nnsz
					continue
				}
			}
			break
		}
		if SelectTrailingSpace {
			for b < n {
				nr, nsz := utf8.DecodeRune(pl[b:])
				if !hSpace(nr) {
					break
				}
				b += nsz
			}
		}
	case hSpace(r):
		for a > 0 {
			pr, psz := utf8.DecodeLastRune(pl[:a])
			if !hSpace(pr) {
				break
			}
			a -= psz
		}
		for b < n {
			nr, nsz := utf8.DecodeRune(pl[b:])
			if !hSpace(nr) {
				break
			}
			b += nsz
		}
	default:
		for a > 0 {
			pr, psz := utf8.DecodeLastRune(pl[:a])
			cur, _ := utf8.DecodeRune(pl[a:])
			if !graphemeExtends(pr, cur) {
				break
			}
			a -= psz
		}
		for b < n {
			pr, _ := utf8.DecodeLastRune(pl[:b])
			nr, nsz := utf8.DecodeRune(pl[b:])
			if !graphemeExtends(pr, nr) {
				break
			}
			b += nsz
		}
	}
	return a, b
}

// paragraphRange is what a triple click at byte offset off of pl selects:
// the paragraph, from the line break before it to the one after it, that
// break included (Blink's kParagraph: "Include the paragraph break").
func paragraphRange(pl []byte, off int) (int, int) {
	n := len(pl)
	off = min(max(off, 0), n)
	a, b := off, off
	for a > 0 && pl[a-1] != '\n' {
		a--
	}
	for b < n && pl[b] != '\n' {
		b++
	}
	if b < n {
		b++
	}
	return a, b
}

func wordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Mc, r) ||
		unicode.Is(unicode.Me, r) || unicode.Is(unicode.Pc, r)
}

func hSpace(r rune) bool { return r != '\n' && unicode.IsSpace(r) }

// midJoins reports whether m between a and b keeps them one word (UAX #29
// WB6/7, WB11/12).
func midJoins(a, m, b rune) bool {
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

// graphemeExtends reports whether r continues the grapheme of prev:
// marks, variation selectors, ZWJ sequences, skin tones, tags and flag
// pairs.
func graphemeExtends(prev, r rune) bool {
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

// A selection's granule: what a press selected by and a drag after it
// extends by.
const (
	granChar uint8 = iota
	granWord
	granParagraph
)

// extendBy is a drag after a double or triple press reaching off: the
// granule there and the one the press selected, together, the drag's end
// at the far side (Blink's ExtendSelectionAsDirectional).
func (v *View) extendBy(off int32) {
	sa, sb := v.selA, v.selB
	if v.gran == granParagraph {
		v.selectLine(off)
	} else {
		v.selectWord(off)
	}
	lo, hi := v.selA, v.selB
	v.selA, v.selB = sa, sb
	if lo < v.granA {
		v.selA, v.selB = v.granB, lo
		return
	}
	v.selA, v.selB = v.granA, max(hi, v.granB)
}
