package messages

import (
	"encoding/binary"
	"testing"
	"time"

	"github.com/some-du6e/clack/assets/fonts"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// wideUIFont is a UI font that measures very differently from Lato in
// every slot (a stand-in for Appearance > Font): Lato with every advance
// doubled, so text wraps differently at any width.
func wideUIFont() *theme.UIFont {
	u := &theme.UIFont{Name: "test-wide"}
	src := map[*byte][]byte{}
	for i, s := range theme.Slots {
		b := fonts.Regular
		switch {
		case s.Weight >= theme.Bold && s.Style == 1:
			b = fonts.BoldItalic
		case s.Weight >= theme.Bold:
			b = fonts.Bold
		case s.Style == 1:
			b = fonts.Italic
		}
		w, ok := src[&b[0]]
		if !ok {
			w = doubleAdvances(b)
			src[&b[0]] = w
		}
		u.Faces[i] = theme.FaceSlot{Slot: s, Data: w}
	}
	return u
}

// doubleAdvances copies a TrueType font with every hmtx advance doubled.
func doubleAdvances(src []byte) []byte {
	b := append([]byte(nil), src...)
	n := int(binary.BigEndian.Uint16(b[4:]))
	tables := map[string][2]int{}
	for i := 0; i < n; i++ {
		r := b[12+16*i:]
		tables[string(r[:4])] = [2]int{int(binary.BigEndian.Uint32(r[8:])), int(binary.BigEndian.Uint32(r[12:]))}
	}
	hhea, hmtx := tables["hhea"], tables["hmtx"]
	numH := int(binary.BigEndian.Uint16(b[hhea[0]+34:]))
	for i := 0; i < numH; i++ {
		at := hmtx[0] + 4*i
		binary.BigEndian.PutUint16(b[at:], 2*binary.BigEndian.Uint16(b[at:]))
	}
	return b
}

// TestFontSwitchRelayout: switching the UI font (Theme.SetUIShaper, what
// Appearance > Font installs) re-lays every row out in the new font in
// the very next frame: each on-screen row is as tall as a fresh row in
// that font, rows tile the viewport, the scrollbar forgets the old
// heights, and nothing moves afterwards. Then back to Lato.
func TestFontSwitchRelayout(t *testing.T) {
	for _, scale := range []float32{1, 2} {
		e := newFlingEnv(t, "C0FAKE00001", scale)
		e.size.X = int(460 * scale) // narrow: wider glyphs wrap more lines
		until(t, 10*time.Second, "the channel", e.frame, func() bool { return e.v.s.snap.loaded && len(e.v.s.items) > 3 })
		for i := 0; i < 10; i++ {
			e.frame()
		}
		if e.check("Lato") > 0 {
			t.Fatal("not settled in Lato")
		}
		heights := func() map[string]int {
			s := e.v.s
			out := map[string]int{}
			for i := s.visible[0]; i < s.visible[1]; i++ {
				if s.items[i].kind == itMsg {
					out[s.items[i].key] = s.heights[i]
				}
			}
			return out
		}
		lato := heights()

		sh, err := theme.NewShaperWith(wideUIFont())
		if err != nil {
			t.Fatal(err)
		}
		for _, step := range []struct{ name string }{{"wide"}, {"Lato again"}} {
			if step.name == "wide" {
				e.th.SetUIShaper(sh)
			} else {
				lsh, _ := theme.NewShaperWith(nil)
				e.th.SetUIShaper(lsh)
			}
			e.frame() // one frame
			if n := e.check(step.name + ", first frame"); n > 0 {
				t.Fatalf("@%vx %s: %d rows kept a stale layout", scale, step.name, n)
			}
			now := heights()
			if step.name == "wide" {
				changed := 0
				for k, h := range now {
					if lh, ok := lato[k]; ok && lh != h {
						changed++
					}
				}
				if changed == 0 {
					t.Errorf("@%vx: no row changed height in the wide font (%d on screen)", scale, len(now))
				}
			}
			sb := &e.v.s.sb // laid out at the end of the frame
			if sb.knownFG != theme.FontGen() {
				t.Errorf("@%vx %s: scrollbar heights from an older font", scale, step.name)
			}
			for k, h := range now {
				if sb.known[k] != h {
					t.Errorf("@%vx %s: scrollbar has %s at %d, the row is %d", scale, step.name, k, sb.known[k], h)
				}
			}
			key, y := e.firstMsg()
			for i := 0; i < 30; i++ {
				e.frame()
			}
			for k, h := range heights() {
				if now[k] != 0 && now[k] != h {
					t.Errorf("@%vx %s: %s went from %d to %d after the switch frame", scale, step.name, k, now[k], h)
				}
			}
			if ny, ok := e.yOf(key); !ok || ny != y {
				t.Errorf("@%vx %s: %s moved from %d to %d (%v) after the switch frame", scale, step.name, key, y, ny, ok)
			}
			e.check(step.name + ", settled")
		}
	}
}
