package richtext

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// TestRawPreHighlightMidLine: a preformatted element of RawLines lines or
// more (one BrRawPre span) whose keyword, find or search match sits in the
// middle of a line is cut there, and each part after the first stays
// preformatted: laid out a line a piece, whitespace-only lines and all,
// as the element's spans are. (The parts after a cut became BrRaw, rich
// text: a newline, a line of spaces and a newline turned into a paragraph
// run, the spaces moving to the next line's start.)
func TestRawPreHighlightMidLine(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 2*RawLines; i++ {
		fmt.Fprintf(&b, "func w%d() {\n    call(a, body.x%d)\n    \n\t\n}\n", i, i)
	}
	text := b.String()
	m := giantMessage(t, text, "", true)
	if d := FromMessage(R, m); len(d.Blocks) != 1 || len(d.Blocks[0].Spans) != 1 || d.Blocks[0].Spans[0].Br != BrRawPre {
		t.Fatalf("want one BrRawPre span (blocks %d)", len(d.Blocks))
	}
	// whole words in the middle of a line ("a, |body|.x7", "func |w300|("):
	// cuts with no newline in the white space around them, which the cut
	// path takes on any code (keywords match whole words only); then a
	// word after a line's indent
	for _, words := range [][]string{{"body"}, {"body", "w300"}, {"x7"}, {"call"}} {
		hd := FromMessage(R, m)
		HighlightDoc(hd.Blocks, words)
		if len(hd.Blocks) != 1 || len(hd.Blocks[0].Spans) < 2 {
			t.Fatalf("%q: the raw span wasn't cut at its matches: the test misses the cut path", words)
		}
		for i, sp := range hd.Blocks[0].Spans {
			if IsRaw(&sp) && sp.Br != BrRawPre {
				t.Errorf("%q: part %d of the preformatted element is laid out as rich text (Br %d)", words, i, sp.Br)
				break
			}
		}
		hw := expandedDoc(m)
		HighlightDoc(hw.Blocks, words)
		if got := expandDoc(hd); !reflect.DeepEqual(got, hw) {
			n, w := 0, 0
			if len(got.Blocks) > 0 {
				n = len(got.Blocks[0].Spans)
			}
			if len(hw.Blocks) > 0 {
				w = len(hw.Blocks[0].Spans)
			}
			t.Errorf("%q: the highlighted raw span stands for %d spans, the element's highlighted spans are %d", words, n, w)
		}
		if got, w := PlainText(hd), PlainText(hw); got != w {
			t.Errorf("%q: plain text %d bytes, want %d", words, len(got), len(w))
		}
		d := FromMessage(R, m)
		if got, w := expandDoc(FindHighlighted(d, words)), FindHighlighted(expandedDoc(m), words); !reflect.DeepEqual(got, w) {
			t.Errorf("find %q marks other runs than the element's spans get", words)
		}
	}
}
