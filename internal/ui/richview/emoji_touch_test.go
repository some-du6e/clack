package richview

// Consecutive emoji touch, as Slack's do: .c-emoji is its size (22px,
// 32px for jumbomoji) with no margin, so custom emoji tiles drawn to join
// up (stretchy ones) join. Clack gave every emoji 1px on each side, and a
// modal's preview of stretchy tiles showed a thin seam between each two
// (Karim, #clack 2026-11-22, Windows at 125%). Synthetic images.

import (
	"image"
	"image/color"
	"strings"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	rt "github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// emojiFrags are v's image frags, in order.
func emojiFrags(v *View) []*frag {
	var out []*frag
	for i := range v.d.frags {
		if f := &v.d.frags[i]; f.kind == fImage && !strings.HasPrefix(f.url, iconURL) {
			out = append(out, f)
		}
	}
	return out
}

// drawnRect is f's image as view.go draws it (whole pixels).
func drawnRect(f *frag) image.Rectangle {
	x := int(f.x + 0.5)
	return image.Rect(x, f.y, x+f.size, f.y+f.size)
}

// TestEmojiRunsTouch: four emoji in a row draw edge to edge, each image
// starting where the one before it ends, medium and jumbo, at 100% and
// 125%, for custom emoji and for unicode ones in text.
func TestEmojiRunsTouch(t *testing.T) {
	st := newTestStyle()
	em := func(n string) rt.Span { return rt.Span{Kind: rt.Emoji, ID: n, URL: "https://emoji.test/" + n + ".png"} }
	for _, c := range []struct {
		name  string
		d     rt.Doc
		scale float32
	}{
		{"custom", doc(para(em("a"), em("b"), em("c"), em("d"))), 1},
		{"custom at 125%", doc(para(em("a"), em("b"), em("c"), em("d"))), 1.25},
		{"jumbo", rt.Doc{Blocks: []rt.Block{para(em("a"), em("b"), em("c"), em("d"))}, Jumbo: true}, 1},
		{"jumbo at 125%", rt.Doc{Blocks: []rt.Block{para(em("a"), em("b"), em("c"), em("d"))}, Jumbo: true}, 1.25},
		{"unicode in text", doc(para(txt("🟢🟢🟢🟢"))), 1.25},
	} {
		v := New(c.d, Options{})
		v.relayout(st, 600, c.scale, c.scale)
		fs := emojiFrags(v)
		if len(fs) != 4 {
			t.Fatalf("%s: %d emoji frags, want 4", c.name, len(fs))
		}
		for i := 1; i < len(fs); i++ {
			prev, cur := drawnRect(fs[i-1]), drawnRect(fs[i])
			if cur.Min.X != prev.Max.X || cur.Min.Y != prev.Min.Y {
				t.Errorf("%s: emoji %d drawn at %v, emoji %d at %v: want them touching (Slack's .c-emoji has no margin)", c.name, i-1, prev, i, cur)
			}
		}
	}
}

// tileImages serves every image as one tile: a bar across its whole width
// from 3/8 to 5/8 of its height on a transparent ground.
type tileImages struct{ img paint.ImageOp }

func (t tileImages) Get(string, int) (paint.ImageOp, bool) { return t.img, true }

var tileBar = color.NRGBA{0x6c, 0x4a, 0xd8, 0xff}

func barTile(n int) *image.NRGBA {
	im := image.NewNRGBA(image.Rect(0, 0, n, n))
	for y := n * 3 / 8; y < n*5/8; y++ {
		for x := 0; x < n; x++ {
			im.SetNRGBA(x, y, tileBar)
		}
	}
	return im
}

// TestStretchyEmojiNoSeam: a jumbo run of six tiles whose bars run edge
// to edge draws one unbroken bar at 125%: along the bar's middle row,
// every pixel from the first tile's left edge to the last one's right
// edge is the bar's colour, never the panel's showing through a seam.
func TestStretchyEmojiNoSeam(t *testing.T) {
	st := newTestStyle()
	st.Images = tileImages{img: paint.NewImageOp(barTile(64))}
	pal := st.Palette
	var spans []rt.Span
	for i := 0; i < 6; i++ {
		spans = append(spans, rt.Span{Kind: rt.Emoji, ID: "stretchy-m", URL: "https://emoji.test/stretchy-m.png"})
	}
	v := New(rt.Doc{Blocks: []rt.Block{para(spans...)}, Jumbo: true}, Options{})
	const scale = 1.25
	const w, h, pad = 520, 120, 16
	var ops op.Ops
	paint.FillShape(&ops, pal.Panel, clip.Rect{Max: image.Pt(w, h)}.Op())
	tr := op.Offset(image.Pt(pad, pad)).Push(&ops)
	gtx := layout.Context{Ops: &ops, Now: time.Now(), Metric: unit.Metric{PxPerDp: scale, PxPerSp: scale},
		Constraints: layout.Exact(image.Pt(w-2*pad, h-2*pad))}
	v.Layout(gtx, st, &recHandler{})
	tr.Pop()
	fs := emojiFrags(v)
	if len(fs) != 6 {
		t.Fatalf("%d emoji frags, want 6", len(fs))
	}
	first, last := drawnRect(fs[0]), drawnRect(fs[len(fs)-1])
	img := pixeltest.Render(t, w, h, &ops)
	y := pad + first.Min.Y + first.Dy()/2
	near := func(c color.RGBA, want color.NRGBA) bool {
		d := func(a, b uint8) int { return max(int(a)-int(b), int(b)-int(a)) }
		return d(c.R, want.R) <= 24 && d(c.G, want.G) <= 24 && d(c.B, want.B) <= 24
	}
	var seams []int
	for x := pad + first.Min.X; x < pad+last.Max.X; x++ {
		if c := img.RGBAAt(x, y); !near(c, tileBar) {
			seams = append(seams, x)
		}
	}
	if len(seams) > 0 {
		t.Errorf("the tiles' bar breaks at x %v on row %d (want one bar from x %d to %d)", seams, y, pad+first.Min.X, pad+last.Max.X)
	}
}
