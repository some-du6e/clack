package richview

import (
	"bytes"
	_ "embed"
	"image"
	"image/png"
	"sync"

	"gioui.org/f32"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
)

// A message emoji that failed to load. Slack's rich-text Emoji doesn't
// set shouldFallbackOnLoadError, so the <img alt=":name:"> stays broken,
// and Chromium draws its fallback (html_image_fallback_helper.cc). An img
// with an alt attribute in a standards-mode document isn't treated as
// replaced: its content is the broken image icon (IDR_BROKENIMAGE, an
// <img width=16 height=16>, floated to the start) followed by the alt
// text. In the emoji's 22px box (.c-emoji: inline-flex, overflow hidden;
// .c-emoji--inline img: overflow hidden) the icon sits at the top left
// and the text, which doesn't fit beside it, wraps under it, out of the
// box. The icons are Chromium's, default_100_percent and
// default_200_percent (brokenimage/LICENSE).

//go:embed brokenimage/broken_image_100.png
var brokenPNG1x []byte

//go:embed brokenimage/broken_image_200.png
var brokenPNG2x []byte

var brokenOps struct {
	once sync.Once
	op   [2]paint.ImageOp
	ok   [2]bool
}

func brokenIcon(pxPerDp float32) (paint.ImageOp, bool) {
	brokenOps.once.Do(func() {
		for i, b := range [][]byte{brokenPNG1x, brokenPNG2x} {
			if img, err := png.Decode(bytes.NewReader(b)); err == nil {
				brokenOps.op[i], brokenOps.ok[i] = paint.NewImageOp(img), true
			}
		}
	})
	i := 0
	if pxPerDp >= 1.5 { // Chromium's scale factor picks the 2x bitmap
		i = 1
	}
	return brokenOps.op[i], brokenOps.ok[i]
}

// paintBrokenEmoji draws the broken image icon for an emoji box at
// (x, y), size px across, at 16 CSS px.
func paintBrokenEmoji(ops *op.Ops, x, y, size int, pxPerDp float32) {
	img, ok := brokenIcon(pxPerDp)
	if !ok || size <= 0 {
		return
	}
	box := image.Rect(x, y, x+size, y+size)
	s := int(16*pxPerDp + 0.5)
	sz := img.Size()
	if sz.X <= 0 || sz.Y <= 0 {
		return
	}
	// the icon's img is width=16 height=16: object-fit fill stretches the
	// 14x16 bitmap (28x32 at 2x) to the square
	cl := clip.Rect(box).Push(ops)
	t := op.Affine(f32.AffineId().Scale(f32.Point{}, f32.Pt(float32(s)/float32(sz.X), float32(s)/float32(sz.Y))).Offset(f32.Pt(float32(x), float32(y)))).Push(ops)
	img.Add(ops)
	paint.PaintOp{}.Add(ops)
	t.Pop()
	cl.Pop()
}
