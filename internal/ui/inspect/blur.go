package inspect

import (
	"image"
	"runtime"
	"sync"
)

// The blurs of the review and the copy (the margin around an element, the
// rects the user marked). A box blur rad px each way, three times, rows
// then columns, edges clamped to the rectangle: exactly the arithmetic the
// tool always used, (sum + d/2) / d per channel and pass, so every pixel
// comes out as it did, bit for bit (blur_test.go holds the old code). It
// was the review's wait: a 3200x2000 px shot blurred whole took the
// review's "preparing the shot…" a second and more. Now:
//
//   - the division is a table's lookup, the four channels go together,
//     and no closure runs per pixel;
//   - a shot's margin (blurOutside) runs only the passes the margin's
//     pixels need: whole rows and columns within 3·rad of the margin's
//     inner edge, and in the rows and columns across the element only
//     their spans in the margin, 2·rad, rad and 0 px wider for passes 1,
//     2 and 3 (a pass's pixel reads rad px each way of the last pass);
//   - columns go in blocks of 16 (one cache line of each row), and rows
//     and blocks are shared out over the CPUs.

// blur box-blurs img inside r three times, rad px each way, rows then
// columns (edges clamped: nothing outside r comes in).
func blur(img *image.RGBA, r image.Rectangle, rad int) {
	blurOutside(img, r, rad, image.Rectangle{})
}

// blurOutside is blur for the pixels of r outside keep (img coordinates):
// those come out exactly as blur makes them. The pixels inside keep are
// left as anything: the caller draws over them (paintShot draws the
// element there sharp).
func blurOutside(img *image.RGBA, r image.Rectangle, rad int, keep image.Rectangle) {
	r = r.Intersect(img.Bounds())
	w, h := r.Dx(), r.Dy()
	if w == 0 || h == 0 || rad <= 0 {
		return
	}
	// the margins around keep, in r's own coordinates; none kept: all of r
	ml, mr, mt, mb := w, 0, h, 0
	if k := keep.Intersect(r).Sub(r.Min); !k.Empty() {
		ml, mr, mt, mb = k.Min.X, w-k.Max.X, k.Min.Y, h-k.Max.Y
	}
	d := 2*rad + 1
	q := make([]uint8, 255*d+1) // (sum + d/2) / d
	for s := range q {
		q[s] = uint8((s + d/2) / d)
	}
	b := blurJob{img: img, r: r, rad: rad, q: q, ml: ml, mr: mr, mt: mt, mb: mb}
	// rows: whole ones near the top and bottom margins (the columns
	// across keep read them), the margins' spans of the others
	b.parallel(h, false, func(bufs *lineBufs, y0, y1 int) {
		for y := y0; y < y1; y++ {
			left, right := w, w // the whole row
			if y >= mt+3*rad && y < h-mb-3*rad {
				left, right = ml, w-mr
			}
			b.row(bufs, y, left, right)
		}
	})
	// columns, in blocks of colBlock: whole ones in the left and right
	// margins, the top and bottom margins' spans of the others
	blocks := (w + colBlock - 1) / colBlock
	b.parallel(blocks, true, func(bufs *lineBufs, k0, k1 int) {
		for k := k0; k < k1; k++ {
			b.columns(bufs, k*colBlock, min((k+1)*colBlock, w))
		}
	})
}

// colBlock is how many columns go together: 64 bytes of each row.
const colBlock = 16

type blurJob struct {
	img            *image.RGBA
	r              image.Rectangle
	rad            int
	q              []uint8
	ml, mr, mt, mb int // the margins (r's coordinates); ml w: no keep
}

// lineBufs are one goroutine's lines: the passes' and a block's columns.
type lineBufs struct {
	t1, t2 []uint8
	cols   [colBlock][]uint8
	outs   [colBlock][]uint8
}

// grow makes l's lines n px or more: the passes' (both phases), and a
// block's columns too when cols (the columns' phase; the rows' reads none).
func (l *lineBufs) grow(n int, cols bool) {
	if cap(l.t1) < 4*n {
		l.t1, l.t2 = make([]uint8, 4*n), make([]uint8, 4*n)
	}
	if cols && cap(l.cols[0]) < 4*n {
		for i := range l.cols {
			l.cols[i], l.outs[i] = make([]uint8, 4*n), make([]uint8, 4*n)
		}
	}
}

// lines keeps the goroutines' lines from blur to blur (the review's image,
// then each redraw after a mark, blur a shot-sized image; a 3200 px
// side's lines are 435 KB a goroutine).
var lines = sync.Pool{New: func() any { return new(lineBufs) }}

// parallel runs f over [0, n) in contiguous parts, one per CPU (at most
// 8), each with its own lines (cols: a block's columns too); small jobs on
// the calling goroutine.
func (b *blurJob) parallel(n int, cols bool, f func(bufs *lineBufs, i0, i1 int)) {
	long := max(b.r.Dx(), b.r.Dy())
	work := b.r.Dx() * b.r.Dy()
	parts := min(runtime.GOMAXPROCS(0), 8, n)
	run := func(i0, i1 int) {
		bufs := lines.Get().(*lineBufs)
		bufs.grow(long, cols)
		f(bufs, i0, i1)
		lines.Put(bufs)
	}
	if work < 1<<16 || parts <= 1 {
		run(0, n)
		return
	}
	var wg sync.WaitGroup
	for p := range parts {
		i0, i1 := n*p/parts, n*(p+1)/parts
		wg.Add(1)
		go func() {
			defer wg.Done()
			run(i0, i1)
		}()
	}
	wg.Wait()
}

// row blurs row y (r's coordinates) in place: the result where it's
// wanted, [0, left) and [right, w) (left >= right: all of it).
func (b *blurJob) row(bufs *lineBufs, y, left, right int) {
	w := b.r.Dx()
	o := b.img.PixOffset(b.r.Min.X, b.r.Min.Y+y)
	line := b.img.Pix[o : o+4*w : o+4*w]
	box3(line, line, bufs.t1, bufs.t2, w, b.rad, left, right, b.q)
}

// columns blurs columns [x0, x1) (r's coordinates) in place: whole ones in
// the left and right margins, the spans in the top and bottom margins of
// the others.
func (b *blurJob) columns(bufs *lineBufs, x0, x1 int) {
	w, h := b.r.Dx(), b.r.Dy()
	img, stride := b.img, b.img.Stride
	base := img.PixOffset(b.r.Min.X, b.r.Min.Y)
	n := x1 - x0
	// the rows each pass reads: all of them when one column of the block
	// is a margin's, else the top and bottom margins' with the passes'
	// reach (3·rad)
	whole := x0 < b.ml || x1 > w-b.mr
	left, right := b.mt, h-b.mb
	if whole {
		left, right = h, h
	}
	readL, readR := h, h
	if left < right {
		readL, readR = min(h, left+3*b.rad), max(0, right-3*b.rad)
	}
	gather := func(y int) {
		o := base + y*stride + 4*x0
		for c := range n {
			copy(bufs.cols[c][4*y:4*y+4], img.Pix[o+4*c:o+4*c+4])
		}
	}
	if readL >= readR {
		for y := range h {
			gather(y)
		}
	} else {
		for y := range readL {
			gather(y)
		}
		for y := readR; y < h; y++ {
			gather(y)
		}
	}
	for c := range n {
		x := x0 + c
		cl, cr := left, right
		if x < b.ml || x >= w-b.mr {
			cl, cr = h, h // a margin's column: all of it
		}
		box3(bufs.outs[c], bufs.cols[c], bufs.t1, bufs.t2, h, b.rad, cl, cr, b.q)
	}
	scatter := func(y int) {
		o := base + y*stride + 4*x0
		for c := range n {
			copy(img.Pix[o+4*c:o+4*c+4], bufs.outs[c][4*y:4*y+4])
		}
	}
	if left >= right {
		for y := range h {
			scatter(y)
		}
	} else {
		for y := range left {
			scatter(y)
		}
		for y := right; y < h; y++ {
			scatter(y)
		}
	}
}

// box3 runs the three passes over a line of n px (4 bytes each) from src
// into dst (they may be the same slice: src is read whole before dst is
// written), through t1 and t2: dst gets the result at [0, left) and
// [right, n), all of it when left >= right. Pass k computes its result
// where pass k+1 reads it: 2·rad and rad px past those spans for passes
// 1 and 2.
func box3(dst, src, t1, t2 []uint8, n, rad, left, right int, q []uint8) {
	if left >= right {
		pass(t1, src, n, rad, 0, n, q)
		pass(t2, t1, n, rad, 0, n, q)
		pass(dst, t2, n, rad, 0, n, q)
		return
	}
	spans := func(dst, src []uint8, grow int) {
		l, r := min(n, left+grow), max(0, right-grow)
		if l >= r {
			pass(dst, src, n, rad, 0, n, q)
			return
		}
		pass(dst, src, n, rad, 0, l, q)
		pass(dst, src, n, rad, r, n, q)
	}
	spans(t1, src, 2*rad)
	spans(t2, t1, rad)
	spans(dst, t2, 0)
}

// pass blurs src into dst once at [lo, hi): each px the rounded mean of the
// 2·rad+1 px around it in src (indices clamped to the line's n px), q the
// table of (sum + d/2) / d.
func pass(dst, src []uint8, n, rad, lo, hi int, q []uint8) {
	if lo >= hi {
		return
	}
	last := n - 1
	var s0, s1, s2, s3 int
	for j := lo - rad; j <= lo+rad; j++ {
		k := min(max(j, 0), last)
		o := 4 * k
		s0 += int(src[o])
		s1 += int(src[o+1])
		s2 += int(src[o+2])
		s3 += int(src[o+3])
	}
	for i := lo; i < hi; i++ {
		o := 4 * i
		dst[o], dst[o+1], dst[o+2], dst[o+3] = q[s0], q[s1], q[s2], q[s3]
		a := 4 * min(i+rad+1, last)
		z := 4 * max(i-rad, 0)
		s0 += int(src[a]) - int(src[z])
		s1 += int(src[a+1]) - int(src[z+1])
		s2 += int(src[a+2]) - int(src[z+2])
		s3 += int(src[a+3]) - int(src[z+3])
	}
}
