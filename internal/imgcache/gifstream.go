package imgcache

// A GIF decoded one frame at a time (an animated WebP too: webpanim.go).
// image/gif's DecodeAll holds every frame's pixels at once (a 480×270 GIF
// of 100 frames is 13 MB of indices before a single frame is composed),
// so an animation here keeps its encoded bytes and an index of where each
// frame's LZW data starts, and composes the next frame on its canvas when
// it's due, as browsers do.
//
// Composition follows Chromium (and Firefox): a frame's transparent
// pixels leave the canvas as it is, DisposalBackground clears the frame's
// rectangle to transparent (not the background colour), DisposalPrevious
// puts back what was under the frame before it was drawn, each pass
// starts from an empty canvas, and a frame delay of 10 ms or less plays
// at 100 ms. A frame reaching past the logical screen is clipped to it, a
// colour index past the colour table is transparent, and a frame whose
// data ends early keeps what decoded.
//
// What an index and a decoder allocate is charged to a Budget first
// (decmem.go): the index's frames and colour tables as it grows, the
// decoder's canvas, the copy under a DisposalPrevious frame, its row and
// LZW tables, an animated WebP frame's picture (released once it's drawn),
// and the frame scaler's halved canvases. A charge the decode pool can't
// cover now (ErrBusy) fails before anything changes, so the same call can
// be made again later.

import (
	"compress/lzw"
	"errors"
	"image"
	"image/gif"
	"io"
	"log"
	"time"
	"unsafe"

	xdraw "golang.org/x/image/draw"

	"github.com/some-du6e/clack/internal/imgcache/internal/budget"
)

var errBadGIF = errors.New("imgcache: bad GIF")

// maxFrames is the most frames a GIF's index lists: a frame takes ~80
// bytes of index, and a 32 MB file of 1-pixel frames would otherwise
// list millions. Past it the animation plays the first maxFrames and the
// cut is logged (at 100 ms a frame that's over 8 minutes of animation).
const maxFrames = 5000

// gifIndex is a GIF's structure, read without decoding any pixels.
type gifIndex struct {
	w, h   int // the logical screen
	frames []gifFrame
	// loop is image/gif's LoopCount: 0 plays forever, -1 once (no
	// NETSCAPE2.0 block), n > 0 n+1 times (Chromium's repetition count)
	loop int
	// prev: some frame disposes to previous (the decoder keeps a copy of
	// what's under it)
	prev bool
	// webp: an animated WebP's frames (webpanim.go), not a GIF's
	webp bool
}

// grow makes room in ix.frames for one more frame, charging bg for a new
// array when it's full (doubling, as append would, up to maxFrames).
func (ix *gifIndex) grow(bg *budget.Budget) error {
	if len(ix.frames) < cap(ix.frames) {
		return nil
	}
	n := min(max(8, 2*cap(ix.frames)), maxFrames)
	if err := bg.Charge(int64(n) * int64(gifFrameBytes)); err != nil {
		return err
	}
	f := make([]gifFrame, len(ix.frames), n)
	copy(f, ix.frames)
	ix.frames = f
	return nil
}

// gifFrameBytes is a gifFrame, paletteBytes a gifPalette as allocated
// (1,032 bytes in Go's 1,152-byte size class).
const (
	gifFrameBytes = int(unsafe.Sizeof(gifFrame{}))
	paletteBytes  = 1152
)

// gifFrame is one image of a GIF: where it goes, how it's composed and
// where its LZW data starts.
type gifFrame struct {
	r     image.Rectangle // on the logical screen; may reach past it
	pal   *gifPalette
	trans int // the transparent colour index, -1 for none
	disp  byte
	inter bool // interlaced rows
	lit   int  // the LZW minimum code size
	data  int  // offset of the image data's first sub-block
	delay time.Duration
	// an animated WebP's frame (webpanim.go): data is where its ALPH and
	// VP8/VP8L chunks start and n their length; blend draws it over the
	// canvas (else it replaces its rectangle); disp is
	// gif.DisposalBackground for its dispose bit
	n     int
	blend bool
}

// gifPalette is a colour table as RGBA (GIF colours are opaque).
type gifPalette struct {
	n int
	c [256][4]byte
}

// readPalette reads a colour table of n colours, charging bg for it.
func (ix *gifIndex) readPalette(b []byte, n int, bg *budget.Budget) (*gifPalette, error) {
	if err := bg.Charge(paletteBytes); err != nil {
		return nil, err
	}
	p := &gifPalette{n: n}
	for i := 0; i < n; i++ {
		p.c[i] = [4]byte{b[3*i], b[3*i+1], b[3*i+2], 0xff}
	}
	return p, nil
}

// frameDelay is a frame's display time from its delay in centiseconds:
// browsers play 0–10 ms as 100 ms.
func frameDelay(cs int) time.Duration { return animDelay(time.Duration(cs) * 10 * time.Millisecond) }

// animDelay is a frame's display time for its stated duration (a GIF's
// or an animated WebP's): Chromium plays 10 ms or less as 100 ms.
func animDelay(d time.Duration) time.Duration {
	if d > 10*time.Millisecond {
		return d
	}
	return 100 * time.Millisecond
}

// skipBlocks returns the offset past the sub-blocks starting at p (past
// the zero-length terminator), or -1 when the data ends first.
func skipBlocks(b []byte, p int) int {
	for p < len(b) {
		n := int(b[p])
		p++
		if n == 0 {
			return p
		}
		p += n
	}
	return -1
}

// indexGIF reads b's logical screen, loop count and frames, charging bg
// for what the index holds as it grows (its frames and colour tables; a
// frame's own table only once the frame is kept). A file that breaks off
// after its first frame is the frames it has.
func indexGIF(b []byte, bg *budget.Budget) (*gifIndex, error) {
	if len(b) < 13 || string(b[:3]) != "GIF" {
		return nil, errBadGIF
	}
	ix := &gifIndex{w: int(b[6]) | int(b[7])<<8, h: int(b[8]) | int(b[9])<<8, loop: -1}
	p := 13
	var global *gifPalette
	if fl := b[10]; fl&0x80 != 0 {
		n := 1 << (fl&7 + 1)
		if p+3*n > len(b) {
			return nil, errBadGIF
		}
		var err error
		if global, err = ix.readPalette(b[p:], n, bg); err != nil {
			return nil, err
		}
		p += 3 * n
	}
	// the graphic control block in force for the next image
	trans, disp, cs := -1, byte(0), 0
	truncated := false
blocks:
	for p < len(b) {
		switch b[p] {
		case 0x21: // extension
			if p+2 >= len(b) {
				break blocks
			}
			label, q := b[p+1], p+2
			switch {
			case label == 0xf9 && q+4 < len(b) && b[q] >= 4: // graphic control
				pk := b[q+1]
				disp = pk >> 2 & 7
				cs = int(b[q+2]) | int(b[q+3])<<8
				trans = -1
				if pk&1 != 0 {
					trans = int(b[q+4])
				}
			case label == 0xff && q+12 < len(b) && b[q] == 11 &&
				(string(b[q+1:q+12]) == "NETSCAPE2.0" || string(b[q+1:q+12]) == "ANIMEXTS1.0"):
				if r := q + 12; r+3 < len(b) && b[r] >= 3 && b[r+1] == 1 {
					ix.loop = int(b[r+2]) | int(b[r+3])<<8 // 0: forever
				}
			}
			if p = skipBlocks(b, q); p < 0 {
				break blocks
			}
		case 0x2c: // image descriptor
			if p+10 > len(b) {
				break blocks
			}
			x, y := int(b[p+1])|int(b[p+2])<<8, int(b[p+3])|int(b[p+4])<<8
			fw, fh := int(b[p+5])|int(b[p+6])<<8, int(b[p+7])|int(b[p+8])<<8
			pk := b[p+9]
			p += 10
			pal, local, nLocal := global, -1, 0
			if pk&0x80 != 0 {
				nLocal = 1 << (pk&7 + 1)
				if p+3*nLocal > len(b) {
					break blocks
				}
				local = p // read once the frame is kept
				p += 3 * nLocal
			}
			if p >= len(b) {
				break blocks
			}
			lit := int(b[p])
			p++
			data := p
			if p = skipBlocks(b, p); p < 0 {
				p = len(b) // the last frame's data runs off the end: what's there decodes
			}
			if pal == nil && local < 0 || lit < 2 || lit > 8 || fw > MaxSide || fh > MaxSide || fw*fh > MaxPixels {
				break blocks // image/gif refuses these too
			}
			if fw > 0 && fh > 0 {
				if len(ix.frames) == maxFrames {
					truncated = true
					break blocks
				}
				if err := ix.grow(bg); err != nil {
					return nil, err
				}
				if local >= 0 {
					var err error
					if pal, err = ix.readPalette(b[local:], nLocal, bg); err != nil {
						return nil, err
					}
				}
				ix.frames = append(ix.frames, gifFrame{r: image.Rect(x, y, x+fw, y+fh), pal: pal, trans: trans,
					disp: disp, inter: pk&0x40 != 0, lit: lit, data: data, delay: frameDelay(cs)})
				ix.prev = ix.prev || disp == gif.DisposalPrevious
			}
			trans, disp, cs = -1, 0, 0 // a graphic control block applies to one image
		default: // the trailer (0x3b), or bytes that aren't a block
			break blocks
		}
	}
	if len(ix.frames) == 0 {
		return nil, errBadGIF
	}
	if ix.w <= 0 || ix.h <= 0 { // no logical screen: the first frame's extent (image/gif's Config)
		ix.w, ix.h = ix.frames[0].r.Max.X, ix.frames[0].r.Max.Y
	}
	if ix.w <= 0 || ix.h <= 0 || ix.w > MaxSide || ix.h > MaxSide || ix.w*ix.h > MaxPixels {
		return nil, ErrTooBig
	}
	if truncated {
		log.Printf("imgcache: a GIF of more than %d frames plays its first %d", maxFrames, maxFrames)
	}
	return ix, nil
}

// blockReader reads a frame's LZW data out of its sub-blocks.
type blockReader struct {
	b   []byte
	off int
	n   int // bytes left in the current sub-block
}

func (r *blockReader) ReadByte() (byte, error) {
	if r.n == 0 {
		if r.off >= len(r.b) {
			return 0, io.ErrUnexpectedEOF
		}
		r.n = int(r.b[r.off])
		if r.n == 0 {
			return 0, io.EOF // the terminator (left in place: EOF again)
		}
		r.off++
	}
	if r.off >= len(r.b) {
		r.n = 0
		return 0, io.ErrUnexpectedEOF
	}
	c := r.b[r.off]
	r.off++
	r.n--
	return c, nil
}

func (r *blockReader) Read(p []byte) (int, error) {
	for i := range p {
		c, err := r.ReadByte()
		if err != nil {
			if i > 0 {
				return i, nil
			}
			return 0, err
		}
		p[i] = c
	}
	return len(p), nil
}

// gifDecoder composes a GIF's frames in order on its canvas.
type gifDecoder struct {
	ix     *gifIndex
	data   []byte
	b      *budget.Budget // what its buffers charge (the owner's: a load, a player)
	canvas *image.RGBA
	save   []byte // under a DisposalPrevious frame, before it was drawn
	row    []byte
	br     blockReader
	lz     *lzw.Reader
	next   int // the frame compose draws next
	last   int // the frame on the canvas, -1 none
	// an animated WebP's (webpanim.go): the frame's chunks as a still
	// WebP of their own, and why the last frame drawn didn't decode
	wfile []byte
	werr  error
}

// newGIFDecoder makes a decoder of ix's frames, its canvas and later
// buffers charged to b.
func newGIFDecoder(ix *gifIndex, data []byte, b *budget.Budget) (*gifDecoder, error) {
	if err := b.Charge(rgbaBytes(image.Pt(ix.w, ix.h)) + decoderFixed); err != nil {
		return nil, err
	}
	return &gifDecoder{ix: ix, data: data, b: b, canvas: image.NewRGBA(image.Rect(0, 0, ix.w, ix.h)), last: -1}, nil
}

// decoderFixed is what a decoder allocates besides its buffers: itself and
// its canvas's header.
const decoderFixed = 1 << 10

// lzwBytes is an lzw.Reader (its 4K suffix, 8K prefix and 8K output
// tables).
const lzwBytes = 24 << 10

// decoderBytes is what a decoder of ix holds besides the encoded data:
// the canvas and the LZW tables, the rectangle under a DisposalPrevious
// frame, and what a WebP frame's decode allocates (webpFrameBytes: its
// picture is no bigger than the canvas).
func decoderBytes(ix *gifIndex) int {
	n := ix.w*ix.h*4 + 16<<10
	if ix.prev {
		n += ix.w * ix.h * 4
	}
	if ix.webp {
		n += webpFrameBytes(ix)
	}
	return n
}

// compose draws the next frame on the canvas, after the previous frame's
// disposal, and returns its index. After the last frame comes the first,
// on an empty canvas. A frame whose data is cut short keeps what decoded.
//
// What the frame needs is charged to d.b first: a buffer that doesn't fit
// fails compose before the canvas changes, the same frame still next.
func (d *gifDecoder) compose() (int, error) {
	i := d.next
	cr := d.canvas.Rect
	f := &d.ix.frames[i]
	if f.disp == gif.DisposalPrevious && d.save == nil {
		// the whole canvas's, once: every frame's rectangle fits in it
		n := 4 * cr.Dx() * cr.Dy()
		if err := d.b.Charge(int64(n)); err != nil {
			return i, err
		}
		d.save = make([]byte, n)
	}
	var wm image.Image // an animated WebP frame's picture, decoded first
	var wb *budget.Budget
	if d.ix.webp {
		var err error
		if wm, wb, err = d.decodeWebPFrame(f); budget.IsBusy(err) {
			return i, err
		}
		defer wb.Release() // the picture, once it's drawn
		d.werr = err
	} else if err := d.gifBuffers(f); err != nil {
		return i, err
	}
	if i == 0 {
		clear(d.canvas.Pix)
	} else if d.last >= 0 {
		pf := &d.ix.frames[d.last]
		switch r := pf.r.Intersect(cr); pf.disp {
		case gif.DisposalBackground:
			for y := r.Min.Y; y < r.Max.Y; y++ {
				o := d.canvas.PixOffset(r.Min.X, y)
				clear(d.canvas.Pix[o : o+4*r.Dx()])
			}
		case gif.DisposalPrevious:
			copyRect(d.canvas, r, d.save, false)
		}
	}
	if f.disp == gif.DisposalPrevious {
		r := f.r.Intersect(cr)
		copyRect(d.canvas, r, d.save[:4*r.Dx()*r.Dy()], true)
	}
	if d.ix.webp {
		d.drawWebP(f, wm)
	} else {
		d.draw(f)
	}
	d.last = i
	d.next = (i + 1) % len(d.ix.frames)
	return i, nil
}

// gifBuffers makes what draw reads a GIF frame with, charged to d.b once:
// the LZW tables and a row as wide as the widest frame.
func (d *gifDecoder) gifBuffers(f *gifFrame) error {
	if d.lz == nil {
		w := 0
		for i := range d.ix.frames {
			w = max(w, d.ix.frames[i].r.Dx())
		}
		if err := d.b.Charge(lzwBytes + int64(w)); err != nil {
			return err
		}
		d.br = blockReader{b: d.data, off: f.data}
		d.lz = lzw.NewReader(&d.br, lzw.LSB, f.lit).(*lzw.Reader)
		d.row = make([]byte, w)
	}
	return nil
}

// copyRect copies the canvas pixels under r into buf (out) or back.
func copyRect(m *image.RGBA, r image.Rectangle, buf []byte, out bool) {
	w := 4 * r.Dx()
	for y, k := r.Min.Y, 0; y < r.Max.Y && k+w <= len(buf); y, k = y+1, k+w {
		o := m.PixOffset(r.Min.X, y)
		if out {
			copy(buf[k:k+w], m.Pix[o:o+w])
		} else {
			copy(m.Pix[o:o+w], buf[k:k+w])
		}
	}
}

// draw decodes frame f's indices a row at a time onto the canvas (its
// LZW reader and row made already: gifBuffers).
func (d *gifDecoder) draw(f *gifFrame) {
	d.br = blockReader{b: d.data, off: f.data}
	d.lz.Reset(&d.br, lzw.LSB, f.lit)
	fw, fh := f.r.Dx(), f.r.Dy()
	row := d.row[:fw]
	cr := d.canvas.Rect
	x0, x1 := max(f.r.Min.X, cr.Min.X), min(f.r.Max.X, cr.Max.X)
	pal, trans := f.pal, f.trans
	for j := 0; j < fh; j++ {
		if _, err := io.ReadFull(d.lz, row); err != nil {
			return // what decoded stays (browsers show a partial frame)
		}
		y := f.r.Min.Y + j
		if f.inter {
			y = f.r.Min.Y + interlacedRow(j, fh)
		}
		if y < cr.Min.Y || y >= cr.Max.Y || x0 >= x1 {
			continue
		}
		px := d.canvas.Pix[d.canvas.PixOffset(x0, y):]
		for k, c := range row[x0-f.r.Min.X : x1-f.r.Min.X] {
			if int(c) == trans || int(c) >= pal.n {
				continue
			}
			*(*[4]byte)(px[4*k : 4*k+4]) = pal.c[c]
		}
	}
}

// interlacedRow is the row of the j-th row stored in an interlaced
// image of h rows: every 8th from 0, every 8th from 4, every 4th from 2,
// then every 2nd from 1.
func interlacedRow(j, h int) int {
	for _, p := range [4][2]int{{0, 8}, {4, 8}, {2, 4}, {1, 2}} { // first row, step
		n := (h - p[0] + p[1] - 1) / p[1]
		if j < n {
			return p[0] + p[1]*j
		}
		j -= n
	}
	return 0
}

// scaledSize is a w×h image's size with its larger side at most max (0:
// 2048), as Scale makes it.
func scaledSize(w, h, max int) image.Point {
	if max <= 0 {
		max = 2048
	}
	if w > max || h > max {
		if w >= h {
			h, w = max*h/w, max
		} else {
			w, h = max*w/h, max
		}
	}
	return image.Pt(maxInt(w, 1), maxInt(h, 1))
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// frameScaler scales a GIF's canvas to the size it's drawn at, fast
// enough to run for every frame: halved by averaging 2×2 blocks while
// it's twice the size or more, then bilinear. Sampling plain bilinear
// from a much larger canvas would alias (and shimmer as it plays); the
// kernel scalers Scale uses for stills cost several times more a frame.
// A GIF's still goes through it too, so the first frame of the
// animation is the still exactly.
type frameScaler struct{ tmp [2]*image.RGBA }

// scale draws src (the whole of it) into dst (the whole of it), the
// halved canvases it makes charged to b first (it draws nothing when one
// doesn't fit).
func (s *frameScaler) scale(dst, src *image.RGBA, b *budget.Budget) error {
	cur := src
	for i := 0; cur.Rect.Dx() >= 2*dst.Rect.Dx() && cur.Rect.Dy() >= 2*dst.Rect.Dy(); i++ {
		t, err := s.buf(i, cur.Rect.Dx()/2, cur.Rect.Dy()/2, b)
		if err != nil {
			return err
		}
		halve(t, cur)
		cur = t
	}
	if cur.Rect.Size() == dst.Rect.Size() && cur.Stride == dst.Stride && cur.Rect.Min == (image.Point{}) && dst.Rect.Min == (image.Point{}) {
		copy(dst.Pix, cur.Pix)
		return nil
	}
	xdraw.ApproxBiLinear.Scale(dst, dst.Rect, cur, cur.Rect, xdraw.Src, nil)
	return nil
}

// prepare makes the halved canvases scale needs to draw a canvas of size
// src into a frame of size dst, charged to b, so that scale makes none.
func (s *frameScaler) prepare(src, dst image.Point, b *budget.Budget) error {
	w, h := src.X, src.Y
	for i := 0; w >= 2*dst.X && h >= 2*dst.Y; i++ {
		w, h = w/2, h/2
		if _, err := s.buf(i, w, h, b); err != nil {
			return err
		}
	}
	return nil
}

// buf is the scaler's i-th halved canvas, w×h: one it has, or a new one
// charged to b when it has none that big.
func (s *frameScaler) buf(i, w, h int, b *budget.Budget) (*image.RGBA, error) {
	t := s.tmp[i%2]
	if t == nil || cap(t.Pix) < 4*w*h {
		if err := b.Charge(rgbaBytes(image.Pt(w, h))); err != nil {
			return nil, err
		}
		t = image.NewRGBA(image.Rect(0, 0, w, h))
		s.tmp[i%2] = t
	} else {
		t.Pix, t.Stride, t.Rect = t.Pix[:4*w*h], 4*w, image.Rect(0, 0, w, h)
	}
	return t, nil
}

// bytes is what the scaler's buffers hold for a w×h canvas (at most a
// half and a quarter of it).
func scalerBytes(w, h int) int { return w * h * 5 / 4 }

// halve sets each of dst's pixels (premultiplied) to the average of the
// 2×2 block of src under it.
func halve(dst, src *image.RGBA) {
	w, h := dst.Rect.Dx(), dst.Rect.Dy()
	for y := 0; y < h; y++ {
		a := src.Pix[src.PixOffset(src.Rect.Min.X, src.Rect.Min.Y+2*y):]
		b := src.Pix[src.PixOffset(src.Rect.Min.X, src.Rect.Min.Y+2*y+1):]
		o := dst.Pix[dst.PixOffset(dst.Rect.Min.X, dst.Rect.Min.Y+y):]
		for x := 0; x < w; x++ {
			i, k := 8*x, 4*x
			for c := 0; c < 4; c++ {
				o[k+c] = byte((uint(a[i+c]) + uint(a[i+4+c]) + uint(b[i+c]) + uint(b[i+4+c]) + 2) >> 2)
			}
		}
	}
}
