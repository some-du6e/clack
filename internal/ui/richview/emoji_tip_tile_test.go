package richview

// The emoji tooltip draws the emoji large, as Slack's Emoji renderTooltip
// does: EmojiTooltipEmoji (the emoji at LARGER on a 64px white tile, 4px
// padding) over the :name:, centred, 4px between them. Clack drew the name
// alone (Karim, 11-24: "little puny tooltip intended?"). Synthetic data,
// drawn headlessly.

import (
	"image"
	"image/color"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	rt "github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

func TestEmojiTooltipShowsTheEmojiLikeSlack(t *testing.T) {
	em := rt.Span{Kind: rt.Emoji, ID: "okaga", URL: "https://e.test/okaga.png"}
	var bs []rt.Block
	for i := 0; i < 8; i++ {
		bs = append(bs, para(txt("a synthetic line")))
	}
	bs = append(bs, para(txt("custom "), em, txt(" here")))
	// dark: the panel isn't white, so white is the tile's
	st := NewStyle(themeShaper(), noImages{}, DarkPalette())
	v := New(doc(bs...), Options{})
	var r input.Router
	var ops op.Ops
	const w, h = 400, 320
	t0 := time.Unix(1000, 0)
	frame := func(now time.Time) {
		ops.Reset()
		paint.FillShape(&ops, st.Palette.Panel, clip.Rect{Max: image.Pt(w, h)}.Op())
		gtx := layout.Context{Ops: &ops, Source: r.Source(), Now: now, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(image.Pt(w, h))}
		v.Layout(gtx, st, &recHandler{})
		r.Frame(&ops)
	}
	frame(t0)
	var f *frag
	for i := range v.d.frags {
		if v.d.frags[i].kind == fImage {
			f = &v.d.frags[i]
		}
	}
	if f == nil {
		t.Fatal("no emoji frag")
	}
	if f.y < 120 {
		t.Fatalf("the emoji at y %d: no room above it for the tooltip", f.y)
	}
	r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(f.x+float32(f.size)/2, float32(f.y+f.size/2))})
	frame(t0)
	frame(t0.Add(EmojiTipDelay + 10*time.Millisecond))
	img := pixeltest.Render(t, w, h, &ops)

	// the box: 8px over the emoji; 8 + 64 tile + 4 + 18 line + 8 tall
	cx := int(f.x) + f.size/2
	bottom := f.y - 8
	top := bottom - (8 + 64 + 4 + 18 + 8)
	tile := image.Rect(cx-32, top+8, cx+32, top+8+64)
	white := color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	// the tile's inside, its rounded corners left out
	if n := pixeltest.Count(img, tile.Inset(3), white, 6); n < 55*55 {
		t.Errorf("the tooltip's emoji tile: %d white px in %v, want the 64px white tile (EmojiTooltipEmoji at LARGER)", n, tile)
	}
	// the :name: under it, in the tooltip's text colour
	line := image.Rect(cx-60, top+8+64+4, cx+60, bottom-8)
	if n := pixeltest.Ink(img, line, st.Palette.TooltipText, st.Palette.TooltipBg); n < 10 {
		t.Errorf("no :name: under the tile (%d px in %v)", n, line)
	}
	// and no tile in the box's text-only rows above the tile
	if n := pixeltest.Count(img, image.Rect(cx-30, top+1, cx+30, top+6), white, 6); n > 0 {
		t.Errorf("white above the tile (%d px): the box isn't where Slack puts it", n)
	}
}
