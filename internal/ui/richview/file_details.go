package richview

import (
	"image"

	"github.com/some-du6e/clack/internal/richtext"
)

// detailsImage lays out image f as FileDetails' preview (Options.
// FileDetails) at (x, y) in width w and returns its height: 2bi5
// MessageImageFile with largeImage, EPKq FileImageThumbnail at maxSize
// 480 in a box of thumb_480_w/h, else thumb_360_w/h (f.Thumb is that
// thumb: the caller picks it), its .p-file_image_thumbnail__wrapper's 1px
// #1d1c1d21 border and radius 8 round it, and `.p-file_details__preview
// .p-file_image_thumbnail__image {max-width: 100%}` fits it to the
// content's width, never upscaled. With isFileDetails it draws no ALT
// badge (renderAltBadge) and with showActions off no FileActions. A click
// is the image's (Handler.OpenFile).
func (l *lay) detailsImage(f *richtext.File, x, y, w int) int {
	t := f.Thumb
	if t == nil || t.URL == "" || t.W <= 0 || t.H <= 0 || w <= 0 {
		return 0 // MessageImageFile draws nothing without the thumb's size
	}
	iw, ih := l.dp(float32(t.W)), l.dp(float32(t.H))
	if iw > w {
		ih = max(ih*w/iw, 1)
		iw = w
	}
	r := image.Rect(x, y, x+iw, y+ih)
	p := &l.st.Palette
	tg := l.d.addTarget(target{kind: tFile, file: f})
	rad := l.dp(8)
	src := l.fileStill(f) // animations off: the deanimated still (animated.go)
	l.d.imgs = append(l.d.imgs, imgPrim{r: r, url: src.URL, radii: [4]int{rad, rad, rad, rad}, target: tg, px: max(iw, ih)})
	l.fileControls(f, len(l.d.imgs)-1, r)
	l.d.hits = append(l.d.hits, hit{r: r, t: tg})
	l.d.boxes = append(l.d.boxes, box{kind: bBorder, r: r, radius: rad, stroke: p.Border, bw: max(l.dp(1), 1), target: -1, layer: 1})
	l.plainAdd(fileName(f))
	return ih
}
