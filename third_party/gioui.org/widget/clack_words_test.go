// SPDX-License-Identifier: Unlicense OR MIT

package widget

import (
	"strings"
	"testing"

	"gioui.org/f32"
)

// clackWordCases are what a double click selects, by Chromium's word
// breaks: Blink's word granularity is ICU's word break iterator
// (position_units_word.cc, text_break_iterator_icu.cc), and Chromium's
// ICU (third_party/icu, patches/wordbrk.patch) moves the full stops from
// MidNumLet to MidNum "so that FQDN labels can be split at '.'", while
// ICU's own rules take the colons out of MidLetter (Blink's
// WordBreakSwedish: "k:a" is two words in English). So a period joins
// only digits, a colon nothing; apostrophes, the middle dot and the other
// MidLetter and MidNumLet marks still join letters. at is the clicked
// rune's index. (internal/ui/richview's TestWordRangeLikeChromium has the
// same cases.)
var clackWordCases = []struct {
	s    string
	at   int
	want string
}{
	{"fmt.Println(x)", 6, "Println"},
	{"fmt.Println(x)", 1, "fmt"},
	{"fmt.Println(x)", 3, "."},
	{"TODO:fix it", 1, "TODO"},
	{"TODO:fix it", 6, "fix"},
	{"TODO:fix it", 4, ":"},
	{"work:yay: done", 6, "yay"},
	{"k:a", 0, "k"},
	{"www.google.com", 6, "google"},
	{"foo.bar@example.com", 1, "foo"},
	{"e.g. this", 0, "e"},
	{"U.S.A.", 2, "S"},
	{"a.1", 0, "a"},
	{"foo::bar", 3, ":"},
	{"pi is 3.14 ok", 7, "3.14"},
	{"1,000.50 USD", 2, "1,000.50"},
	{"v1.2.3 ships", 0, "v1.2.3"},
	{"can't stop", 1, "can't"},
	{"dogs' toys", 1, "dogs"},
	{"l’amour fou", 3, "l’amour"},
	{"col·legi nou", 1, "col·legi"},
	{"abc123 x", 1, "abc123"},
	{"snake_case_name", 3, "snake_case_name"},
	{"soft­hyphen", 1, "soft­hyphen"},
	{"hello  world", 5, "  "},
	{"a\tb", 1, "\t"},
	{"x  y", 1, " "},
}

func TestClackWordRangeLikeChromium(t *testing.T) {
	setSelectTrailingSpace(t, false) // Linux's and macOS's word
	for _, c := range clackWordCases {
		rs := []rune(c.s)
		lo, hi := clackWordRange(rs, c.at)
		if got := string(rs[lo:hi]); got != c.want {
			t.Errorf("a double click on %q at %d selected %q, want %q", c.s, c.at, got, c.want)
		}
	}
}

// A double click past the end of a line that wraps selects the clicked
// line's last word, as Blink's ChooseWordSide does at the end of a line
// that isn't its paragraph's (kPreviousWordIfOnBoundary): not the next
// line's first word, whose rune offset is the same. On the line's last
// word too, where the caret goes before the space the line wraps after.
func TestClackEditorDoubleClickWrappedLineEnd(t *testing.T) {
	for _, trailing := range []bool{false, true} {
		t.Run(map[bool]string{false: "Linux and macOS", true: "Windows' trailing space"}[trailing], func(t *testing.T) {
			setSelectTrailingSpace(t, trailing)
			const s = "the first paragraph wraps over a few lines here"
			c := newClickEd(t, s, false, 120)
			rs := []rune(s)
			next := 0 // the second line's first rune
			y0 := c.e.text.closestToRune(0).y
			for i := 1; i < len(rs); i++ {
				if c.e.text.closestToRune(i).y != y0 {
					next = i
					break
				}
			}
			if next == 0 || rs[next-1] != ' ' {
				t.Fatalf("the text doesn't wrap after a space at 120 px (next line at %d)", next)
			}
			fields := strings.Fields(string(rs[:next]))
			want := fields[len(fields)-1]
			if trailing {
				want += " "
			}
			line := c.e.text.closestToRune(next - 1)
			y := float32(line.y - line.ascent.Ceil()/2)
			// past the line's end, near the editor's right edge
			c.send(edClicks(f32.Pt(118, y), 2)...)
			if got := c.selected(); got != want {
				t.Errorf("a double click past the end of the wrapped first line selected %q, want %q (its last word)", got, want)
			}
			// on the last word's end, before the space
			c.send(edClicks(f32.Pt(float32(line.x.Round())-1, y), 2)...)
			if got := c.selected(); got != want {
				t.Errorf("a double click at the end of the first line's last word selected %q, want %q", got, want)
			}
			// a line's start is the next word's (kNextWordIfOnBoundary)
			first := c.e.text.closestToRune(next)
			c.send(edClicks(f32.Pt(float32(first.x.Round())+1, float32(first.y-first.ascent.Ceil()/2)), 2)...)
			nextWord := strings.Fields(string(rs[next:]))[0]
			if trailing {
				nextWord += " "
			}
			if got := c.selected(); got != nextWord {
				t.Errorf("a double click at the second line's start selected %q, want %q", got, nextWord)
			}
		})
	}
}
