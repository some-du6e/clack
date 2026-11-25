package views

import (
	"image"
	"image/color"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// A playable video's tile before its frame loads is a square in the dark
// theme: FileEntityThumbnailIcon's img-border ring (rgba(sk_foreground_low,
// .13)) and the --video container's 4% rgba(sk_foreground_min) wash show
// it on the row, around the 20% black overlay and the white play-filled
// (CDN build bv1-13, boot-shared AZy2 and PQXV). The overlay alone was
// all but invisible there: the tile's edge as dark as its inside.
func TestFileTileVideoSquareDark(t *testing.T) {
	th := theme.New(true)
	f := &Files{base: base{th: th}}
	const s, m = 36, 4
	var ops op.Ops
	paint.Fill(&ops, th.C.Bg) // the row's surface
	gtx := layout.Context{Ops: &ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Exact(image.Pt(s, s)), Now: time.Now()}
	st := op.Offset(image.Pt(m, m)).Push(&ops)
	f.fileTile(gtx, &fileItem{core.File{Name: "demo.mp4", Filetype: "mp4", Mime: "video/mp4", IsVideo: true}}, s)
	st.Pop()
	img := pixeltest.Render(t, s+2*m, s+2*m, &ops)
	sum := func(x, y int) int { c := img.RGBAAt(x, y); return int(c.R) + int(c.G) + int(c.B) }
	row, edge, inside := sum(1, 1), sum(m, m+s/2), sum(m+4, m+8)
	if edge < inside+30 || edge < row+30 {
		t.Errorf("the tile's edge (RGB sum %d) doesn't show against its inside (%d) or the row (%d): no img-border ring", edge, inside, row)
	}
	if n := pixeltest.Count(img, image.Rect(m+8, m+8, m+28, m+28), color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}, 40); n < 20 {
		t.Errorf("%d white pixels in the tile's middle, want the play-filled glyph", n)
	}
}
