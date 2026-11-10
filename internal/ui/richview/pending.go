package richview

import (
	"image"
	"image/color"
	"math"
	"time"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"

	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
)

// The viewer's optimistic message while its files upload (Slack's
// appendPlaceholderMessage with pendingFiles, drawn by PendingFilesAtom):
//
//	PendingFilesMetaTextWrapper  c-message_kit__file__meta: 13px at
//	                             foreground_max .7, 4px under it
//	PendingFilesGalleryGridImage the File's own bytes; an upload still in
//	                             flight at .5 (c-pending_files__thumbnail--pending)
//	PendingFilesSpinner          InfiniteSpinner jumbo (52px), white, in
//	                             the tile's centre (c-pending_files__spinner--center)

// fileStatus lays out the meta line above a placeholder's files.
func (l *lay) fileStatus(b *richtext.Block, x, y, w int) int {
	// the same .c-message_kit__file__meta line as the title line that
	// replaces it (filemeta.go): no jump at the swap
	fs := flowStyle{face: font.Font{Typeface: l.st.Face}, size: l.sp(13), lineH: l.dp(fileMetaLineH), col: l.st.Palette.Text2,
		emoji: l.dp(16), maxLines: 1, noSelect: true}
	h, _ := l.textFlow(b.Spans, x, y, w, &fs)
	return h + l.dp(4)
}

// fade halves the opacity of the boxes and text added since nb, nf.
func (l *lay) fade(nb, nf int) {
	half := func(c *color.NRGBA) { c.A = uint8(float32(c.A)*.5 + .5) }
	for i := nb; i < len(l.d.boxes); i++ {
		b := &l.d.boxes[i]
		half(&b.fill)
		half(&b.stroke)
	}
	for i := nf; i < len(l.d.frags); i++ {
		half(&l.d.frags[i].col)
	}
}

// spinPrim is an InfiniteSpinner drawn every frame.
type spinPrim struct {
	r     image.Rectangle
	white bool // color="white" (else blue)
}

// pendingTile marks a gallery tile of a file still uploading: its image
// at .5 and the centred jumbo spinner.
func (l *lay) pendingTile(f *richtext.File, r image.Rectangle, img int) {
	if !f.Pending {
		return
	}
	if img >= 0 && img < len(l.d.imgs) {
		l.d.imgs[img].alpha = 0.5
	}
	d := l.dp(52)
	c := r.Min.Add(r.Max).Div(2)
	l.d.spinners = append(l.d.spinners, spinPrim{r: image.Rect(c.X-d/2, c.Y-d/2, c.X-d/2+d, c.Y-d/2+d), white: true})
}

// draw paints Slack's InfiniteSpinner (s6So): in a 100-unit box, a
// stroke-8 track at .25 of content-ter and two 55-unit arcs (dashoffset
// 90, the ring turned -45°) rotating once per 1.2s on the ring's and the
// tail's cubic-beziers.
func (s *spinPrim) draw(gtx layout.Context, st *Style) {
	d := s.r.Dx()
	if d <= 0 {
		return
	}
	defer op.Offset(s.r.Min).Push(gtx.Ops).Pop()
	sc := float32(d) / 100
	r := 35 * sc
	sw := 8 * sc
	ctr := f32.Pt(float32(d)/2, float32(d)/2)
	arc := func(a0, sweep float32, c color.NRGBA) {
		var p clip.Path
		p.Begin(gtx.Ops)
		p.MoveTo(f32.Pt(ctr.X+r*float32(math.Cos(float64(a0))), ctr.Y+r*float32(math.Sin(float64(a0)))))
		p.ArcTo(ctr, ctr, sweep)
		paint.FillShape(gtx.Ops, c, clip.Stroke{Path: p.End(), Width: sw}.Op())
	}
	track := st.Palette.Text2
	track.A = uint8(float32(track.A)*.25 + .5)
	arc(0, 2*math.Pi-0.001, track)
	col := st.Palette.Link
	if s.white {
		col = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	}
	t := float64(gtx.Now.UnixMilli()%1200) / 1200
	dash := float32(55.0 / 219.9 * 2 * math.Pi)
	base := float32(-90.0/219.9*2*math.Pi) - float32(math.Pi/4)
	arc(base+float32(2*math.Pi*bezierAt(.25, .29, .54, .86, t)), dash, col)
	arc(base+float32(2*math.Pi*bezierAt(.41, .24, .64, .69, t)), dash, col)
	gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(33 * time.Millisecond)})
}

// bezierAt is CSS cubic-bezier(x1, y1, x2, y2) at progress x.
func bezierAt(x1, y1, x2, y2, x float64) float64 {
	lo, hi := 0.0, 1.0
	t := x
	for range 30 {
		t = (lo + hi) / 2
		mt := 1 - t
		bx := 3*mt*mt*t*x1 + 3*mt*t*t*x2 + t*t*t
		if bx < x {
			lo = t
		} else {
			hi = t
		}
	}
	mt := 1 - t
	return 3*mt*mt*t*y1 + 3*mt*t*t*y2 + t*t*t
}
