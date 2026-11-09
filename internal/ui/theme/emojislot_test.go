package theme

import (
	"image"
	"image/color"
	"strings"
	"sync"
	"testing"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
)

// recImages is an EmojiImages that serves a 1x1 image for every URL and
// records what was asked for.
type recImages struct {
	mu   sync.Mutex
	urls []string
}

func (r *recImages) Get(url string, _ int) (paint.ImageOp, bool) {
	r.mu.Lock()
	r.urls = append(r.urls, url)
	r.mu.Unlock()
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.White)
	return paint.NewImageOp(img), true
}

func (r *recImages) asked(stem string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, u := range r.urls {
		if strings.HasSuffix(u, "/"+stem+".png") {
			n++
		}
	}
	return n
}

func testGtx(w int) layout.Context {
	return layout.Context{
		Ops:         new(op.Ops),
		Metric:      unit.Metric{PxPerDp: 2, PxPerSp: 2},
		Constraints: layout.Constraints{Max: image.Pt(w, 1<<20)},
	}
}

// scream is Karim's title shape: text, three U+1F631, more text that
// wraps at the profile pane's width.
const scream = "under EN DEE AY 😱😱😱 — msgs expire in 72hrs unless they end with //"

// TestEmojiTofuScan renders a 😱 through every theme text path (single
// line, wrapping, DrawText, ellipsized multi-line) and fails when any glyph
// is .notdef (tofu) or comes from a system font instead of Slack's image.
func TestEmojiTofuScan(t *testing.T) {
	th := New(false)
	imgs := &recImages{}
	th.Images = imgs
	paths := []struct {
		name string
		draw func(gtx layout.Context)
	}{
		{"Label", func(gtx layout.Context) { th.Label(gtx, 15, Regular, th.C.Fg, "hi 😱") }},
		{"Para", func(gtx layout.Context) { th.Para(gtx, 18, 27, Regular, th.C.Fg, scream) }},
		{"Layout MaxLines 2", func(gtx layout.Context) {
			th.Layout(gtx, TextStyle{Size: 15, LineHeight: 22, MaxLines: 2, Color: th.C.Fg}, strings.Repeat(scream+" ", 4))
		}},
		{"DrawText", func(gtx layout.Context) {
			DrawText(gtx, th.Shaper, LabelOpts{Font: Font(Regular), Size: 15, LineHeight: 22, Color: th.C.Fg}, scream)
		}},
		{"Measure", func(gtx layout.Context) { th.Measure(gtx, 15, Bold, "😱 karim") }},
	}
	// the scan sees the bug: without Slack's images the emoji reach the
	// shaper and come out as .notdef (no emoji font, as on Karim's Mac) or
	// a system font's glyph
	th.Images = nil
	stop := StartTofuScan()
	th.Para(testGtx(2*370), 18, 27, Regular, th.C.Fg, scream)
	// (or, where no system font has it, as blank slots: the slot face
	// maps emoji for editors, emojislot_gen.py)
	if st := stop(); st.Notdef+st.Fallback+st.Slots < 3 {
		t.Fatalf("without images: %+v, want the three 😱 as tofu, fallback or blank slot glyphs", st)
	} else {
		t.Logf("without images (the old path): %d .notdef, %d system-font glyphs, %d blank slots", st.Notdef, st.Fallback, st.Slots)
	}
	th.Images = imgs
	for _, p := range paths {
		before := imgs.asked("1f631")
		stop := StartTofuScan()
		p.draw(testGtx(2 * 370))
		st := stop()
		if st.Glyphs == 0 {
			t.Errorf("%s: no glyphs scanned", p.name)
		}
		if st.Notdef > 0 || st.Fallback > 0 {
			t.Errorf("%s: %d .notdef and %d system-font glyphs (emoji not drawn as Slack's image)", p.name, st.Notdef, st.Fallback)
		}
		if p.name != "Measure" && imgs.asked("1f631") == before {
			t.Errorf("%s: the 1f631 image was never requested", p.name)
		}
	}
}

// Wrapping text with emoji lays out like the same text with the slot
// rune: Gio wraps it, and every visible emoji gets one image at its glyph.
func TestEmojiSlotsWrap(t *testing.T) {
	th := New(false)
	imgs := &recImages{}
	th.Images = imgs
	gtx := testGtx(2 * 370)
	d := th.Para(gtx, 18, 27, Regular, th.C.Fg, scream)
	if d.Size.Y < gtx.Sp(27)*2 {
		t.Fatalf("title didn't wrap: %v", d.Size)
	}
	if got := imgs.asked("1f631"); got != 3 {
		t.Fatalf("1f631 drawn %d times, want 3", got)
	}
	// the placeholder never makes a Lato line taller
	plain := th.Para(testGtx(2*370), 18, 27, Regular, th.C.Fg, strings.ReplaceAll(scream, "😱😱😱", "WWW"))
	if d.Size.Y != plain.Size.Y {
		t.Errorf("height %d with emoji, %d without", d.Size.Y, plain.Size.Y)
	}

	// truncated text: only the emoji that fit are drawn
	imgs2 := &recImages{}
	th.Images = imgs2
	th.Layout(testGtx(2*120), TextStyle{Size: 15, LineHeight: 22, MaxLines: 1 + 1, Color: th.C.Fg}, "a 😱 "+strings.Repeat("word ", 40)+"😱😱")
	if got := imgs2.asked("1f631"); got != 1 {
		t.Errorf("truncated: 1f631 drawn %d times, want 1", got)
	}
}

// The slot face is in every theme shaper's collection, maps its rune (and, for editors, emoji Lato lacks)
// and is never the default face.
func TestEmojiSlotFace(t *testing.T) {
	sh := NewShaperFor(false)
	if slotIndex(sh) < 0 {
		t.Fatal("NewShaperFor has no slot face")
	}
	if slotIndex(text.NewShaper(text.NoSystemFonts())) != -1 {
		t.Fatal("a foreign shaper reported a slot face")
	}
	slot := slotIndex(sh)
	// go-text keeps the space after the placeholder in its run: the slot
	// face must give it Lato's width, not zero
	sh.LayoutString(text.Parameters{Font: Font(Regular), PxPerEm: 30 << 6, MaxWidth: 1000}, "a"+string(emojiSlotRune)+" b")
	type gl struct{ face, gid, adv int }
	var gs []gl
	for g, ok := sh.NextGlyph(); ok; g, ok = sh.NextGlyph() {
		_, fi, gid := splitGlyphID(g.ID)
		gs = append(gs, gl{fi, int(gid), g.Advance.Round()})
	}
	if len(gs) != 4 || gs[0].face != 0 || gs[3].face != 0 {
		t.Fatalf("glyphs %+v", gs)
	}
	if gs[1] != (gl{slot, emojiSlotGID, 38}) { // 1.27 * 30
		t.Errorf("placeholder %+v, want the slot glyph 38px wide", gs[1])
	}
	if gs[2].adv != 8 || gs[2].gid == 0 { // Lato Regular's space: 0.256 * 30
		t.Errorf("space after the placeholder %+v, want 8px", gs[2])
	}
}
