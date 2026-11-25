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
	f := &rt.File{ID: "F0CANVCLIP1", Name: "Fun ideas", Title: "Fun ideas", Type: "quip", Mode: "quip",
		CanvasPreview: []rt.Block{{Kind: rt.List, Items: items}}}
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
				ops := new(op.Ops)
				paint.FillShape(ops, bg, clip.Rect{Max: image.Pt(w, h)}.Op())
				v := New(doc(fileBlock(f), para(txt("Next heading"))), Options{TS: "1700000000.000100", Channel: "C0CLIP"})
				gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(w, h))}
				v.Layout(gtx, st, nil)

				// the preview was cut, across a line (a line's top within
				// one 22px line of the cut): there is something to clip
				if len(v.d.fragCuts) == 0 {
					t.Fatal("the preview wasn't cut at 240px: nothing to check")
				}
				c := v.d.fragCuts[0]
				crossed := false
				for i := c.from; i < c.to; i++ {
					if fr := v.d.frags[i]; fr.top < c.bot && fr.top > c.bot-22 {
						crossed = true
					}
				}
				if !crossed {
					t.Fatalf("no line crosses the cut at y %d: nothing to check", c.bot)
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
				// columns: the pane alone
				ink := 0
				band := image.Rect(card.Min.X+12, card.Max.Y+1, card.Max.X-12, card.Max.Y+5)
				for y := band.Min.Y; y < band.Max.Y; y++ {
					for x := band.Min.X; x < band.Max.X; x++ {
						if colDist(img.RGBAAt(x, y), color.RGBA(bg)) > 40 {
							ink++
						}
					}
				}
				if ink > 0 {
					t.Errorf("%d inked pixels in %v, under the card's bottom edge at y %d: the preview's text runs past the card", ink, band, card.Max.Y)
				}
			})
		}
	}
}
