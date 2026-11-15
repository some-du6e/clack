package theme

import (
	"strings"
	"testing"

	"gioui.org/font"
	"gioui.org/text"
	gtfont "github.com/go-text/typesetting/font"
	"golang.org/x/image/math/fixed"

	"github.com/some-du6e/clack/assets/fonts"
	"github.com/some-du6e/clack/internal/ui/shortcut"
)

// keysRunes are the code points the face maps (keysface_gen.py's KEYS):
// the Mac key symbols Lato lacks.
const keysRunes = "⌃⌥⇧⌘⎋⌫⌦⏎↩⇥⇤⇪⌅⇞⇟"

// keysGlyph is one shaped glyph: its face in the collection and its gid.
type keysGlyph struct {
	r    rune
	face int
	gid  gtfont.GID
	adv  fixed.Int26_6
	asc  fixed.Int26_6
	desc fixed.Int26_6
}

// shapeKeys lays s out on one line and pairs each glyph with its rune
// (every string here shapes one glyph per rune).
func shapeKeys(t *testing.T, sh *text.Shaper, f font.Font, s string) []keysGlyph {
	t.Helper()
	sh.LayoutString(text.Parameters{Font: f, PxPerEm: fixed.I(30), MaxWidth: 1 << 20, MaxLines: 1}, s)
	rs := []rune(s)
	var out []keysGlyph
	for g, ok := sh.NextGlyph(); ok; g, ok = sh.NextGlyph() {
		if g.Flags&text.FlagParagraphBreak != 0 && g.Runes == 0 {
			continue
		}
		_, fi, gid := splitGlyphID(g.ID)
		kg := keysGlyph{face: fi, gid: gid, adv: g.Advance, asc: g.Ascent, desc: g.Descent}
		if i := len(out); i < len(rs) {
			kg.r = rs[i]
		}
		out = append(out, kg)
	}
	if len(out) != len(rs) {
		t.Fatalf("%q shaped as %d glyphs, want %d", s, len(out), len(rs))
	}
	return out
}

// keysFaceIndexes are the collection indexes of KeysFamily's faces in a
// shaper NewShaperFor / NewShaperWith built.
func keysFaceIndexes(t *testing.T, sh *text.Shaper) map[int]bool {
	t.Helper()
	v, ok := shaperFaces.Load(sh)
	if !ok {
		t.Fatal("shaper has no registered faces")
	}
	want := map[*gtfont.Font]bool{
		parseFace(keysRegularTTF).Face().Font: true,
		parseFace(keysBoldTTF).Face().Font:    true,
	}
	idx := map[int]bool{}
	for i, f := range v.(*emboldener).faces {
		if f != nil && want[f.Font] {
			idx[i] = true
		}
	}
	if len(idx) != 2 {
		t.Fatalf("collection has %d Clack Keys faces, want 2", len(idx))
	}
	return idx
}

// "⌃⌥⇧⌘D" (the inspect tool's hint on macOS) draws real glyphs: ⌃ ⌥ ⇧ ⌘
// from the bundled Clack Keys face, D from Lato. Lato has none of the
// four, and Gio's system fallback found ⇧ and ⌘ but not ⌃ or ⌥ on macOS,
// so the α.85 toast read "Inspect tool on: □□⇧⌘D to inspect".
func TestMacModifiersShapeFromKeysFace(t *testing.T) {
	sh := NewShaperFor(false)
	keys := keysFaceIndexes(t, sh)
	for _, w := range []font.Weight{Light, Regular, Bold, Black} {
		for _, f := range []font.Font{Font(w), ItalicFont(w)} {
			gs := shapeKeys(t, sh, f, "⌃⌥⇧⌘D")
			for _, g := range gs {
				if g.gid == 0 {
					t.Errorf("%v %v: %q is .notdef (tofu)", w, f.Style, g.r)
				}
				if g.adv <= 0 {
					t.Errorf("%v %v: %q has no advance", w, f.Style, g.r)
				}
			}
			for _, g := range gs[:4] {
				if !keys[g.face] {
					t.Errorf("%v %v: %q shaped from face %d, want Clack Keys %v", w, f.Style, g.r, g.face, keys)
				}
			}
			if d := gs[4]; keys[d.face] || d.face >= 8 {
				t.Errorf("%v %v: D shaped from face %d, want Lato (0-7)", w, f.Style, d.face)
			}
		}
	}
}

// The face covers every symbol it is built for, each with an outline, and
// shapes them without any system font (so the hint can't depend on what a
// machine has installed).
func TestKeysFaceCoverage(t *testing.T) {
	for name, b := range map[string][]byte{"regular": keysRegularTTF, "bold": keysBoldTTF} {
		face := parseFace(b).Face()
		for _, r := range keysRunes {
			gid, ok := face.NominalGlyph(r)
			if !ok || gid == 0 {
				t.Errorf("%s: no glyph for %q (U+%04X)", name, r, r)
				continue
			}
			if out, ok := face.GlyphDataOutline(gid); !ok || len(out.Segments) == 0 {
				t.Errorf("%s: %q has no outline", name, r)
			}
		}
	}
	// Nothing Lato draws itself: the face only fills its gaps.
	lato := parseFace(fonts.Regular).Face()
	for _, r := range keysRunes {
		if _, ok := lato.NominalGlyph(r); ok {
			t.Errorf("Lato has %q: drop it from the keys face", r)
		}
	}
	sh := text.NewShaper(text.NoSystemFonts(), text.WithCollection(FacesFor(false)))
	sh.LayoutString(text.Parameters{Font: Font(Regular), PxPerEm: fixed.I(30), MaxWidth: 1 << 20}, keysRunes)
	n := 0
	for g, ok := sh.NextGlyph(); ok; g, ok = sh.NextGlyph() {
		if g.Flags&text.FlagParagraphBreak != 0 && g.Runes == 0 {
			continue
		}
		n++
		if _, _, gid := splitGlyphID(g.ID); gid == 0 {
			t.Errorf("glyph %d of %q is .notdef without system fonts", n, keysRunes)
		}
	}
	if want := len([]rune(keysRunes)); n != want {
		t.Errorf("%d glyphs, want %d", n, want)
	}
}

// A hint never changes a line's height: the keys face's vertical metrics
// are within Lato's, and Gio takes a line's from its tallest run.
func TestKeysFaceKeepsLatoLineHeight(t *testing.T) {
	sh := NewShaperFor(false)
	for _, w := range []font.Weight{Regular, Bold} {
		plain := shapeKeys(t, sh, Font(w), "Inspect D")
		hint := shapeKeys(t, sh, Font(w), "Inspect ⌃⌥⇧⌘D")
		if plain[0].asc != hint[0].asc || plain[0].desc != hint[0].desc {
			t.Errorf("weight %v: line ascent/descent %v/%v with the hint, %v/%v without", w, hint[0].asc, hint[0].desc, plain[0].asc, plain[0].desc)
		}
	}
}

// Every Mac shortcut hint Clack formats (menu rows, tooltips, toasts, the
// composer's + menu, click hints) shapes with no tofu and no system font,
// in Lato and in a UI font that lacks the symbols too.
func TestMacShortcutHintsHaveNoTofu(t *testing.T) {
	var hints []string
	for _, c := range []string{
		"Mod+Alt+Ctrl+Shift+D", "Mod+Shift+K", "Ctrl+Alt+Up", "Alt+Shift+Down",
		"Mod+Enter", "Mod+Shift+Enter", "Ctrl+Tab", "Mod+,", "Mod++", "Shift+Esc",
		"Alt+Left", "Ctrl+Right", "Mod+[",
	} {
		hints = append(hints, shortcut.FormatFor("darwin", c), shortcut.FormatAsWrittenFor("darwin", c))
		hints = append(hints, strings.Join(shortcut.ChipsFor("darwin", c), " "))
	}
	hints = append(hints, shortcut.ClickHintFor("darwin", "Mod+Alt"), "Inspect tool on: ⌃⌥⇧⌘D to inspect", keysRunes)
	sh := NewShaperFor(false)
	for _, h := range hints {
		for _, w := range []font.Weight{Regular, Bold} {
			stop := StartTofuScan()
			sh.LayoutString(text.Parameters{Font: Font(w), PxPerEm: fixed.I(15), MaxWidth: 1 << 20, MaxLines: 1}, h)
			for g, ok := sh.NextGlyph(); ok; g, ok = sh.NextGlyph() {
				ScanGlyph(sh, g)
			}
			if st := stop(); st.Notdef > 0 || st.Fallback > 0 {
				t.Errorf("%q (weight %v): %d .notdef, %d system-font glyphs", h, w, st.Notdef, st.Fallback)
			}
		}
	}
}
