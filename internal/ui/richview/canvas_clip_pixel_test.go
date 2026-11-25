package richview

import (
	"fmt"
	"image"
	"image/color"
	"testing"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	rt "github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// Synthetic fixtures only.

// Karim's report on α.165 ("text is over the canvas thingy", a thread in
// the dark theme): a canvas card's preview longer than FileQuip's 240px
// snippet stops at the card's rounded bottom edge, where the pillow's
// overflow: hidden clips it. The line the cut crosses shows its top
// (FileQuip passes no allowExpand: no fade, no Show more), and nothing goes
// under the card's border or into the message's next block. Natively and
// with MessageFile patched (component mode), light and dark. The message
// goes on after the card (a paragraph), as Karim's did: a View clips to
// its own box, so with the card last nothing could show under it.
func TestCanvasPreviewClipped(t *testing.T) {
	var items [][]rt.Span
	for range 16 {
		items = append(items, []rt.Span{txt("orb+check me puts your check on a orb")},
			[]rt.Span{txt("drunk+press is a drunk hydraulic press for when you've had one too many")})
	}
	// a preview whose cut crosses a line 6 to 12px under its top, so the
	// line's glyphs reach under the cut: leading paragraphs (34px each: a
	// 22px line and the 12px between blocks) shift the list's 22px lines
	// against the cut by 12px a paragraph, and eleven of them shift it to
	// every even or every odd offset in a line
	preview := func(lead int) []rt.Block {
		var bs []rt.Block
		for range lead {
			bs = append(bs, para(txt("Ideas so far")))
		}
		return append(bs, rt.Block{Kind: rt.List, Items: items})
	}
	modes := []struct {
		name string
		on   func(t *testing.T)
	}{
		{"native", func(*testing.T) {}},
		{"MessageFile patched", func(t *testing.T) {
			slackc.SetPatched([]string{"MessageFile"})
			t.Cleanup(func() { slackc.SetPatched(nil) })
		}},
	}
	for _, m := range modes {
		for _, dark := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/dark=%v", m.name, dark), func(t *testing.T) {
				m.on(t)
				st := newTestStyle()
				if dark {
					st.SetPalette(DarkPalette())
				}
				const w, h = 640, 760
				bg := st.Palette.Panel
				var ops *op.Ops
				var v *View
				var c cutRange
				depth, next := 0, 0
				for lead := range 11 {
					o := new(op.Ops)
					paint.FillShape(o, bg, clip.Rect{Max: image.Pt(w, h)}.Op())
					f := &rt.File{ID: "F0CANVCLIP1", Name: "Fun ideas", Title: "Fun ideas", Type: "quip", Mode: "quip", CanvasPreview: preview(lead)}
					vv := New(doc(fileBlock(f), para(txt("Thoughts welcome"))), Options{TS: "1700000000.000100", Channel: "C0CLIP"})
					gtx := layout.Context{Ops: o, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(w, h))}
					vv.Layout(gtx, st, nil)
					if len(vv.d.fragCuts) == 0 {
						t.Fatal("the preview wasn't cut at 240px: nothing to check")
					}
					cc := vv.d.fragCuts[0]
					after := -1 // the top of the first line under the card: the paragraph's
					for i := int(cc.to); i < len(vv.d.frags); i++ {
						if vv.d.frags[i].top >= cc.bot {
							after = vv.d.frags[i].top
							break
						}
					}
					if after < 0 {
						t.Fatal("nothing under the card: the paragraph after it isn't laid out")
					}
					// a line whose top is 6 to 12px above the cut: its
					// x-height glyphs straddle it, so unclipped they ink the
					// first pixels under the card
					for i := cc.from; i < cc.to; i++ {
						if d := cc.bot - vv.d.frags[i].top; d >= 6 && d <= 12 && (depth == 0 || absInt(d-9) < absInt(depth-9)) {
							ops, v, c, depth, next = o, vv, cc, d, after
						}
					}
				}
				if v == nil {
					t.Fatal("no line's top 6 to 12px above the cut in any preview: nothing to check")
				}
				card := image.Rectangle{}
				for _, b := range v.d.boxes {
					if b.r.Dx()*b.r.Dy() > card.Dx()*card.Dy() {
						card = b.r
					}
				}
				if card.Max.Y != c.bot {
					t.Errorf("the card ends at y %d, the preview's cut at %d: want the card to end at the cut", card.Max.Y, c.bot)
				}

				img := pixeltest.Render(t, w, h, ops)
				inked := func(r image.Rectangle) int {
					n := 0
					for y := r.Min.Y; y < r.Max.Y; y++ {
						for x := r.Min.X; x < r.Max.X; x++ {
							if colDist(img.RGBAAt(x, y), color.RGBA(bg)) > 40 {
								n++
							}
						}
					}
					return n
				}
				// the preview's text is drawn: the lines above the cut ink
				// the card
				if n := inked(image.Rect(card.Min.X+12, card.Max.Y-40, card.Max.X-12, card.Max.Y-2)); n == 0 {
					t.Fatalf("no ink in the card's last 40px above the cut: the preview's text isn't drawn, nothing to check")
				}
				// under the card's bottom edge, across the preview's text
				// columns, down to 2px into the next paragraph's line box
				// (above its glyphs): the pane alone (the line the cut
				// crosses would ink it unclipped)
				band := image.Rect(card.Min.X+12, card.Max.Y+1, card.Max.X-12, min(card.Max.Y+10, next+2))
				if band.Dy() < 3 {
					t.Fatalf("the paragraph after the card starts at y %d, %dpx under it: no room to look", next, next-card.Max.Y)
				}
				if n := inked(band); n > 0 {
					t.Errorf("%d inked pixels in %v, under the card's bottom edge at y %d (a line's top %dpx above it): the preview's text runs past the card", n, band, card.Max.Y, depth)
				}
			})
		}
	}
}

func absInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
