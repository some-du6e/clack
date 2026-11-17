package richview

// Where the flow keeps a pill's ::before and ::after (flow.go
// addPseudoSlot) and a rule's margin and border at its ends, for its
// component to draw them (comps.go pill): the picture's place is on the
// pill's first line right after its 2px padding, 1em of the pill's own
// 15px wide (Slack's message text, not the CSS engine's 16px root). The
// picture is an atomic inline, and Chromium's LineBreaker breaks before
// and after every atomic inline whatever the characters next to it
// (CanBreakAfter, CanBreakAfterAtomicInline, line_breaker.cc): "(" can end
// a line the picture starts the next of, and "@relay" can start the line
// after the picture. Under a line clamp the "…" goes where Chromium's
// LineTruncator puts it: after the picture when both fit, else the
// picture is hidden, and the pill's box stays under the "…". Synthetic
// text.

import (
	"encoding/json"
	"fmt"
	"image"
	"math"
	"testing"
	"time"

	"gioui.org/op"

	rt "github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// pwSettle lays a fresh View of d out at width w until the components'
// measurements are in (the second layout keeps their places).
func pwSettle(st *Style, d rt.Doc, w int) *View {
	return pwSettleOpts(st, d, Options{}, w)
}

func pwSettleOpts(st *Style, d rt.Doc, o Options, w int) *View {
	v := New(d, o)
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
	wrapped, after, paren := 0, 0, 0
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
		lp := findFrag(t, &v.d, "(")
		bs := pwBoxes(&v.d, bg)
		if !sl.ok || !sl.at || at == nil || conn == nil || len(bs) == 0 {
			t.Fatalf("w=%d: slot %+v, words %q, boxes %v", w, sl, fragStrings(&v.d), bs)
		}
		// on the pill's first line, right after its 2px padding
		b1 := bs[0]
		if math.Abs(float64(sl.x-float32(b1.r.Min.X)-2)) > 0.6 || sl.top > float32(b1.r.Min.Y) || sl.bot < float32(b1.r.Max.Y) {
			t.Errorf("w=%d: the picture's place at x=%.1f (line %v..%v), the pill's first box %v", w, sl.x, sl.top, sl.bot, b1.r)
		}
		if int(sl.top) > lp.top {
			// "(" ended its line, the picture (with the pill's padding)
			// opens the next one
			paren++
			if b1.r.Min.X != 0 || math.Abs(float64(sl.x-2)) > 0.6 {
				t.Errorf("w=%d: after \"(\" ended line %d, the pill's box starts at %d and the picture at %.1f, want 0 and 2", w, lp.top, b1.r.Min.X, sl.x)
			}
		}
		if at.line != conn.line {
			wrapped++
		}
		if float32(at.top) != sl.top {
			after++ // the line broke after the picture
			if at.x != 0 {
				t.Errorf("w=%d: @relay after the break at x=%.1f, want the line start", w, at.x)
			}
			if b1.r.Max.X != int(sl.x+19+0.5) || pwCorners(b1) != [4]int{3, 0, 0, 3} {
				t.Errorf("w=%d: the first line's box %v corners %v, want it to end after the picture, sliced", w, b1.r, pwCorners(b1))
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
	if wrapped < 8 || after == 0 || paren == 0 {
		t.Errorf("the pill wrapped at %d widths, broke after the picture at %d, after \"(\" at %d: want all three (Chromium breaks before and after an atomic inline)",
			wrapped, after, paren)
	}
}

// A box ::after goes last inside the pill, on its last line before the
// 2px end padding, and a line may break after it: ")" can start the next
// line alone. A text ::before stays glued to the name. A rule's margin and
// border sit outside the pill's box at its ends.
func TestPillAfterAndEdges(t *testing.T) {
	slackc.SetStyle("test:pill-after", `.c-member_slug::after { content: ''; display: inline-block; width: 6px; height: 6px; background: #0000ff }
		.c-member_slug::before { content: '~' }
		.c-member_slug { margin: 0 5px; border: 1px solid #000000 }`)
	t.Cleanup(func() { slackc.RemoveStyle("test:pill-after") })
	st := NewStyle(themeShaper(), noImages{}, LightPalette())
	bg := st.Palette.MentionBg
	alone := 0
	for w := 150; w <= 520; w++ {
		v := pwSettle(st, pwDoc(), w)
		cs := v.cs[0]
		if cs.pseudo[1] != 6 || !cs.pbox[1] || cs.pseudo[0] <= 0 || cs.pbox[0] || cs.edge != [2]float32{6, 6} {
			t.Fatalf("w=%d: pseudo %v box %v edges %v, want ::after 6 (a box), ::before text, edges 5+1", w, cs.pseudo, cs.pbox, cs.edge)
		}
		c := v.d.comps[0]
		bs := pwBoxes(&v.d, bg)
		at, conn := pwFrag(&v.d, "@relay"), pwFrag(&v.d, "connector")
		lp := findFrag(t, &v.d, "(")
		rp := findFrag(t, &v.d, ")")
		if !c.slots[0].at || !c.slots[1].at || len(bs) == 0 || at == nil || conn == nil {
			t.Fatalf("w=%d: slots %+v, boxes %v", w, c.slots, bs)
		}
		// at the end of the pill's last box (it may go to the next line on
		// its own, with the end padding)
		last := bs[len(bs)-1]
		sl := c.slots[1]
		if sl.top > float32(last.r.Min.Y) || sl.bot < float32(last.r.Max.Y) || int(sl.x+6+2+0.5) != last.r.Max.X {
			t.Errorf("w=%d: ::after's place at x=%.1f, line %v..%v, want at the end of the last box %v", w, sl.x, sl.top, sl.bot, last.r)
		}
		// ")" after it: on its line right after the end padding and the
		// rule's border and margin, or the next line's start
		switch {
		case rp.top == int(sl.top):
			if math.Abs(float64(rp.x-(sl.x+6+2+6))) > 0.6 {
				t.Errorf("w=%d: \")\" at x=%.1f, the ::after's place from %.1f: want 2px padding and 6px after it", w, rp.x, sl.x)
			}
		case rp.top > int(sl.top):
			alone++
			if rp.x != 0 {
				t.Errorf("w=%d: \")\" went to the next line at x=%.1f, want its start", w, rp.x)
			}
		default:
			t.Errorf("w=%d: \")\" on line %d before the ::after's %v", w, rp.top, sl.top)
		}
		if sl := c.slots[0]; sl.top != float32(at.top) {
			t.Errorf("w=%d: the text ::before on line %v, @relay on %d: text stays glued", w, sl.top, at.top)
		}
		if b := bs[0]; b.r.Min.Y > lp.bot || b.r.Min.Y >= lp.top && b.r.Min.X != int(lp.x+lp.w+6+0.5) {
			t.Errorf("w=%d: the pill's box starts at %d, \"(\" ends at %.1f: want 5px margin and 1px border between", w, b.r.Min.X, lp.x+lp.w)
		}
	}
	if alone == 0 {
		t.Error("\")\" never went to the next line after the ::after box: Chromium breaks after every atomic inline")
	}
}

// A line clamp that ends a line with the picture (the line breaks after
// it, "@relay" starts the next): the "…" goes after the picture when both
// fit, 1px past its margin box (PlaceEllipsisNextTo: the picture's border
// box offset, past its 1px left margin, plus its margin box's width), in
// the pill's color (an inline-block's "…" takes its style:
// LayoutObject::SlowEffectiveStyle); when they don't, the picture is
// hidden and "…" follows "(" in the text's color. Either way the pill's
// 21px piece (its padding and the picture's place) keeps its box,
// rounded at its start and square at its end (the pill goes on past the
// clamp), as TruncateLine changes no inline box.
func TestPillEllipsisAfterPicture(t *testing.T) {
	slackc.SetStyle("test:pill-clamp", pwPictureRule)
	t.Cleanup(func() { slackc.RemoveStyle("test:pill-clamp") })
	st := NewStyle(themeShaper(), noImages{}, LightPalette())
	bg := st.Palette.MentionBg
	kept, hidden := 0, 0
	for w := 140; w <= 340; w++ {
		full := pwSettle(st, pwDoc(), w)
		fsl, at := full.d.comps[0].slots[0], pwFrag(&full.d, "@relay")
		if !fsl.ok || at == nil || int(fsl.top) != full.d.lines[0].top || at.line == 0 {
			continue // not the layout that ends the first line with the picture
		}
		v := pwSettleOpts(st, pwDoc(), Options{MaxLines: 1}, w)
		ell, lp := pwEll(&v.d), findFrag(t, &v.d, "(")
		bs, fb := pwBoxes(&v.d, bg), pwBoxes(&full.d, bg)
		if ell == nil || len(v.d.lines) != 1 {
			t.Fatalf("w=%d: %d lines, \"…\" %v under a 1-line clamp", w, len(v.d.lines), ell)
		}
		if len(bs) != 1 || len(fb) == 0 || bs[0].r != fb[0].r || bs[0].r.Dx() != 21 || pwCorners(bs[0]) != [4]int{3, 0, 0, 3} {
			t.Errorf("w=%d: the clamped pill's boxes %v (unclamped %v), want its first line's 21px box, rounded at its start, square at its end", w, bs, fb)
		}
		switch sl := v.d.comps[0].slots[0]; {
		case sl.ok && sl.at:
			kept++
			if math.Abs(float64(ell.x-(sl.x+20))) > 0.6 || ell.col != st.Palette.MentionText {
				t.Errorf("w=%d: \"…\" at %.1f in %v, the picture's place %.1f..%.1f: want it 1px past the place, in the pill's %v", w, ell.x, ell.col, sl.x, sl.x+19, st.Palette.MentionText)
			}
			if ell.x+ell.w > float32(w)+0.01 {
				t.Errorf("w=%d: \"…\" at %.1f..%.1f past the line's end", w, ell.x, ell.x+ell.w)
			}
		default:
			hidden++
			if math.Abs(float64(ell.x-(lp.x+lp.w))) > 0.6 || ell.col != st.Palette.Text {
				t.Errorf("w=%d: the picture hidden, \"…\" at %.1f in %v, \"(\" ends at %.1f: want it right after, in the text's %v", w, ell.x, ell.col, lp.x+lp.w, st.Palette.Text)
			}
			if fsl.x+20+ell.w <= float32(w) {
				t.Errorf("w=%d: the picture hidden, but it and \"…\" fit (to %.1f)", w, fsl.x+20+ell.w)
			}
		}
	}
	if kept == 0 || hidden == 0 {
		t.Errorf("the clamp ended the line with the picture kept at %d widths, hidden at %d: want both", kept, hidden)
	}
}

// The "…" a clamp puts over a component pill's box (the picture hidden
// after "(") is drawn over the box, which the component paints after the
// static text: Chromium paints the ellipsis after every child of its line.
func TestPillEllipsisDrawsOverPill(t *testing.T) {
	slackc.SetStyle("test:pill-clamp-draw", pwPictureRule)
	t.Cleanup(func() { slackc.RemoveStyle("test:pill-clamp-draw") })
	st := NewStyle(themeShaper(), noImages{}, LightPalette())
	const h = 40
	for w := 140; w <= 340; w++ {
		full := pwSettle(st, pwDoc(), w)
		fsl, at := full.d.comps[0].slots[0], pwFrag(&full.d, "@relay")
		if !fsl.ok || at == nil || int(fsl.top) != full.d.lines[0].top || at.line == 0 {
			continue
		}
		v := pwSettleOpts(st, pwDoc(), Options{MaxLines: 1}, w)
		bs := pwBoxes(&v.d, st.Palette.MentionBg)
		ell := pwEll(&v.d)
		if sl := v.d.comps[0].slots[0]; sl.ok || ell == nil || len(bs) != 1 {
			continue // the picture kept
		}
		ops := new(op.Ops)
		pwFrame(v, st, w, h, ops)
		img := pixeltest.Render(t, w, h, ops)
		pwSave(t, fmt.Sprintf("pill-wrap-richview-clamp-%d", w), img)
		r := image.Rect(int(ell.x+1.5), bs[0].r.Min.Y, int(ell.x+ell.w), bs[0].r.Max.Y).Intersect(bs[0].r) // clear of "("
		n := 0
		for y := r.Min.Y; y < r.Max.Y; y++ {
			for x := r.Min.X; x < r.Max.X; x++ {
				if c := img.RGBAAt(x, y); 299*int(c.R)+587*int(c.G)+114*int(c.B) < 120*1000 {
					n++
				}
			}
		}
		if n < 3 {
			t.Errorf("w=%d: %d dark px of the \"…\" in the pill's box %v at %v: drawn under the box", w, n, bs[0].r, r)
		}
		return
	}
	t.Fatal("no width hid the picture under the clamp")
}

// A pill that goes on past a line clamp has no end edge on its last line
// shown, for a rule's border, corners and shadow too: its component's
// WrapInline gets each box's cut sides (compRef.lines), as Chromium paints
// a fragment that isn't the inline box's last. A pill that ends on the
// clamped line keeps its end edge.
func TestPillClampOpenEnd(t *testing.T) {
	slackc.SetStyle("test:pill-clamp-end", pwPictureRule)
	t.Cleanup(func() { slackc.RemoveStyle("test:pill-clamp-end") })
	st := NewStyle(themeShaper(), noImages{}, LightPalette())
	open, closed := 0, 0
	for w := 140; w <= 420; w++ {
		v := pwSettleOpts(st, pwDoc(), Options{MaxLines: 1}, w)
		if pwEll(&v.d) == nil {
			continue
		}
		fr := (&compRef{v: v, i: 0, st: st}).lines(nil)
		if len(fr) == 0 {
			continue // the pill starts past the clamp
		}
		full := pwSettle(st, pwDoc(), w)
		goesOn := len(pwBoxes(&full.d, st.Palette.MentionBg)) > 1
		if l := fr[len(fr)-1]; l.CutEnd != goesOn || l.CutStart {
			t.Errorf("w=%d: the clamped pill's last fragment %+v, want an open end %v (it goes on past the clamp)", w, l, goesOn)
		}
		if goesOn {
			open++
		} else {
			closed++
		}
	}
	if open == 0 || closed == 0 {
		t.Errorf("the pill went on past the clamp at %d widths and ended before it at %d: want both", open, closed)
	}
}

// A table column's natural width is its widest cell's max-content width:
// a cell's widest hard line, each line with its own pills (one on another
// line doesn't widen it), a pill with its padding and its component's
// ::before. A member the store can't name before a pill (MissingText's
// 120px bar, no component) doesn't shift which component's state a later
// pill is measured with, so the pill doesn't wrap in its cell.
func TestTableCellWidthPerLine(t *testing.T) {
	slackc.SetStyle("test:pill-table-width", pwPictureRule)
	t.Cleanup(func() { slackc.RemoveStyle("test:pill-table-width") })
	const gone = "U0GONE0001"
	SetMemberRead(func(id string) (string, MemberMiss) {
		if id == gone {
			return "", MemberGone
		}
		return "", MemberLoaded
	})
	t.Cleanup(func() { SetMemberRead(nil) })
	st := NewStyle(themeShaper(), noImages{}, LightPalette())
	d := doc(rt.Block{Kind: rt.Table, Rows: [][][]rt.Span{
		{{txt("Notes")}, {txt("Who")}},
		{{txt("a much longer description of it"), {Kind: rt.Break}, {Kind: rt.User, ID: "U0ANA00001", Text: "@ana"}}, {{Kind: rt.User, ID: gone, Text: "@ghost"}}},
		{{txt("intake")}, {{Kind: rt.User, ID: pwApp, Text: "@relay connector"}}},
	}})
	v := pwSettle(st, d, 900)
	desc, who := findFrag(t, &v.d, "a"), findFrag(t, &v.d, "Who")
	it := findFrag(t, &v.d, "it")
	at, conn := pwFrag(&v.d, "@relay"), pwFrag(&v.d, "connector")
	if len(v.d.comps) != 2 || at == nil || conn == nil {
		t.Fatalf("comps %d, words %q", len(v.d.comps), fragStrings(&v.d))
	}
	// the first column: its 12px paddings around its widest line, the
	// description (@ana's line, 23px wider than its text, is narrower)
	if col0, want := who.x-desc.x-2*12-1, it.x+it.w-desc.x; math.Abs(float64(col0-want)) > 1.5 {
		t.Errorf("the first column's content is %.1fpx wide, its widest line %.1fpx: want that line's width", col0, want)
	}
	if sl := v.d.comps[1].slots[0]; at.line != conn.line || !sl.ok || int(sl.top) != at.top {
		t.Errorf("@relay connector wrapped in its cell (@relay on line %d, connector on %d, its picture's place %+v): the column was measured without its picture", at.line, conn.line, sl)
	}
}

// When the line breaks after the picture, the mention's box (the profile
// card's anchor, a plugin's click rect) still holds the first line's piece:
// the picture and the padding before it.
func TestPillBoundsHoldPictureLine(t *testing.T) {
	slackc.SetStyle("test:pill-bounds", pwPictureRule)
	t.Cleanup(func() { slackc.RemoveStyle("test:pill-bounds") })
	st := NewStyle(themeShaper(), noImages{}, LightPalette())
	seen := 0
	for w := 150; w <= 520; w++ {
		v := pwSettle(st, pwDoc(), w)
		at := pwFrag(&v.d, "@relay")
		bs := pwBoxes(&v.d, st.Palette.MentionBg)
		if at == nil || len(bs) < 2 || bs[0].r.Max.Y > at.top {
			continue
		}
		seen++
		if r := v.targetBounds(at.target); !bs[0].r.In(r) || !fragRect(at).In(r) {
			t.Errorf("w=%d: the mention's bounds %v leave out its first line's box %v or @relay %v", w, r, bs[0].r, fragRect(at))
		}
	}
	if seen == 0 {
		t.Fatal("the line never broke after the picture")
	}
}

// clickBridge is a JS side that listens for clicks on the document and
// keeps the events Clack dispatches at a link's stand-in element
// (slackc.ClickLink).
type clickBridge struct{ evs [][]byte }

func (b *clickBridge) Render(uint64, string, []byte)               {}
func (b *clickBridge) RenderFn(uint64, int64, []byte)              {}
func (b *clickBridge) Unmount(uint64)                              {}
func (b *clickBridge) Dispatch(_ int64, ev []byte)                 { b.evs = append(b.evs, append([]byte(nil), ev...)) }
func (b *clickBridge) Call(int64, []byte, func([]byte, error))     {}
func (b *clickBridge) SetRects(map[int64][4]float32)               {}
func (b *clickBridge) SetViewport(float32, float32, float32, bool) {}
func (b *clickBridge) WantsEvent(typ string) bool                  { return typ == "click" }

// A click on a mention whose line broke after its picture goes to a plugin
// listening for clicks with the element's bounding rect (clickLink), which
// holds the first line's piece too: the picture and the padding before it.
func TestPillClickRectHoldsPictureLine(t *testing.T) {
	slackc.SetStyle("test:pill-click", pwPictureRule)
	t.Cleanup(func() { slackc.RemoveStyle("test:pill-click") })
	b := &clickBridge{}
	slackc.SetBridge(b)
	t.Cleanup(func() { slackc.SetBridge(nil) })
	st := NewStyle(themeShaper(), noImages{}, LightPalette())
	seen := 0
	for w := 150; w <= 520 && seen < 3; w++ {
		hs := &harness{st: st, v: New(pwDoc(), Options{}), h: &recHandler{}, w: w}
		for i := 0; i < 4; i++ {
			hs.frame()
		}
		at := pwFrag(&hs.v.d, "@relay")
		bs := pwBoxes(&hs.v.d, st.Palette.MentionBg)
		if at == nil || len(bs) < 2 || bs[0].r.Max.Y > at.top {
			continue
		}
		seen++
		b.evs = b.evs[:0]
		p := center(at)
		hs.send(press(p, time.Duration(seen)*time.Second), release(p, time.Duration(seen)*time.Second+20*time.Millisecond))
		if len(b.evs) != 1 {
			t.Fatalf("w=%d: %d click events dispatched, want 1", w, len(b.evs))
		}
		var ev struct {
			Native struct {
				Rect [4]float32 `json:"rect"`
			} `json:"native"`
		}
		if err := json.Unmarshal(b.evs[0], &ev); err != nil {
			t.Fatalf("w=%d: %v in %s", w, err, b.evs[0])
		}
		r := ev.Native.Rect
		got := image.Rect(int(r[0]), int(r[1]), int(r[0]+r[2]+0.5), int(r[1]+r[3]+0.5))
		if !bs[0].r.In(got) || !fragRect(at).In(got) {
			t.Errorf("w=%d: the click's rect %v leaves out the picture's line box %v or @relay %v", w, got, bs[0].r, fragRect(at))
		}
	}
	if seen == 0 {
		t.Fatal("the line never broke after the picture")
	}
}

// In a table cell a center- or right-aligned column moves the cell's
// content after the flow laid it out: the pill's picture and the plugin
// tree's origin move with its box (they're kept relative to it).
func TestPillPictureInAlignedTable(t *testing.T) {
	slackc.SetStyle("test:pill-table", pwPictureRule)
	t.Cleanup(func() { slackc.RemoveStyle("test:pill-table") })
	st := NewStyle(themeShaper(), noImages{}, LightPalette())
	for _, align := range []string{"center", "right"} {
		d := doc(rt.Block{Kind: rt.Table, ColAlign: []string{"", align}, Rows: [][][]rt.Span{
			{{txt("Owner")}, {txt("Contact")}},
			{{txt("intake")}, {rt.Span{Kind: rt.User, ID: pwApp, Text: "@ana"}}},
		}})
		v := pwSettle(st, d, 520)
		bs := pwBoxes(&v.d, st.Palette.MentionBg)
		at := pwFrag(&v.d, "@ana")
		if len(v.d.comps) != 1 || len(bs) != 1 || at == nil {
			t.Fatalf("%s: comps %d, boxes %v, @ana %v", align, len(v.d.comps), bs, at)
		}
		c := &v.d.comps[0]
		sl := c.slots[0]
		if !sl.at || math.Abs(float64(sl.x-float32(bs[0].r.Min.X)-2)) > 0.6 || math.Abs(float64(at.x-sl.x-19)) > 0.6 {
			t.Errorf("%s: the picture's place %+v, the pill's box %v, @ana at %.1f: want it 2px into the box, right before the name", align, sl, bs[0].r, at.x)
		}
		if o := c.origin(); o.Min.X != bs[0].r.Min.X || !bs[0].r.In(o) {
			t.Errorf("%s: the component's origin %v, the pill's box %v", align, o, bs[0].r)
		}
		contact := findFrag(t, &v.d, "Contact")
		if align == "right" && math.Abs(float64((at.x+at.w+2)-(contact.x+contact.w))) > 1.5 {
			t.Errorf("right: the pill ends at %.1f, the column's header at %.1f", at.x+at.w+2, contact.x+contact.w)
		}
	}
}

// Inline code that wraps is sliced like a pill: each line's box has its
// border closed only at the code's ends (no right side and square right
// corners before the break, no left side after it), and the space it broke
// at collapses.
func TestCodeChipSlicedBoxes(t *testing.T) {
	st := NewStyle(themeShaper(), noImages{}, LightPalette())
	d := doc(para(txt("run "), rt.Span{Kind: rt.Text, Text: "npm install --save-dev some-package", Style: rt.Code}, txt(" now")))
	seen := 0
	for w := 120; w <= 420; w++ {
		v := layoutAt(st, d, Options{}, w)
		var bs []box
		for _, b := range v.d.boxes {
			if b.kind == bBorder && b.fill == st.Palette.CodeBg {
				bs = append(bs, b)
			}
		}
		if len(bs) < 2 {
			continue
		}
		seen++
		for i, b := range bs {
			first, last := i == 0, i == len(bs)-1
			var want uint8
			if !first {
				want |= cutStart
			}
			if !last {
				want |= cutEnd
			}
			if b.cut != want {
				t.Errorf("w=%d: code box %d of %d %v cut %b, want %b", w, i+1, len(bs), b.r, b.cut, want)
			}
		}
		// the first line's box ends at its last word, the space collapsed
		var lastOn *frag
		for i := range v.d.frags {
			if f := &v.d.frags[i]; f.kind == fText && f.size == 12 && f.top <= bs[0].r.Min.Y && f.bot >= bs[0].r.Max.Y {
				lastOn = f
			}
		}
		if lastOn == nil || bs[0].r.Max.X != int(lastOn.x+lastOn.w+0.5) {
			t.Errorf("w=%d: the first code box %v, its last word %v", w, bs[0].r, lastOn)
		}
	}
	if seen < 5 {
		t.Fatalf("the code wrapped at %d widths", seen)
	}
}
