package imgcache

// Animated WebP, played like a GIF (gifstream.go, anim.go). Slack serves
// some custom emoji as animated WebP behind a .png URL (:explodes:), and
// x/image/webp decodes only still WebP: the ANIM/ANMF chunks are skipped,
// the file ends without an image, and the decode fails. Here the RIFF
// container is indexed as a GIF is, without decoding any pixels: the
// canvas, the loop count and each ANMF frame's rectangle, duration,
// blending, disposal and where its ALPH and VP8/VP8L chunks are. A
// frame's chunks decode through x/image/webp as a still WebP of their
// own when the frame is due.
//
// Composition follows Chromium's WEBPImageDecoder: every pass starts
// from a transparent canvas; a frame is alpha-blended over the canvas, or
// with its no-blend bit replaces its rectangle; dispose-to-background
// clears the rectangle to transparent (the ANIM background colour is
// ignored, as Chromium ignores it); the loop count is the number of
// passes (0: forever); and a duration of 10 ms or less plays at 100 ms
// (DeferredImageDecoder, for every format). A frame that doesn't decode
// leaves the canvas as it was.
//
// Sizes follow libwebp, because x/image allocates by them and the bytes
// are anyone's (a custom emoji): a picture's size is its own bitstream
// header's (VP8's key frame header, VP8L's), which must be its ANMF
// rectangle's, and every rectangle must lie in the canvas (libwebp's
// demux: CheckFrameBounds), so the canvas bounds every frame's decode. A
// file breaking either rule is undecodable. A still WebP's picture must be
// its VP8X canvas's size (libwebp's ParseHeadersInternal), and decodes
// rewrapped on its own as a frame does (decodeWebP): x/image refuses a
// VP8X still that has an alpha flag but no ALPH chunk, as every lossless
// one with alpha and metadata has, and Chromium shows those.
//
// The header size is necessary but not sufficient: what a lossless (VP8L)
// picture allocates also depends on its structure (how many prefix-code
// groups its meta image names, how big each code's alphabet is), which the
// header doesn't constrain. So every picture decodes through the vendored
// x/image decoders in internal/webp and internal/vp8l, which charge each
// allocation to a byte budget before making it: a child of the decode's
// Budget (decmem.go), limited to webpBudget, scaled to the picture's size,
// that draws from the process-wide decode pool like every other charge. A
// file whose decode would need more fails with ErrTooBig before the
// allocation that doesn't fit; one the pool can't cover now, with ErrBusy.
// Stills, animation frames and ALPH alpha planes (compression 1 is a VP8L
// stream) all go through it. An animation frame's picture is given back
// once it's drawn on the canvas.

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/draw"
	"image/gif"
	"io"
	"log"
	"time"

	"github.com/some-du6e/clack/internal/imgcache/internal/budget"
	"github.com/some-du6e/clack/internal/imgcache/internal/webp"
)

var errBadWebP = errors.New("imgcache: bad WebP")

// isWebP reports whether data is a RIFF WebP file.
func isWebP(b []byte) bool {
	return len(b) >= 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP"
}

// isAnimWebP reports whether data is a WebP whose VP8X header says it's
// animated.
func isAnimWebP(b []byte) bool {
	return len(b) >= 30 && isWebP(b) && string(b[12:16]) == "VP8X" && b[20]&0x02 != 0
}

// isAnim reports whether data may hold an animation (a GIF or an
// animated WebP): it decodes through the frame index.
func isAnim(data []byte) bool { return isGIF(data) || isAnimWebP(data) }

// indexAnim is indexGIF or indexWebP, by data's signature, the index
// charged to bg.
func indexAnim(data []byte, bg *budget.Budget) (*gifIndex, error) {
	if isAnimWebP(data) {
		return indexWebP(data, bg)
	}
	return indexGIF(data, bg)
}

func le24(b []byte) int { return int(b[0]) | int(b[1])<<8 | int(b[2])<<16 }

// riffEnd is where a WebP's RIFF form ends: at its stated size, or at
// the end of the data when that comes first.
func riffEnd(b []byte) int {
	end := len(b)
	if n := int(binary.LittleEndian.Uint32(b[4:8])) + 8; n >= 12 && n < end {
		end = n
	}
	return end
}

// indexWebP reads an animated WebP's canvas, loop count and frames, its
// frames array charged to bg as it grows. A file that breaks off after its
// first frame is the frames it has.
func indexWebP(b []byte, bg *budget.Budget) (*gifIndex, error) {
	if !isAnimWebP(b) {
		return nil, errBadWebP
	}
	end := riffEnd(b)
	ix := &gifIndex{webp: true}
	truncated, seenX := false, false
	for p := 12; p+8 <= end; {
		id, size := string(b[p:p+4]), int(binary.LittleEndian.Uint32(b[p+4:p+8]))
		q := p + 8
		cut := size < 0 || q+size > end
		if cut { // past the end: what's there is the file
			if id != "ANMF" {
				break
			}
			size = end - q
		}
		c := b[q : q+size]
		switch id {
		case "VP8X":
			if size < 10 || seenX {
				return nil, errBadWebP
			}
			seenX = true
			ix.w, ix.h = 1+le24(c[4:]), 1+le24(c[7:])
			if ix.w > MaxSide || ix.h > MaxSide || ix.w*ix.h > MaxPixels {
				return nil, ErrTooBig
			}
		case "ANIM":
			if size >= 6 {
				// WebP counts passes, GIF's (and gifIndex's) repetitions
				switch n := int(binary.LittleEndian.Uint16(c[4:6])); n {
				case 0:
					ix.loop = 0 // forever
				case 1:
					ix.loop = -1 // once
				default:
					ix.loop = n - 1
				}
			}
		case "ANMF":
			if size < 16 {
				break
			}
			x, y := 2*le24(c[0:]), 2*le24(c[3:])
			fw, fh := 1+le24(c[6:]), 1+le24(c[9:])
			if !seenX || x+fw > ix.w || y+fh > ix.h { // libwebp's CheckFrameBounds
				return nil, errBadWebP
			}
			if len(ix.frames) == maxFrames {
				truncated = true
				p = end
				continue
			}
			// the picture's own header gives the size x/image decodes it at:
			// the rectangle's (libwebp's demux takes the frame's size from
			// it, its mux writes the rectangle from it)
			_, pic, lossless := webpPicture(c[16:])
			pw, ph, ok := webpSize(pic, lossless)
			if !ok && cut { // the file ends in the frame's header: the frames before it
				p = end
				continue
			}
			if !ok || pw != fw || ph != fh {
				return nil, errBadWebP
			}
			fl := c[15]
			f := gifFrame{r: image.Rect(x, y, x+fw, y+fh), trans: -1, data: q + 16, n: size - 16,
				delay: animDelay(time.Duration(le24(c[12:])) * time.Millisecond), blend: fl&0x02 == 0}
			if fl&0x01 != 0 {
				f.disp = gif.DisposalBackground
			}
			if err := ix.grow(bg); err != nil {
				return nil, err
			}
			ix.frames = append(ix.frames, f)
		}
		p = q + size + size&1
	}
	if !seenX || len(ix.frames) == 0 {
		return nil, errBadWebP
	}
	if truncated {
		log.Printf("imgcache: an animated WebP of more than %d frames plays its first %d", maxFrames, maxFrames)
	}
	return ix, nil
}

// webpPicture finds a frame's or a still's picture in chunks: the first
// ALPH chunk and the VP8 or VP8L chunk (each with its 8-byte header),
// skipping any other chunks before them. A picture chunk cut short by the
// end of the data is what's there (x/image decodes it as far as it goes).
func webpPicture(chunks []byte) (alph, pic []byte, lossless bool) {
	for p := 0; p+8 <= len(chunks); {
		id, size := string(chunks[p:p+4]), int(binary.LittleEndian.Uint32(chunks[p+4:p+8]))
		if size < 0 || p+8+size > len(chunks) {
			if id != "VP8 " && id != "VP8L" {
				break
			}
			size = len(chunks) - p - 8 // cut short
		}
		ch := chunks[p : p+8+size]
		switch id {
		case "ALPH":
			if alph == nil {
				alph = ch
			}
		case "VP8 ", "VP8L":
			return alph, ch, id == "VP8L"
		}
		p += 8 + size + size&1
	}
	return nil, nil, false
}

// webpSize is a picture's size by its own bitstream header (pic: the
// VP8 or VP8L chunk, header and all), the size x/image allocates its
// decode by. ok is false where libwebp's VP8LGetInfo or VP8GetInfo finds
// no picture: a VP8L signature or version that's wrong; a VP8 frame that
// isn't a key frame, has a bad start code or an unknown profile, isn't
// shown, has a first partition past the chunk, or is 0 wide or high.
func webpSize(pic []byte, lossless bool) (w, h int, ok bool) {
	if len(pic) < 8 {
		return 0, 0, false
	}
	b := pic[8:]
	if lossless {
		// the signature, the width and height less one in 14 bits each, an
		// alpha hint and a 3-bit version
		if len(b) < 5 || b[0] != 0x2f || b[4]>>5 != 0 {
			return 0, 0, false
		}
		v := binary.LittleEndian.Uint32(b[1:5])
		return int(v&0x3fff) + 1, int(v>>14&0x3fff) + 1, true
	}
	// the 3-byte frame tag, the start code, then the width and height in
	// 14 bits each (2 bits of upscaling above)
	if len(b) < 10 || b[3] != 0x9d || b[4] != 0x01 || b[5] != 0x2a {
		return 0, 0, false
	}
	tag := int(b[0]) | int(b[1])<<8 | int(b[2])<<16
	if tag&1 != 0 || tag>>1&7 > 3 || tag>>4&1 == 0 || tag>>5 >= len(b) {
		return 0, 0, false
	}
	w, h = int(b[6])|int(b[7]&0x3f)<<8, int(b[8])|int(b[9]&0x3f)<<8
	return w, h, w > 0 && h > 0
}

// webpStillLen is how long webpStill's file of alph and pic is.
func webpStillLen(alph, pic []byte, lossless bool) int {
	n := 12 + len(pic) + len(pic)&1
	if alph != nil && !lossless {
		n += 18 + len(alph) + len(alph)&1
	}
	return n
}

// webpStill writes into buf (reused) a still WebP of a picture chunk of
// w×h and, for a lossy one, its ALPH chunk (VP8L carries its own alpha,
// and x/image refuses ALPH with it): only what x/image decodes, every
// chunk's length its real one.
func webpStill(buf, alph, pic []byte, lossless bool, w, h int) []byte {
	b := append(buf[:0], "RIFF\x00\x00\x00\x00WEBP"...)
	chunk := func(ch []byte) {
		b = append(b, ch...)
		binary.LittleEndian.PutUint32(b[len(b)-len(ch)+4:], uint32(len(ch)-8)) // a cut one's real length
		if len(ch)&1 != 0 {
			b = append(b, 0)
		}
	}
	if alph != nil && !lossless {
		b = append(b, "VP8X\x0a\x00\x00\x00\x10\x00\x00\x00"...)
		b = append(b, byte(w-1), byte((w-1)>>8), byte((w-1)>>16), byte(h-1), byte((h-1)>>8), byte((h-1)>>16))
		chunk(alph)
	}
	chunk(pic)
	binary.LittleEndian.PutUint32(b[4:8], uint32(len(b)-8))
	return b
}

// stillWebP finds a still WebP's picture: in the simple format the VP8
// or VP8L chunk the file starts with; in the extended one (VP8X, not
// animated) the ALPH and VP8/VP8L chunks after any others (ICCP, EXIF,
// XMP), whose header must give the canvas's size, as libwebp's
// ParseHeadersInternal requires (x/image checks the canvas, then
// allocates by the header). ALPH counts only under VP8X's alpha flag, as
// libwebp's demux (Chromium's) has it.
func stillWebP(b []byte, maxPixels, maxSide int) (alph, pic []byte, lossless bool, w, h int, err error) {
	if !isWebP(b) {
		return nil, nil, false, 0, 0, errBadWebP
	}
	chunks := b[12:riffEnd(b)]
	if len(chunks) < 8 {
		return nil, nil, false, 0, 0, errBadWebP
	}
	cw, ch := 0, 0
	switch string(chunks[:4]) {
	case "VP8 ", "VP8L":
		_, pic, lossless = webpPicture(chunks)
	case "VP8X":
		size := int(binary.LittleEndian.Uint32(chunks[4:8]))
		if size < 10 || size > len(chunks)-8 {
			return nil, nil, false, 0, 0, errBadWebP
		}
		x := chunks[8:]
		if x[0]&0x02 != 0 { // animated: indexWebP
			return nil, nil, false, 0, 0, errBadWebP
		}
		cw, ch = 1+le24(x[4:]), 1+le24(x[7:])
		alph, pic, lossless = webpPicture(chunks[min(8+size+size&1, len(chunks)):])
		if x[0]&0x10 == 0 {
			alph = nil
		}
	default:
		return nil, nil, false, 0, 0, errBadWebP
	}
	w, h, ok := webpSize(pic, lossless)
	if !ok || cw != 0 && (w != cw || h != ch) {
		return nil, nil, false, 0, 0, errBadWebP
	}
	if w > maxSide || h > maxSide || w*h > maxPixels {
		return nil, nil, false, 0, 0, ErrTooBig
	}
	return alph, pic, lossless, w, h, nil
}

// webpStillReader reads a still WebP of a picture: the VP8/VP8L chunk,
// and for a lossy one its ALPH chunk under a VP8X (VP8L carries its own
// alpha), each chunk's length rewritten to what's there, without copying
// the picture's bytes (a still can be a multi-megabyte photo).
func webpStillReader(alph, pic []byte, lossless bool, w, h int) io.Reader {
	head := func(id string, n int) []byte {
		b := append([]byte(id), 0, 0, 0, 0)
		binary.LittleEndian.PutUint32(b[4:], uint32(n))
		return b
	}
	parts := [][]byte{[]byte("WEBP")}
	bodyLen := 4
	add := func(id string, payload []byte) {
		parts = append(parts, head(id, len(payload)), payload)
		bodyLen += 8 + len(payload)
		if len(payload)&1 != 0 { // chunks are padded to even
			parts = append(parts, []byte{0})
			bodyLen++
		}
	}
	if alph != nil && !lossless {
		add("VP8X", []byte{0x10, 0, 0, 0,
			byte(w - 1), byte((w - 1) >> 8), byte((w - 1) >> 16),
			byte(h - 1), byte((h - 1) >> 8), byte((h - 1) >> 16)})
		add("ALPH", alph[8:])
	}
	add(string(pic[:4]), pic[8:])
	readers := []io.Reader{bytes.NewReader(head("RIFF", bodyLen))}
	for _, p := range parts {
		readers = append(readers, bytes.NewReader(p))
	}
	return io.MultiReader(readers...)
}

// decodeWebP decodes a still WebP: its picture's chunks alone (stillWebP,
// webpStillReader), with the size checks the bitstream header needs, then
// decodePicture within the picture's budget. maxPixels/maxSide are the
// caller's decoded-size limit (Decode passes MaxPixels/MaxSide;
// DecodeWithin a larger one), which bounds the picture and so its budget.
func decodeWebP(data []byte, maxPixels, maxSide int, b *budget.Budget) (image.Image, error) {
	alph, pic, lossless, w, h, err := stillWebP(data, maxPixels, maxSide)
	if err != nil {
		return nil, err
	}
	return decodePicture(webpStillReader(alph, pic, lossless, w, h), w, h, b.Sub(webpBudget(w, h)))
}

// decodePicture decodes a still WebP r of a w×h picture, every allocation
// charged first to pb (internal/webp, internal/vp8l): a child of the
// decode's budget limited to webpBudget(w, h), drawing from the decode
// pool. One past its limit fails the decode, before it's made, with an
// error that is ErrTooBig (the same bytes always need the same memory);
// one the pool can't cover now, with ErrBusy.
func decodePicture(r io.Reader, w, h int, pb *budget.Budget) (image.Image, error) {
	m, err := webp.Decode(r, pb)
	if err != nil {
		return nil, budgetErr(err)
	}
	return m, nil
}

// webpBudget is the most decoding a w×h WebP picture may allocate in all:
// webpPixelBytes for what's proportional to its pixels, and webpCodeBytes
// for its lossless prefix codes. w×h is at most the caller's MaxPixels or
// DecodeWithin limit, so that limit's webpBudget is the hard ceiling: about
// 0.5 GB at MaxPixels (40 MP), of which a real 40 MP lossless picture uses
// roughly 0.3 GB.
func webpBudget(w, h int) int64 { return webpPixelBytes(w, h) + webpCodeBytes(w, h) }

// webpPixelBytes is what a w×h picture's decode allocates in proportion
// to its pixels, at most: a lossless one's pixels (4 bytes each), the copy
// a colour-indexing transform unpacks them into when it packs several to
// a byte (4 more, the packed ones then at most 2), and its transform and
// meta prefix-code tile images (¼ each, 3 at most); a lossy one's ALPH
// plane, decoded as a lossless picture is and then kept (1 more), and its
// YCbCr in whole macroblocks (1.5, each side rounded up to 16). That's
// under 9.3 a pixel and 27 per row and column, and 1 MB covers the fixed
// rest: the decoders and readers, colour caches, the sub-images' prefix
// codes, the scratch codes are read with, the group index map.
func webpPixelBytes(w, h int) int64 {
	return 10*int64(w)*int64(h) + 32*int64(w+h) + 1<<20
}

// webpCodeBytes is what a w×h lossless picture's prefix-code groups may
// take: 4 MB, plus 40 bytes a pixel, up to 96 MB. That's what libwebp's own
// encoder writes with room to spare. Its worst case, measured (Pillow
// 10.2, libwebp 1.3.2, lossless), is a picture of 16×16 tiles of distinct
// few-colour palettes: 2,428-2,600 groups on 800×832, 20.5 MB as the
// decoder keeps them, 31 bytes a pixel; real pictures use a few groups.
// Its encoder never writes more than 2,600 groups (MAX_HUFF_IMAGE_SIZE)
// with colour caches of up to 10 bits, at most 36.4 KB each here, hence
// the cap. Groups no tile uses cost nothing (internal/vp8l reads and drops
// them).
func webpCodeBytes(w, h int) int64 {
	return min(4<<20+40*int64(w)*int64(h), 96<<20)
}

// webpFrameBytes bounds what decoding one of ix's frames allocates
// (drawWebP): the frame's chunks as a still WebP and x/image's copy of a
// lossy picture's partitions, at most the largest frame's chunks each,
// and webpDecodeBytes for a picture as big as the canvas, which every
// frame's rectangle, and so its picture, fits in (indexWebP).
func webpFrameBytes(ix *gifIndex) int {
	most := 0
	for i := range ix.frames {
		most = max(most, ix.frames[i].n)
	}
	return 2*most + webpDecodeBytes(ix.w, ix.h)
}

// webpDecodeBytes is what a player reserves for decoding a w×h frame
// (webpFrameBytes): its pixel-proportional working set, webpPixelBytes.
// The frame's prefix codes, which webpBudget bounds too, are freed as
// soon as the frame is decoded and are small for real pictures, so they
// aren't reserved for the player's life.
func webpDecodeBytes(w, h int) int { return int(webpPixelBytes(w, h)) }

// decodeWebPFrame decodes WebP frame f's picture, charged to a child of
// d.b that the caller releases once the picture is drawn. A frame that
// doesn't decode is the error (ErrBusy: one the pool can't cover now).
func (d *gifDecoder) decodeWebPFrame(f *gifFrame) (image.Image, *budget.Budget, error) {
	end := min(f.data+f.n, len(d.data))
	if f.data >= end {
		return nil, nil, errBadWebP
	}
	alph, pic, lossless := webpPicture(d.data[f.data:end])
	// the decoder allocates by the picture's header: only one of the
	// frame's own size decodes (indexWebP checked it; this is what's
	// decoded), and within that size's budget (decodePicture), so a playing
	// animation can't allocate out of proportion to its canvas per frame
	w, h, ok := webpSize(pic, lossless)
	if !ok || w != f.r.Dx() || h != f.r.Dy() {
		return nil, nil, errBadWebP
	}
	if n := webpStillLen(alph, pic, lossless); cap(d.wfile) < n {
		// the file grows with the largest frame so far, only, so what it
		// charges over the decoder's life is under the encoded bytes
		if err := d.b.Charge(int64(n)); err != nil {
			return nil, nil, budgetErr(err)
		}
		d.wfile = make([]byte, 0, n)
	}
	d.wfile = webpStill(d.wfile, alph, pic, lossless, w, h)
	pb := d.b.Sub(webpBudget(w, h))
	m, err := decodePicture(bytes.NewReader(d.wfile), w, h, pb)
	if err != nil {
		pb.Release()
		return nil, nil, err
	}
	return m, pb, nil
}

// drawWebP composes WebP frame f's picture m on the canvas: blended over
// what's there, or replacing its rectangle. A frame that didn't decode (m
// nil) leaves the canvas as it is (d.werr says why).
func (d *gifDecoder) drawWebP(f *gifFrame, m image.Image) {
	if m == nil {
		return
	}
	mb := m.Bounds()
	// the ALPH plane is sized by the rectangle, which the picture's size
	// is: one that's short would be read past
	if a, ok := m.(*image.NYCbCrA); ok && (a.AStride < mb.Dx() || len(a.A) < a.AStride*mb.Dy()) {
		d.werr = errBadWebP
		return
	}
	r := f.r.Intersect(d.canvas.Rect).Intersect(mb.Sub(mb.Min).Add(f.r.Min))
	if r.Empty() {
		return
	}
	op := draw.Src
	if f.blend {
		op = draw.Over
	}
	draw.Draw(d.canvas, r, m, mb.Min.Add(r.Min.Sub(f.r.Min)), op)
}

// firstWebPFrame is an animated WebP's first frame on its canvas, composed
// as playback composes it (the still its decode draws first): what a copy
// of the image takes, as Chromium's copyImageAt copies the frame shown.
// The canvas is within maxPixels and maxSide; everything is charged to b.
func firstWebPFrame(data []byte, maxPixels, maxSide int, b *budget.Budget) (image.Image, error) {
	ix, err := indexAnim(data, b)
	if err != nil {
		return nil, budgetErr(err)
	}
	if ix.w <= 0 || ix.h <= 0 || ix.w > maxSide || ix.h > maxSide || ix.w*ix.h > maxPixels {
		return nil, ErrTooBig
	}
	d, err := newGIFDecoder(ix, data, b)
	if err != nil {
		return nil, budgetErr(err)
	}
	if _, err := d.compose(); err != nil {
		return nil, budgetErr(err)
	}
	if d.werr != nil {
		return nil, budgetErr(d.werr)
	}
	return d.canvas, nil
}
