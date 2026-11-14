package imgcache

// A GIF decoded one frame at a time. image/gif's DecodeAll holds every
// frame's pixels at once (a 480×270 GIF of 100 frames is 13 MB of indices
// before a single frame is composed), so an animation here keeps its
// encoded bytes and an index of where each frame's LZW data starts, and
// composes the next frame on its canvas when it's due, as browsers do.
//
// Composition follows Chromium (and Firefox): a frame's transparent
// pixels leave the canvas as it is, DisposalBackground clears the frame's
// rectangle to transparent (not the background colour), DisposalPrevious
// puts back what was under the frame before it was drawn, each pass
// starts from an empty canvas, and a frame delay of 10 ms or less plays
// at 100 ms. A frame reaching past the logical screen is clipped to it, a
// colour index past the colour table is transparent, and a frame whose
// data ends early keeps what decoded.

import (
	"compress/lzw"
	"errors"
	"image"
	"image/gif"
	"io"
	"log"
	"time"

	xdraw "golang.org/x/image/draw"
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
}

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
}

// gifPalette is a colour table as RGBA (GIF colours are opaque).
type gifPalette struct {
	n int
	c [256][4]byte
}

func readPalette(b []byte, n int) *gifPalette {
	p := &gifPalette{n: n}
	for i := 0; i < n; i++ {
		p.c[i] = [4]byte{b[3*i], b[3*i+1], b[3*i+2], 0xff}
	}
	return p
}

// frameDelay is a frame's display time from its delay in centiseconds:
// browsers play 0–10 ms as 100 ms.
func frameDelay(cs int) time.Duration {
	if d := time.Duration(cs) * 10 * time.Millisecond; d > 10*time.Millisecond {
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

// indexGIF reads b's logical screen, loop count and frames. A file that
// breaks off after its first frame is the frames it has.
func indexGIF(b []byte) (*gifIndex, error) {
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
		global = readPalette(b[p:], n)
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
			pal := global
			if pk&0x80 != 0 {
				n := 1 << (pk&7 + 1)
				if p+3*n > len(b) {
					break blocks
				}
				pal = readPalette(b[p:], n)
				p += 3 * n
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
			if pal == nil || lit < 2 || lit > 8 || fw > MaxSide || fh > MaxSide || fw*fh > MaxPixels {
				break blocks // image/gif refuses these too
			}
			if fw > 0 && fh > 0 {
				if len(ix.frames) == maxFrames {
					truncated = true
					break blocks
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
	canvas *image.RGBA
	save   []byte // under a DisposalPrevious frame, before it was drawn
	row    []byte
	br     blockReader
	lz     *lzw.Reader
	next   int // the frame compose draws next
	last   int // the frame on the canvas, -1 none
}

func newGIFDecoder(ix *gifIndex, data []byte) *gifDecoder {
	return &gifDecoder{ix: ix, data: data, canvas: image.NewRGBA(image.Rect(0, 0, ix.w, ix.h)), last: -1}
}

// decoderBytes is what a decoder of ix holds besides the encoded data.
func decoderBytes(ix *gifIndex) int {
	n := ix.w*ix.h*4 + 16<<10 // the canvas, the LZW tables
	if ix.prev {
		n += ix.w * ix.h * 4
	}
	return n
}

// compose draws the next frame on the canvas, after the previous frame's
// disposal, and returns its index. After the last frame comes the first,
// on an empty canvas. A frame whose data is cut short keeps what decoded.
func (d *gifDecoder) compose() int {
	i := d.next
	cr := d.canvas.Rect
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
	f := &d.ix.frames[i]
	if f.disp == gif.DisposalPrevious {
		r := f.r.Intersect(cr)
		if n := 4 * r.Dx() * r.Dy(); cap(d.save) < n {
			d.save = make([]byte, n)
		} else {
			d.save = d.save[:n]
		}
		copyRect(d.canvas, r, d.save, true)
	}
	d.draw(f)
	d.last = i
	d.next = (i + 1) % len(d.ix.frames)
	return i
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

// draw decodes frame f's indices a row at a time onto the canvas.
func (d *gifDecoder) draw(f *gifFrame) {
	d.br = blockReader{b: d.data, off: f.data}
	if d.lz == nil {
		d.lz = lzw.NewReader(&d.br, lzw.LSB, f.lit).(*lzw.Reader)
	} else {
		d.lz.Reset(&d.br, lzw.LSB, f.lit)
	}
	fw, fh := f.r.Dx(), f.r.Dy()
	if cap(d.row) < fw {
		d.row = make([]byte, fw)
	}
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

// scale draws src (the whole of it) into dst (the whole of it).
func (s *frameScaler) scale(dst, src *image.RGBA) {
	cur := src
	for i := 0; cur.Rect.Dx() >= 2*dst.Rect.Dx() && cur.Rect.Dy() >= 2*dst.Rect.Dy(); i++ {
		w, h := cur.Rect.Dx()/2, cur.Rect.Dy()/2
		t := s.tmp[i%2]
		if t == nil || cap(t.Pix) < 4*w*h {
			t = image.NewRGBA(image.Rect(0, 0, w, h))
			s.tmp[i%2] = t
		} else {
			t.Pix, t.Stride, t.Rect = t.Pix[:4*w*h], 4*w, image.Rect(0, 0, w, h)
		}
		halve(t, cur)
		cur = t
	}
	if cur.Rect.Size() == dst.Rect.Size() && cur.Stride == dst.Stride && cur.Rect.Min == (image.Point{}) && dst.Rect.Min == (image.Point{}) {
		copy(dst.Pix, cur.Pix)
		return
	}
	xdraw.ApproxBiLinear.Scale(dst, dst.Rect, cur, cur.Rect, xdraw.Src, nil)
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
