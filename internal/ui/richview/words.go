package richview

import (
	"bytes"
	"runtime"
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
// word there, a run of spaces, or another character with its marks, as
// Blink's word granularity takes them from ICU's word breaks with
// Chromium's rules (Gio's widget.ClackWordRange, patch 48, through
// gio_patch48.go: a period joins only digits, a colon nothing), over the
// paragraph off is in. A press on a line's break takes the line's last
// word; on Windows a word takes the whitespace after it
// (SelectTrailingSpace).
func wordRange(pl []byte, off int) (int, int) {
	n := len(pl)
	if n == 0 {
		return 0, 0
	}
	off = min(max(off, 0), n-1)
	for off > 0 && !utf8.RuneStart(pl[off]) {
		off--
	}
	// the paragraph with its break: word breaks never cross one
	a := bytes.LastIndexByte(pl[:off], '\n') + 1
	b := n
	if i := bytes.IndexByte(pl[off:], '\n'); i >= 0 {
		b = off + i + 1
	}
	var rs []rune
	var at []int // each rune's byte offset in pl, then b
	pos := 0
	for i := a; i < b; {
		r, sz := utf8.DecodeRune(pl[i:])
		if i == off {
			pos = len(rs)
		}
		rs = append(rs, r)
		at = append(at, i)
		i += sz
	}
	at = append(at, b)
	lo, hi := chromiumWordRange(rs, pos, SelectTrailingSpace) // gio_patch48.go
	return at[lo], at[hi]
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
