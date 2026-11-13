package richview

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	xdraw "golang.org/x/image/draw"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// scaledImages serves synthetic sources the way imgcache does: scaled down
// (never up) with CatmullRom so the larger side is at most px.
type scaledImages struct {
	src  map[string]*image.RGBA
	got  map[string]int // the last px asked for, per URL
	ops  map[[2]any]paint.ImageOp
	dims map[[2]any]image.Point
}

func (s *scaledImages) Get(url string, px int) (paint.ImageOp, bool) {
	s.got[url] = px
	k := [2]any{url, px}
	if op, ok := s.ops[k]; ok {
		return op, true
	}
	src, ok := s.src[url]
	if !ok {
		return paint.ImageOp{}, false
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if m := max(w, h); m > px {
		w, h = max(1, (w*px+m/2)/m), max(1, (h*px+m/2)/m)
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, b, xdraw.Src, nil)
	op := paint.NewImageOp(dst)
	s.ops[k], s.dims[k] = op, image.Pt(w, h)
	return op, true
}

// edgeImage is w x h, dark left of the middle and light right of it.
func edgeImage(w, h int) *image.RGBA {
	im := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := color.RGBA{20, 40, 60, 255}
			if x >= w/2 {
				c = color.RGBA{230, 220, 200, 255}
			}
			im.SetRGBA(x, y, c)
		}
	}
	return im
}

// Karim (karim-1113c): a link unfurl's thumbnail looked blurry. The row
// card (MessageAttachmentDefaultV3, thumb_url) covers a 106px box with a
// 1200x630 og:image; Clack asked for the copy whose larger side was the
// box's and stretched its 55px height to 104. The copy that covers the
// box's device pixels is drawn 1:1, on whole pixels: the image's hard edge
// stays a 1-2 pixel step at 1x, 1.25x and 2x, and the favicon is asked
// for at its drawn size.
func TestUnfurlThumbSharp(t *testing.T) {
	const og, fav = "http://img.test/og.png", "http://img.test/favicon.png"
	d := attDoc("https://example.test/pricing", slack.Attachment{ID: 1, ServiceName: "example.test", ServiceIcon: fav,
		Title: "Pricing", TitleLink: "https://example.test/pricing", Text: "Synthetic plans and prices.",
		ThumbURL: og, ThumbWidth: 1200, ThumbHeight: 630, FromURL: "https://example.test/pricing", OriginalURL: "https://example.test/pricing"})
	for _, scale := range []float32{1, 1.25, 2} {
		name := strconv.FormatFloat(float64(scale), 'f', -1, 32) + "x"
		ims := &scaledImages{src: map[string]*image.RGBA{og: edgeImage(1200, 630), fav: edgeImage(64, 64)},
			got: map[string]int{}, ops: map[[2]any]paint.ImageOp{}, dims: map[[2]any]image.Point{}}
		st := NewStyle(themeShaper(), ims, LightPalette())
		v := New(d, Options{})
		w, h := int(720*scale), int(220*scale)
		var ops op.Ops
		frame := func() {
			ops.Reset()
			paint.FillShape(&ops, st.Palette.Panel, clip.Rect{Max: image.Pt(w, h)}.Op())
			gtx := layout.Context{Ops: &ops, Metric: unit.Metric{PxPerDp: scale, PxPerSp: scale}, Constraints: layout.Exact(image.Pt(w, h))}
			v.Layout(gtx, st, &recHandler{})
		}
		var thumb, icon image.Rectangle
		for range 3 { // the first frame asks for the larger copy
			frame()
		}
		for _, im := range v.d.imgs {
			switch im.url {
			case og:
				thumb = im.r
			case fav:
				icon = im.r
			}
		}
		if thumb.Empty() {
			t.Fatalf("%s: no thumbnail laid out (imgs %+v)", name, v.d.imgs)
		}
		got := ims.dims[[2]any{og, ims.got[og]}]
		if got.X < thumb.Dx() || got.Y < thumb.Dy() {
			t.Errorf("%s: the thumb's copy is %v for a %v box (asked for %dpx): stretched", name, got, thumb.Size(), ims.got[og])
		}
		if got.Y > thumb.Dy()+1 {
			t.Errorf("%s: the thumb's copy is %v for a %v box: more than it covers", name, got, thumb.Size())
		}
		if !icon.Empty() && ims.got[fav] != max(icon.Dx(), icon.Dy()) {
			t.Errorf("%s: favicon asked for at %dpx, drawn %v", name, ims.got[fav], icon.Size())
		}
		img := pixeltest.Render(t, w, h, &ops)
		if dir := os.Getenv("EMBEDS_SHOTS_DIR"); dir != "" {
			if f, err := os.Create(filepath.Join(dir, "unfurl-thumb-"+name+".png")); err == nil {
				png.Encode(f, img)
				f.Close()
			}
		}
		// across the edge in the box's middle row: the pixels between the
		// two colors (10%..90% of the way) are the blur
		y := (thumb.Min.Y + thumb.Max.Y) / 2
		mid := 0
		for x := thumb.Min.X + 2; x < thumb.Max.X-2; x++ {
			r := img.RGBAAt(x, y).R
			if f := (float64(r) - 20) / 210; f > 0.1 && f < 0.9 {
				mid++
			}
		}
		if mid > 2 {
			t.Errorf("%s: the thumb's edge spans %d blended pixels, want at most 2 (sharp)", name, mid)
		}
	}
}
