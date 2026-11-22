package richview

import (
	"strings"
	"testing"
	"time"

	"gioui.org/f32"

	rt "github.com/some-du6e/clack/internal/richtext"
)

// wordCases are what a double click selects, by Chromium's word breaks:
// Blink's word granularity is ICU's word break iterator
// (position_units_word.cc, text_break_iterator_icu.cc), and Chromium's
// ICU (third_party/icu, patches/wordbrk.patch) moves the full stops from
// MidNumLet to MidNum "so that FQDN labels can be split at '.'", while
// ICU's own rules take the colons out of MidLetter (Blink's
// WordBreakSwedish: "k:a" is two words in English). So a period joins
// only digits, a colon nothing; apostrophes, the middle dot and the other
// MidLetter and MidNumLet marks still join letters. at is the clicked
// character's index in s (a rune index; a byte index into s too, for
// ASCII before it).
var wordCases = []struct {
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
	{"k:a", 0, "k"}, // Blink's WordBreakSwedish, in English
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
	{"soft­hyphen", 1, "soft­hyphen"}, // a format character inside a word
	{"hello  world", 5, "  "},
	{"a\tb", 1, "\t"},
	{"x  y", 1, " "}, // no-break spaces aren't WSegSpace: each is its own
}

func TestWordRangeLikeChromium(t *testing.T) {
	setTrailingSpace(t, false) // Linux's and macOS's word
	for _, c := range wordCases {
		off := len(string([]rune(c.s)[:c.at]))
		a, b := wordRange([]byte(c.s), off)
		if got := c.s[a:b]; got != c.want {
			t.Errorf("a double click on %q at %d selected %q, want %q", c.s, c.at, got, c.want)
		}
	}
}

// Blink's click follows a link on every click, the second of a double
// click too (the window counts its press as 2): HTMLAnchorElement's
// default handler doesn't look at the click's detail. A View outside the
// message list (Block Kit surfaces, canvases, the topic, Activity) took
// only a single click's release.
func TestViewLinkOpensOnEveryClick(t *testing.T) {
	const url = "https://example.test/every"
	h := &recHandler{}
	hs := newHarness(doc(para(txt("go to "), rt.Span{Kind: rt.Link, Text: "here", URL: url})), h)
	p := center(findFrag(t, &hs.v.d, "here"))
	hs.send(vclicks(p, 2, time.Second, 150*time.Millisecond)...)
	if len(h.urls) != 2 {
		t.Errorf("a double click on the link opened %q, want it twice (each click)", h.urls)
	}
	// a press the window counts as 2: a click on the text just beside the
	// link, then one on the link
	h.urls = nil
	g := findFrag(t, &hs.v.d, "here")
	q := f32.Pt(g.x-1, float32(g.top+g.bot)/2)
	at := 3 * time.Second
	hs.send(press(q, at), release(q, at+30*time.Millisecond),
		press(f32.Pt(g.x+1, q.Y), at+120*time.Millisecond), release(f32.Pt(g.x+1, q.Y), at+150*time.Millisecond))
	if len(h.urls) != 1 {
		t.Errorf("a click beside the link, then on it: opened %q, want it once", h.urls)
	}
}

// A double click on an emoji selects the emoji, whichever half it lands
// on: Blink's word selection around an image takes the image. Its right
// half's caret offset is after it, which used to select the character
// there instead: at the end of a paragraph, the emoji's closing ':'.
func TestViewDoubleClickEmojiRightHalf(t *testing.T) {
	setTrailingSpace(t, false)
	em := rt.Span{Kind: rt.Emoji, ID: "wave", URL: "https://emoji.test/wave.png"}
	hs := newHarness(doc(para(txt("hi "), em), para(txt("next line"))), &recHandler{})
	var f *frag
	for i := range hs.v.d.frags {
		if hs.v.d.frags[i].kind == fImage {
			f = &hs.v.d.frags[i]
		}
	}
	if f == nil {
		t.Fatalf("no emoji image frag in %q", fragStrings(&hs.v.d))
	}
	want := string(hs.v.d.plain[f.p0:f.p1])
	if !strings.HasPrefix(want, ":") {
		t.Fatalf("the emoji's text is %q", want)
	}
	for _, x := range []float32{.25, .75} {
		p := f32.Pt(f.x+float32(f.size)*x, float32(f.y)+float32(f.size)/2)
		hs.send(vclicks(p, 2, time.Second+time.Duration(x*8)*time.Second, 100*time.Millisecond)...)
		if got := hs.v.SelectedText(); got != want {
			t.Errorf("a double click on the emoji's %v selected %q, want %q", x, got, want)
		}
	}
}

// A double click past the end of a line that wraps selects that line's
// last word, as Blink's ChooseWordSide does after the last word of a
// soft-wrapped line (kPreviousWordIfOnBoundary): not the space it wraps
// at, nor the next line's first word. A line its paragraph ends keeps
// selecting its last word too.
func TestViewDoubleClickWrappedLineEnd(t *testing.T) {
	for _, trailing := range []bool{false, true} {
		setTrailingSpace(t, trailing)
		hs := newHarness(doc(para(txt("short words here extraordinarilylongish next")), para(txt("second paragraph"))), &recHandler{})
		hs.w = 200
		hs.frame()
		if len(hs.v.d.lines) < 3 {
			t.Fatalf("the first paragraph doesn't wrap at 200 px: %d lines (%q)", len(hs.v.d.lines), fragStrings(&hs.v.d))
		}
		ln := hs.v.d.lines[0]
		last := &hs.v.d.frags[ln.f1-1]
		end := last.x + last.w
		if end+20 > float32(hs.w) {
			t.Fatalf("the first line ends at %.0f px of %d: no room past it", end, hs.w)
		}
		pl := string(hs.v.d.plain)
		fields := strings.Fields(pl[:last.p1])
		want := fields[len(fields)-1]
		if trailing {
			want += " "
		}
		y := float32(ln.top+ln.bot) / 2
		at := time.Second
		if trailing {
			at = 5 * time.Second
		}
		hs.send(vclicks(f32.Pt(end+15, y), 2, at, 100*time.Millisecond)...)
		if got := hs.v.SelectedText(); got != want {
			t.Errorf("trailing space %v: a double click past the end of the wrapped first line selected %q, want %q (its last word)", trailing, got, want)
		}
	}
}
