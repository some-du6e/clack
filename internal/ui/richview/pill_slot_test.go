package richview

// Where the flow keeps a pill's ::before and ::after (flow.go
// addPseudoSlot) and a rule's margin and border at its ends, for its
// component to draw them (comps.go pill): the picture's place is on the
// pill's first line right after its 2px padding, 1em of the pill's own
// 15px wide (Slack's message text, not the CSS engine's 16px root), and a
// line breaks around it where Chromium's ICU line breaker breaks around
// the U+FFFC it lays an inline-block out as (UAX #14): after it before
// "@relay" (LB20), never between "(" and it (LB14). Synthetic text.

import (
	"math"
	"testing"

	"gioui.org/op"

	rt "github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/slackc"
)

// pwSettle lays a fresh View of d out at width w until the components'
// measurements are in (the second layout keeps their places).
func pwSettle(st *Style, d rt.Doc, w int) *View {
	v := New(d, Options{})
	ops := new(op.Ops)
	for i := 0; i < 4; i++ {
		pwFrame(v, st, w, 110, ops)
	}
	return v
}

func TestPillPictureSlot(t *testing.T) {
	slackc.SetStyle("test:pill-slot", pwPictureRule)
	t.Cleanup(func() { slackc.RemoveStyle("test:pill-slot") })
	st := NewStyle(themeShaper(), noImages{}, LightPalette())
	bg := st.Palette.MentionBg
	wrapped, after := 0, 0
	for w := 150; w <= 520; w++ {
		v := pwSettle(st, pwDoc(), w)
		if len(v.d.comps) != 1 || len(v.cs) != 1 {
			t.Fatalf("w=%d: %d components", w, len(v.d.comps))
		}
		// 1em of the pill's 15px with MentionProfiles' 1px and 3px margins
		if got := v.cs[0].pseudo[0]; got != 19 || !v.cs[0].pbox[0] {
			t.Fatalf("w=%d: the picture's place is %vpx (box %v), want 1 + 15 + 3", w, got, v.cs[0].pbox[0])
		}
		sl := v.d.comps[0].slots[0]
		at, conn := pwFrag(&v.d, "@relay"), pwFrag(&v.d, "connector")
		paren := findFrag(t, &v.d, "(")
		bs := pwBoxes(&v.d, bg)
		if !sl.ok || at == nil || conn == nil || len(bs) == 0 {
			t.Fatalf("w=%d: slot %+v, words %q, boxes %v", w, sl, fragStrings(&v.d), bs)
		}
		// on the pill's first line, right after its 2px padding
		if b := bs[0]; math.Abs(float64(sl.x-float32(b.r.Min.X)-2)) > 0.6 || sl.top > float32(b.r.Min.Y) || sl.bot < float32(b.r.Max.Y) {
			t.Errorf("w=%d: the picture's place at x=%.1f (line %v..%v), the pill's first box %v", w, sl.x, sl.top, sl.bot, b.r)
		}
		if int(sl.top) != paren.top {
			t.Errorf("w=%d: \"(\" ends its line, the picture starts the next: UAX #14 LB14 keeps them together", w)
		}
		if at.line != conn.line {
			wrapped++
		}
		if float32(at.top) != sl.top {
			after++ // the line broke after the picture
			if at.x != 0 {
				t.Errorf("w=%d: @relay after the break at x=%.1f, want the line start", w, at.x)
			}
			if b := bs[0]; b.r.Max.X != int(sl.x+19+0.5) || pwCorners(b) != [4]int{3, 0, 0, 3} {
				t.Errorf("w=%d: the first line's box %v corners %v, want it to end after the picture, sliced", w, b.r, pwCorners(b))
			}
		} else if math.Abs(float64(at.x-sl.x-19)) > 0.6 {
			t.Errorf("w=%d: @relay at x=%.1f, the picture's place from %.1f: want it right after the 19px place", w, at.x, sl.x)
		}
		if float32(at.y) != sl.base && float32(at.top) == sl.top {
			t.Errorf("w=%d: the place's baseline %v, @relay's %d", w, sl.base, at.y)
		}
		if n := len(v.d.lines); v.d.size.Y != 22*n {
			t.Errorf("w=%d: %d lines %dpx tall, want 22px each (the picture doesn't change the line height)", w, n, v.d.size.Y)
		}
	}
	if wrapped < 8 || after == 0 {
		t.Errorf("the pill wrapped at %d widths, broke after the picture at %d: want both (UAX #14 LB20: CB ÷ AL)", wrapped, after)
	}
}

// A box ::after goes last inside the pill, on its last line before the
// 2px end padding; a text ::before stays glued to the name, which no line
// breaks; a rule's margin and border sit outside the pill's box at its
// ends.
func TestPillAfterAndEdges(t *testing.T) {
	slackc.SetStyle("test:pill-after", `.c-member_slug::after { content: ''; display: inline-block; width: 6px; height: 6px; background: #0000ff }
		.c-member_slug::before { content: '~' }
		.c-member_slug { margin: 0 5px; border: 1px solid #000000 }`)
	t.Cleanup(func() { slackc.RemoveStyle("test:pill-after") })
	st := NewStyle(themeShaper(), noImages{}, LightPalette())
	bg := st.Palette.MentionBg
	for w := 150; w <= 520; w += 3 {
		v := pwSettle(st, pwDoc(), w)
		cs := v.cs[0]
		if cs.pseudo[1] != 6 || !cs.pbox[1] || cs.pseudo[0] <= 0 || cs.pbox[0] || cs.edge != [2]float32{6, 6} {
			t.Fatalf("w=%d: pseudo %v box %v edges %v, want ::after 6 (a box), ::before text, edges 5+1", w, cs.pseudo, cs.pbox, cs.edge)
		}
		c := v.d.comps[0]
		bs := pwBoxes(&v.d, bg)
		at, conn := pwFrag(&v.d, "@relay"), pwFrag(&v.d, "connector")
		paren := findFrag(t, &v.d, "(")
		if !c.slots[0].ok || !c.slots[1].ok || len(bs) == 0 || at == nil || conn == nil {
			t.Fatalf("w=%d: slots %+v, boxes %v", w, c.slots, bs)
		}
		last := bs[len(bs)-1]
		if sl := c.slots[1]; sl.top != float32(conn.top) || int(sl.x+6+2+0.5) != last.r.Max.X {
			t.Errorf("w=%d: ::after's place at x=%.1f line top %v, want at the end of %v (connector's line %d)", w, sl.x, sl.top, last.r, conn.top)
		}
		if sl := c.slots[0]; sl.top != float32(at.top) {
			t.Errorf("w=%d: the text ::before on line %v, @relay on %d: text stays glued", w, sl.top, at.top)
		}
		if b := bs[0]; b.r.Min.Y > paren.bot || b.r.Min.X != int(paren.x+paren.w+6+0.5) {
			t.Errorf("w=%d: the pill's box starts at %d, \"(\" ends at %.1f: want 5px margin and 1px border between", w, b.r.Min.X, paren.x+paren.w)
		}
	}
}
