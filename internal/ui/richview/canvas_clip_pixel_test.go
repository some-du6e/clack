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
// with MessageFile patched (component mode), light and dark.
func TestCanvasPreviewClipped(t *testing.T) {
	var items [][]rt.Span
	for range 16 {
		items = append(items, []rt.Span{txt("orb+check me puts your check on a orb")},
			[]rt.Span{txt("drunk+press is a drunk hydraulic press for when you've had one too many")})
	}
	// a preview whose cut crosses a line within 12px of its top, so the
	// line's glyphs reach under the cut: leading paragraphs (34px each: a
	// 22px line and the 12px between blocks) shift the list's 22px lines
	// against the cut, and one of up to four puts a line there
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
				depth := 1 << 30
				for lead := range 4 {
					o := new(op.Ops)
					paint.FillShape(o, bg, clip.Rect{Max: image.Pt(w, h)}.Op())
					f := &rt.File{ID: "F0CANVCLIP1", Name: "Fun ideas", Title: "Fun ideas", Type: "quip", Mode: "quip", CanvasPreview: preview(lead)}
					vv := New(doc(fileBlock(f)), Options{TS: "1700000000.000100", Channel: "C0CLIP"})
					gtx := layout.Context{Ops: o, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(w, h))}
					vv.Layout(gtx, st, nil)
					if len(vv.d.fragCuts) == 0 {
						t.Fatal("the preview wasn't cut at 240px: nothing to check")
					}
					cc := vv.d.fragCuts[0]
					for i := cc.from; i < cc.to; i++ {
						if fr := vv.d.frags[i]; fr.top < cc.bot && cc.bot-fr.top <= 12 && cc.bot-fr.top < depth {
							ops, v, c, depth = o, vv, cc, cc.bot-fr.top
						}
					}
				}
				if v == nil {
					t.Fatal("no line's top within 12px above the cut in any preview: nothing to check")
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
				// under the card's bottom edge, across the preview's text
				// columns: the pane alone (the line the cut crosses would
				// ink it unclipped)
				ink := 0
				band := image.Rect(card.Min.X+12, card.Max.Y+1, card.Max.X-12, card.Max.Y+10)
				for y := band.Min.Y; y < band.Max.Y; y++ {
					for x := band.Min.X; x < band.Max.X; x++ {
						if colDist(img.RGBAAt(x, y), color.RGBA(bg)) > 40 {
							ink++
						}
					}
				}
				if ink > 0 {
					t.Errorf("%d inked pixels in %v, under the card's bottom edge at y %d (a line's top %dpx above it): the preview's text runs past the card", ink, band, card.Max.Y, depth)
				}
			})
		}
	}
}
