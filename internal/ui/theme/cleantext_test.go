package theme

import (
	"slices"
	"testing"

	"gioui.org/font"
	"gioui.org/text"
	"golang.org/x/image/math/fixed"
)

// shapedGlyph is one glyph out of the shaper: its face (an index into
// FacesFor's collection), glyph id and advance in px.
type shapedGlyph struct{ face, gid, adv int }

// shapeLato shapes s on one line in Lato at weight w, px pixels per em.
func shapeLato(sh *text.Shaper, w font.Weight, px int, s string) []shapedGlyph {
	sh.LayoutString(text.Parameters{Font: Font(w), PxPerEm: fixed.Int26_6(px << 6), MaxWidth: 10000, MaxLines: 1}, s)
	var out []shapedGlyph
	for g, ok := sh.NextGlyph(); ok; g, ok = sh.NextGlyph() {
		_, face, gid := splitGlyphID(g.ID)
		out = append(out, shapedGlyph{face: face, gid: int(gid), adv: g.Advance.Round()})
	}
	return out
}

func runAdvance(gs []shapedGlyph) (w int) {
	for _, g := range gs {
		w += g.adv
	}
	return w
}

// A channel named only with a Hangul filler (letters, so channel names may
// use them) keeps its glyph: HarfBuzz leaves U+115F, U+1160, U+3164 and
// U+FFA0 out of its default ignorables ("the way Uniscribe has
// implemented them is with regular spacing glyphs") and shapes them as
// ordinary spacing glyphs, so Slack draws a font's blank glyph or, with no
// font that has one, tofu: Chrome with Slack's own Lato draws its "?" box
// for U+115F (dogfood, α.104; commit 02c7f4ca). The default ignorables it
// does hide draw nothing, and CleanText drops zero-width spaces.
func TestCleanTextKeepsHangulFillers(t *testing.T) {
	sh := text.NewShaper(text.NoSystemFonts(), text.WithCollection(FacesFor(false)))
	for _, r := range []rune{0x115F, 0x1160, 0x3164, 0xFFA0} {
		s := string(r)
		if CleanText(s) != s {
			t.Errorf("CleanText dropped U+%04X", r)
		}
		// No embedded face maps it: without system fonts it shapes as
		// Lato's .notdef box (1063/2000 em), Chrome's tofu, not as nothing.
		if gs := shapeLato(sh, Regular, 30, CleanText(s)); len(gs) != 1 || gs[0].gid != 0 || gs[0].adv != 16 {
			t.Errorf("U+%04X shaped as %+v; want one .notdef (glyph 0), 16px wide", r, gs)
		}
	}
	if got := CleanText("a\u200Bb\uFEFF"); got != "ab" {
		t.Errorf("CleanText(a ZWSP b BOM) = %q, want %q", got, "ab")
	}
	// richview's words reach the shaper without CleanText: these draw
	// nothing there either, as in Chrome (0px)
	for _, r := range []rune{0x200B, 0x2060, 0xFEFF} {
		if gs := shapeLato(sh, Regular, 30, string(r)); runAdvance(gs) != 0 {
			t.Errorf("U+%04X shaped as %+v; want no advance", r, gs)
		}
	}
}

// A .notdef in a bold or black row is that face's box, as Blink draws it:
// FontFallbackIterator's last stage (kFirstCandidateForNotdefGlyph) gives
// the first candidate, the family's face at the asked weight. fontscan's
// last resort was the collection's first face, Lato Regular, whatever the
// weight (typesetting patch 4).
func TestNotdefTakesTheTextWeight(t *testing.T) {
	faces := FacesFor(false)
	sh := text.NewShaper(text.NoSystemFonts(), text.WithCollection(faces))
	for _, tc := range []struct {
		name string
		w    font.Weight
		adv  int // the face's .notdef at 60px (0: not checked)
	}{
		{"regular", Regular, 32}, // 1063/2000 em
		{"bold", Bold, 33},       // 1094/2000 em
		{"black", Black, 0},
	} {
		gs := shapeLato(sh, tc.w, 60, "\u115F")
		if len(gs) != 1 || gs[0].gid != 0 {
			t.Errorf("%s: U+115F shaped as %+v, want one .notdef", tc.name, gs)
			continue
		}
		if f := faces[gs[0].face].Font; f.Weight != tc.w || f.Style != font.Regular || f.Typeface != Family {
			t.Errorf("%s: the .notdef is %s %v %v's, want the %s face's", tc.name, f.Typeface, f.Weight, f.Style, tc.name)
		}
		if tc.adv > 0 && gs[0].adv != tc.adv {
			t.Errorf("%s: the .notdef is %dpx wide, want %d", tc.name, gs[0].adv, tc.adv)
		}
	}
}

// U+180F MONGOLIAN FREE VARIATION SELECTOR FOUR (Unicode 14) is a default
// ignorable like FVS1-3 and the vowel separator: HarfBuzz hides
// 0x180B..0x180F (is_default_ignorable), and Chrome draws nothing for it.
// go-text's list stopped at U+180E, so FVS4 shaped as a mark with Lato's
// .notdef glyph, a box drawn over the text (typesetting patch 5). Between
// two letters each selector must shape as FVS1 does: hidden, no .notdef
// and no advance.
func TestMongolianVariationSelectorsHidden(t *testing.T) {
	sh := text.NewShaper(text.NoSystemFonts(), text.WithCollection(FacesFor(false)))
	want := shapeLato(sh, Regular, 30, "a"+string(rune(0x180B))+"b")
	if slices.ContainsFunc(want, func(g shapedGlyph) bool { return g.gid == 0 }) {
		t.Fatalf("FVS1 between two letters shaped as %+v, with a .notdef", want)
	}
	for r := rune(0x180C); r <= 0x180F; r++ {
		gs := shapeLato(sh, Regular, 30, "a"+string(r)+"b")
		notdef := slices.ContainsFunc(gs, func(g shapedGlyph) bool { return g.gid == 0 })
		if notdef || !slices.Equal(gs, want) {
			t.Errorf("U+%04X between two letters shaped as %+v; want FVS1's %+v (hidden: no .notdef, no advance)", r, gs, want)
		}
	}
}
