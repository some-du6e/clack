package richview

import (
	"fmt"
	"image"
	"image/color"
	"slices"
	"testing"

	"gioui.org/f32"
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

// clipEmoji is the animated custom emoji the preview's lines start with.
const clipEmoji = "https://e.test/partyparrot.gif"

// clipImages serves every image as an opaque magenta square and
// clipEmoji as an animation, as imgcache does a playing GIF.
type clipImages struct{ img paint.ImageOp }

func newClipImages() *clipImages {
	m := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for i := 0; i < len(m.Pix); i += 4 {
		m.Pix[i], m.Pix[i+1], m.Pix[i+2], m.Pix[i+3] = 220, 20, 160, 255
	}
	return &clipImages{img: paint.NewImageOp(m)}
}

func (c *clipImages) Get(string, int) (paint.ImageOp, bool) { return c.img, true }
func (c *clipImages) Animated(url string, _ int) bool       { return url == clipEmoji }

// clipPreview is a canvas preview whose cut crosses a line: lead
// paragraphs (34px each: a 22px line and the 12px between blocks) shift
// the list's 22px lines against the cut by 12px a paragraph, so eleven of
// them reach every even or every odd offset in a line. Each item starts
// with clipEmoji.
func clipPreview(lead int) []rt.Block {
	em := rt.Span{Kind: rt.Emoji, ID: "partyparrot", URL: clipEmoji}
	var items [][]rt.Span
	for range 16 {
		items = append(items, []rt.Span{em, txt(" orb+check me puts your check on a orb")},
			[]rt.Span{em, txt(" drunk+press is a drunk hydraulic press")}) // one line each: every line starts with the emoji
	}
	var bs []rt.Block
	for range lead {
		bs = append(bs, para(txt("Ideas so far")))
	}
	return append(bs, rt.Block{Kind: rt.List, Items: items})
}

func clipFile(lead int) *rt.File {
	return &rt.File{ID: "F0CANVCLIP1", Name: "Fun ideas", Title: "Fun ideas", Type: "quip", Mode: "quip", CanvasPreview: clipPreview(lead)}
}

// Karim's report on α.165 ("text is over the canvas thingy", a thread in
// the dark theme): a canvas card's preview longer than FileQuip's 240px
// snippet stops at the pillow's padding box, where its overflow: hidden
// clips it: the line the cut crosses shows its top (FileQuip passes no
// allowExpand: no fade, no Show more) down to the card's bottom border,
// which stays whole, and nothing goes under the card, an animated emoji
// (painted every frame, outside the recording) included. Natively and with
// MessageFile patched (component mode), light and dark. The message goes
// on after the card (a paragraph), as Karim's did: a View clips to its own
// box, so with the card last nothing could show under it.
func TestCanvasPreviewClipped(t *testing.T) {
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
				st := NewStyle(sharedShaper(), newClipImages(), LightPalette())
				st.Face, st.Mono = "Go", "Go Mono"
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
					vv := New(doc(fileBlock(clipFile(lead)), para(txt("Thoughts welcome"))), Options{TS: "1700000000.000100", Channel: "C0CLIP"})
					gtx := layout.Context{Ops: o, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(w, h))}
					vv.Layout(gtx, st, nil)
					if len(vv.d.fragCuts) == 0 {
						t.Fatal("the preview wasn't cut at 240px: nothing to check")
					}
					cc := vv.d.fragCuts[0]
					after := -1 // the top of the first line under the card: the paragraph's
					for i := int(cc.to); i < len(vv.d.frags); i++ {
						if vv.d.frags[i].top > cc.bot {
							after = vv.d.frags[i].top
							break
						}
					}
					if after < 0 {
						t.Fatal("nothing under the card: the paragraph after it isn't laid out")
					}
					// a line whose top is 6 to 12px above the cut: its
					// x-height glyphs straddle it, so unclipped they ink the
					// border and the first pixels under the card
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
					if b.kind == bBorder && b.r.Dx()*b.r.Dy() > card.Dx()*card.Dy() {
						card = b.r
					}
				}
				if card.Max.Y != c.bot+1 {
					t.Errorf("the card ends at y %d, the preview's cut at %d: want its 1px bottom border under the cut", card.Max.Y, c.bot)
				}
				if m.name == "native" {
					// the crossing line's emoji animates: drawn each frame
					// by the live pass, outside the recording
					live := slices.ContainsFunc(v.live, func(i int32) bool {
						f := &v.d.frags[i]
						return f.y < c.bot && f.y+f.size > c.bot
					})
					if !live {
						t.Fatalf("no animated emoji the cut crosses in the live pass (live %v): nothing to check", v.live)
					}
				}

				img := pixeltest.Render(t, w, h, ops)
				px := func(x, y int) color.RGBA { return img.RGBAAt(x, y) }
				inked := func(r image.Rectangle) int {
					n := 0
					for y := r.Min.Y; y < r.Max.Y; y++ {
						for x := r.Min.X; x < r.Max.X; x++ {
							if colDist(px(x, y), color.RGBA(bg)) > 40 {
								n++
							}
						}
					}
					return n
				}
				cols := image.Rect(card.Min.X+13, 0, card.Max.X-13, 0) // inside the border and the padding
				// the crossing line shows its top: its emoji and its
				// letters' tops ink the rows above the cut
				if n := inked(image.Rect(cols.Min.X, c.bot-depth, cols.Max.X, c.bot)); n == 0 {
					t.Errorf("no ink in the crossing line's %dpx above the cut: the clip hides what Slack shows", depth)
				}
				// the bottom border: its colour only, along the straight
				// edge (the colour the corner-free end of the row has)
				by := card.Max.Y - 1
				want := px(card.Max.X-13, by)
				bad := 0
				for x := cols.Min.X; x < cols.Max.X; x++ {
					if colDist(px(x, by), want) > 24 {
						bad++
					}
				}
				if bad > 0 || colDist(want, color.RGBA(bg)) < 4 {
					t.Errorf("%d pixels of the card's bottom border row (y %d) aren't its colour %v: the crossing line paints over it", bad, by, want)
				}
				// under the card, across the text columns, down to 2px
				// into the next paragraph's line box (above its glyphs):
				// the pane alone
				band := image.Rect(cols.Min.X, card.Max.Y, cols.Max.X, min(card.Max.Y+10, next+2))
				if band.Dy() < 3 {
					t.Fatalf("the paragraph after the card starts at y %d, %dpx under it: no room to look", next, next-card.Max.Y)
				}
				if n := inked(band); n > 0 {
					t.Errorf("%d inked pixels in %v, under the card's bottom edge at y %d (a line's top %dpx above the cut): the preview runs past the card", n, band, card.Max.Y, depth)
				}
			})
		}
	}
}

// The part of an emoji the snippet's cut hides takes no pointer: under
// the cut, over the "(edited)" label a files-only message puts right
// under the card, neither HitAt nor the emoji tooltip finds the emoji (a
// right-click there opened its image menu, a hover its :name:).
func TestCanvasPreviewHitsStopAtCut(t *testing.T) {
	st := NewStyle(sharedShaper(), newClipImages(), LightPalette())
	st.Face, st.Mono = "Go", "Go Mono"
	for lead := range 11 {
		v := layoutAt(st, doc(fileBlock(clipFile(lead))), Options{TS: "1700000000.000100", Channel: "C0CLIP", Edited: true, EditedTS: "1700000100.000000"}, 640)
		if len(v.d.fragCuts) == 0 {
			t.Fatal("the preview wasn't cut at 240px: nothing to check")
		}
		c := v.d.fragCuts[0]
		for i := c.from; i < c.to; i++ {
			f := &v.d.frags[i]
			if f.kind != fImage || f.url != clipEmoji || f.y >= c.bot || f.y+f.size <= c.bot+2 {
				continue
			}
			// an emoji the cut crosses: 2px under the cut, inside it
			p := f32.Pt(f.x+float32(f.size)/2, float32(c.bot+1))
			if h := v.HitAt(p); h.EmojiURL != "" {
				t.Errorf("lead %d: HitAt under the cut (%v) finds the hidden emoji %q", lead, p, h.EmojiURL)
			}
			if j := v.emojiAt(p); j == int(i) {
				t.Errorf("lead %d: the emoji tooltip under the cut (%v) is the hidden emoji's", lead, p)
			}
			return
		}
	}
	t.Fatal("no emoji the cut crosses in any preview: nothing to check")
}

func absInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
