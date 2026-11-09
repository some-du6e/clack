package theme

import (
	"testing"

	"gioui.org/text"
	"gioui.org/widget"
	"golang.org/x/image/math/fixed"
)

// editorEmoji covers each kind of emoji grapheme: a plain one, VS16, a
// skin tone, a ZWJ family, a ZWJ profession with a tone, a flag, a
// subdivision flag (tags) and an emoji Lato draws itself (©).
var editorEmoji = []string{"😀", "❤️", "👍🏽", "👨‍👩‍👧‍👦", "🧑🏽‍💻", "🇺🇸", "🏴󠁧󠁢󠁳󠁣󠁴󠁿"}

func editorEmojiText() string {
	s := "a"
	for _, e := range editorEmoji {
		s += " " + e
	}
	return s + " ©️ b"
}

// Every emoji grapheme shapes as exactly one blank slot glyph (one
// cluster, all its runes, 1.27em wide) with EditorFont: no tofu, no
// system font, nothing Gio's editor could put a caret inside.
func TestEditorFontShapesEmojiAsSlots(t *testing.T) {
	sh := NewShaperFor(false)
	const px = 30
	stop := StartTofuScan()
	sh.LayoutString(text.Parameters{Font: EditorFont(Font(Regular)), PxPerEm: fixed.I(px), MaxWidth: 1 << 20}, editorEmojiText())
	type cluster struct {
		runes int
		adv   fixed.Int26_6
	}
	var slots []cluster
	inSlot := false
	var adv fixed.Int26_6
	for g, ok := sh.NextGlyph(); ok; g, ok = sh.NextGlyph() {
		ScanGlyph(sh, g)
		if IsEmojiSlot(sh, g) {
			inSlot = true
		}
		adv += g.Advance
		if g.Flags&text.FlagClusterBreak != 0 {
			if inSlot {
				slots = append(slots, cluster{int(g.Runes), adv})
			}
			inSlot, adv = false, 0
		}
	}
	st := stop()
	if st.Notdef > 0 || st.Fallback > 0 {
		t.Fatalf("%d .notdef, %d system-font glyphs: %+v", st.Notdef, st.Fallback, st)
	}
	if len(slots) != len(editorEmoji) {
		t.Fatalf("%d slot clusters, want %d (one per emoji)", len(slots), len(editorEmoji))
	}
	for i, c := range slots {
		if want := len([]rune(editorEmoji[i])); c.runes != want {
			t.Errorf("%q: slot cluster covers %d runes, want %d", editorEmoji[i], c.runes, want)
		}
		if c.adv.Round() != 38 { // 1.27 * 30
			t.Errorf("%q: cluster %dpx wide, want 38", editorEmoji[i], c.adv.Round())
		}
	}
	// Plain labels keep their own path: the slot face is only named by
	// editor fonts, and Lato text is unchanged by it.
	if EditorFont(Font(Bold)).Weight != Bold || string(EditorFont(EditorFont(Font(Regular))).Typeface) != string(Typeface)+", "+EmojiSlotTypeface {
		t.Errorf("EditorFont: %+v", EditorFont(EditorFont(Font(Regular))))
	}
}

// widget.Editor through Theme.Editor: arrows step over a whole emoji,
// Backspace deletes a whole emoji, selection covers it, and an image is
// drawn at every emoji Lato doesn't draw itself.
func TestThemeEditorEmoji(t *testing.T) {
	th := New(false)
	imgs := &recImages{}
	th.Images = imgs
	var ed widget.Editor
	ed.SingleLine = true
	ed.SetText(editorEmojiText())
	layout := func() {
		gtx := testGtx(4000)
		th.Editor(gtx, &ed, 15, th.C.Fg, th.C.Selection)
	}
	stop := StartTofuScan()
	layout()
	st := stop()
	if st.Glyphs == 0 || st.Notdef > 0 || st.Fallback > 0 {
		t.Fatalf("editor tofu scan: %+v", st)
	}
	for _, stem := range []string{"1f600", "2764-fe0f", "1f44d-1f3fd", "1f468-200d-1f469-200d-1f467-200d-1f466", "1f9d1-1f3fd-200d-1f4bb", "1f1fa-1f1f8"} {
		if imgs.asked(stem) == 0 {
			t.Errorf("image %s never drawn (asked %v)", stem, imgs.urls)
		}
	}
	if imgs.asked("a9-fe0f") != 0 {
		t.Errorf("© drew an image over Lato's glyph")
	}

	// caret: one arrow press crosses the whole family
	famStart := len([]rune("a 😀 ❤️ 👍🏽 "))
	famLen := len([]rune("👨‍👩‍👧‍👦"))
	ed.SetCaret(famStart, famStart)
	ed.MoveCaret(1, 1)
	if s, e := ed.Selection(); s != famStart+famLen || e != famStart+famLen {
		t.Errorf("caret after → = %d,%d, want %d", s, e, famStart+famLen)
	}
	// selection over the emoji: one region the width of the slot
	regs := ed.Regions(famStart, famStart+famLen, nil)
	if len(regs) != 1 || regs[0].Bounds.Dx() < 30 || regs[0].Bounds.Dx() > 48 {
		t.Errorf("family regions %+v, want one ~38px box", regs)
	}
	// Backspace removes the whole grapheme
	ed.SetCaret(famStart+famLen, famStart+famLen)
	ed.Delete(-1)
	want := "a 😀 ❤️ 👍🏽  🧑🏽‍💻 🇺🇸 🏴󠁧󠁢󠁳󠁣󠁴󠁿 ©️ b"
	if got := ed.Text(); got != want {
		t.Errorf("after Backspace: %q, want %q", got, want)
	}
	layout() // still lays out without the family
}

// Regions over an emoji whose cluster has several glyphs (a slot and a
// zero-width tone modifier) cover exactly the slot, and the text after it
// starts where the slot ends: the image lands on its blank glyph, not on
// the next word (Gio patch 2 in third_party/gioui.org/CLACK_PATCHES.md).
func TestThemeEditorEmojiRegions(t *testing.T) {
	th := New(false)
	th.Images = &recImages{}
	txt := "a 👋🏾 b 😀 c 🧑🏽‍💻 d"
	var ed widget.Editor
	ed.SingleLine = true
	ed.SetText(txt)
	th.Editor(testGtx(4000), &ed, 15, th.C.Fg, th.C.Selection) // 30px text
	rs := []rune(txt)
	for _, er := range EmojiRuns(rs) {
		r := ed.Regions(er.Start, er.End, nil)
		next := ed.Regions(er.End, er.End+1, nil)
		if len(r) != 1 || len(next) != 1 {
			t.Fatalf("%s: regions %+v, next %+v", er.Stem, r, next)
		}
		if w := r[0].Bounds.Dx(); w < 37 || w > 39 {
			t.Errorf("%s: region %v is %dpx wide, want the 38px slot", er.Stem, r[0].Bounds, w)
		}
		if r[0].Bounds.Max.X != next[0].Bounds.Min.X {
			t.Errorf("%s: region ends at %d, the space after it starts at %d", er.Stem, r[0].Bounds.Max.X, next[0].Bounds.Min.X)
		}
	}
}
