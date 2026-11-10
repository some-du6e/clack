package theme

import (
	"testing"

	"gioui.org/text"
)

// A channel named only U+3164 (HANGUL FILLER, a letter, so channel
// names may use it) keeps its glyph: HarfBuzz shapes the four Hangul
// fillers as ordinary spacing glyphs, so Slack draws a font's blank glyph
// or, with no font that has one, tofu. Zero-width spaces still go.
func TestCleanTextKeepsHangulFillers(t *testing.T) {
	for _, r := range []rune{0x115F, 0x1160, 0x3164, 0xFFA0} {
		if s := string(r); CleanText(s) != s {
			t.Errorf("CleanText dropped U+%04X", r)
		}
	}
	if got := CleanText("a\u200Bb\uFEFF"); got != "ab" {
		t.Errorf("CleanText(a ZWSP b BOM) = %q, want %q", got, "ab")
	}
	// No embedded face maps U+3164: without system fonts it shapes as
	// Lato's .notdef box (1063/2000 em), Chrome's tofu, not as nothing.
	sh := text.NewShaper(text.NoSystemFonts(), text.WithCollection(FacesFor(false)))
	sh.LayoutString(text.Parameters{Font: Font(Regular), PxPerEm: 30 << 6, MaxWidth: 1000, MaxLines: 1}, CleanText("\u3164"))
	n := 0
	for g, ok := sh.NextGlyph(); ok; g, ok = sh.NextGlyph() {
		n++
		_, _, gid := splitGlyphID(g.ID)
		if gid != 0 || g.Advance.Round() != 16 {
			t.Errorf("U+3164 shaped as glyph %d, %dpx wide; want .notdef (0), 16px", gid, g.Advance.Round())
		}
	}
	if n != 1 {
		t.Errorf("U+3164 shaped as %d glyphs, want 1", n)
	}
}
