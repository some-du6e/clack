package composer

import "runtime"

// What the composer's double and triple clicks select. The composer is a
// contenteditable in Slack, so Blink's SelectionController does it there
// (third_party/blink/renderer/core/editing/selection_controller.cc):
// HandleDoubleClick's word (edWordAt's word, whitespace run, punctuation
// or atom), on Windows with the whitespace after a word
// (settings.json5 selectTrailingWhitespaceEnabled), and HandleTripleClick's
// paragraph with its break (selection_adjuster.cc kParagraph: "Include the
// paragraph break"). A drag after them extends by the same granule
// (onDrag's edDragWord and edDragLine).

// edSelectTrailingSpace is Blink's selectTrailingWhitespaceEnabled: on on
// Windows only.
var edSelectTrailingSpace = runtime.GOOS == "windows"

// edClickWord is a double click's selection at pos.
func edClickWord(d *Doc, pos int) (lo, hi int) {
	lo, hi = edWordAt(d, pos)
	if edSelectTrailingSpace && hi > lo && edClass(d, hi-1) == edClsWord {
		for hi < d.Len() && edClass(d, hi) == edClsSpace {
			hi++
		}
	}
	return lo, hi
}

// edClickParagraph is a triple click's selection at pos: its line (the
// Doc's paragraph) and the line break after it.
func edClickParagraph(d *Doc, pos int) (lo, hi int) {
	l := d.LineOf(pos)
	lo, hi = d.LineStart(l), d.LineEnd(l)
	if hi < d.Len() {
		hi++
	}
	return lo, hi
}
