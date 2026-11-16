package imgcache

// A bounded, read-only validator for lossless (VP8L) WebP bitstreams.
//
// x/image's vp8l decoder sizes its Huffman-group array by the largest
// group index the picture's meta prefix-code image names:
//
//	hGroups = make([]hGroup, maxHGroupIndex+1)   // vp8l/decode.go
//
// maxHGroupIndex is read as red<<8|green from the meta image's pixels, so
// it can be up to 65535 — and an hGroup is [5]hTree, each hTree carrying a
// [128]uint32 look-up table, ~2.7 KB a group. A 1×1 picture whose
// one-tile meta image names group 65535 therefore makes x/image allocate
// ~175 MB, completely out of proportion to the canvas, before it reads a
// single tree; the size gate (webpSize) only reads the 14-bit header
// dimensions and never sees this. The same stream decodes a still, every
// animation frame, and (through readAlpha's compression-1 path) a lossy
// picture's ALPH alpha plane, so the allocation repeats per frame and per
// lane worker.
//
// validateVP8L walks the bitstream the way vp8l.Decode does — header,
// transforms, the top-level colour cache, then the use-meta flag — and,
// when meta prefix codes are used, decodes the (small, canvas-bounded)
// meta image and rejects the picture when its largest group index is at
// or beyond the number of tiles the meta image has
// (nTiles(w,hBits)·nTiles(h,hBits)). A well-formed encoder (libwebp)
// assigns group indices densely from zero and never uses more groups than
// tiles, so every real picture passes; the bomb is refused before x/image
// allocates anything. The array x/image then builds holds at most that
// many groups — at most ⌈w/4⌉·⌈h/4⌉, i.e. O(w·h) and tied to the canvas,
// never a fixed 65536.
//
// It mirrors golang.org/x/image/vp8l (decode.go, huffman.go,
// transform.go) closely so it accepts exactly the streams x/image
// accepts: a parse error here is a stream x/image would also reject (after
// allocating), so rejecting it early is safe. It fully decodes only the
// small sub-images (transform tiles and the meta image, each at most the
// canvas) to consume their bits and read the meta indices; it never runs
// the main pixel loop, applies no inverse transforms and builds no output
// image — x/image does the real decode once, now within a canvas-sized
// bound.

import (
	"fmt"
	"io"
)

var errBadVP8L = errBadWebP

// checkWebPLossless bounds the lossless data a WebP picture hands to
// x/image: a VP8L still itself, and a lossy picture's compression-1 ALPH
// alpha plane (which x/image decodes as a VP8L image too, through a
// synthesized header). w×h is the picture's canvas, already validated by
// the caller (stillWebP / webpSize) to be within MaxSide/MaxPixels. Lossy
// pictures without a VP8L-compressed alpha plane carry no lossless data
// and pass through.
func checkWebPLossless(alph, pic []byte, lossless bool, w, h int) error {
	if lossless {
		if len(pic) < 8 {
			return errBadVP8L
		}
		return validateVP8L(pic[8:], w, h)
	}
	if alph != nil && len(alph) >= 9 {
		body := alph[8:] // the ALPH chunk body: a flags byte, then the data
		if body[0]&0x03 == 1 {
			// compression 1: the rest is a VP8L stream x/image decodes with a
			// header synthesized from the picture's w×h (webp/decode.go:readAlpha).
			return validateVP8LAlpha(body[1:], w, h)
		}
	}
	return nil
}

// validateVP8L validates a VP8L bitstream that begins with its own 5-byte
// header (a VP8L chunk's body). w×h is the canvas the picture must be.
func validateVP8L(stream []byte, w, h int) error {
	d := &vlDecoder{b: stream}
	hw, hh, err := d.header()
	if err != nil {
		return err
	}
	if int(hw) != w || int(hh) != h {
		return errBadVP8L // webpSize already ensured this; be strict
	}
	return d.validateBody(hw, hh)
}

// validateVP8LAlpha validates a compression-1 ALPH plane's VP8L bitstream,
// which carries no header of its own: x/image decodes it with a header
// synthesized from the picture's w×h (so the plane is w×h).
func validateVP8LAlpha(stream []byte, w, h int) error {
	if w <= 0 || h <= 0 || w-1 > 0x3fff || h-1 > 0x3fff {
		return errBadVP8L // readAlpha rejects these
	}
	d := &vlDecoder{b: stream}
	return d.validateBody(int32(w), int32(h))
}

// vlDecoder reads a VP8L bit-stream from b, least significant bit first.
type vlDecoder struct {
	b     []byte
	p     int
	bits  uint32
	nBits uint32
}

func (d *vlDecoder) readByte() (byte, error) {
	if d.p >= len(d.b) {
		return 0, io.EOF
	}
	c := d.b[d.p]
	d.p++
	return c, nil
}

func (d *vlDecoder) read(n uint32) (uint32, error) {
	for d.nBits < n {
		c, err := d.readByte()
		if err != nil {
			if err == io.EOF {
				err = io.ErrUnexpectedEOF
			}
			return 0, err
		}
		d.bits |= uint32(c) << d.nBits
		d.nBits += 8
	}
	u := d.bits & (1<<n - 1)
	d.bits >>= n
	d.nBits -= n
	return u, nil
}

// header reads the VP8L header (magic, 14-bit width−1 and height−1, an
// alpha hint and a 3-bit version), returning the width and height.
func (d *vlDecoder) header() (w, h int32, err error) {
	magic, err := d.read(8)
	if err != nil {
		return 0, 0, err
	}
	if magic != 0x2f {
		return 0, 0, errBadVP8L
	}
	width, err := d.read(14)
	if err != nil {
		return 0, 0, err
	}
	height, err := d.read(14)
	if err != nil {
		return 0, 0, err
	}
	if _, err := d.read(1); err != nil { // alpha hint, ignored
		return 0, 0, err
	}
	version, err := d.read(3)
	if err != nil {
		return 0, 0, err
	}
	if version != 0 {
		return 0, 0, errBadVP8L
	}
	return int32(width) + 1, int32(height) + 1, nil
}

const (
	vlTransformPredictor     = 0
	vlTransformCrossColor    = 1
	vlTransformSubtractGreen = 2
	vlTransformColorIndexing = 3
	vlNTransformTypes        = 4
)

// validateBody walks the transforms and the top-level colour-cache and
// use-meta flags, decoding the meta image when present, and rejects a
// picture whose meta prefix codes would make x/image allocate more
// Huffman groups than the canvas has tiles.
func (d *vlDecoder) validateBody(w, h int32) error {
	if w <= 0 || h <= 0 || int(w) > MaxSide || int(h) > MaxSide || int64(w)*int64(h) > int64(MaxPixels) {
		return ErrTooBig
	}
	// Transforms (vp8l.Decode): each may appear once and precedes the pixel
	// data; a colour-indexing transform narrows the width the top level uses.
	var seen [vlNTransformTypes]bool
	for {
		more, err := d.read(1)
		if err != nil {
			return err
		}
		if more == 0 {
			break
		}
		tt, err := d.read(2)
		if err != nil {
			return err
		}
		if seen[tt] {
			return errBadVP8L
		}
		seen[tt] = true
		switch tt {
		case vlTransformPredictor, vlTransformCrossColor:
			bits, err := d.read(3)
			if err != nil {
				return err
			}
			bits += 2
			if _, err := d.decodePix(vlNTiles(w, bits), vlNTiles(h, bits)); err != nil {
				return err
			}
		case vlTransformSubtractGreen:
			// no bits, no sub-image
		case vlTransformColorIndexing:
			nColors, err := d.read(8)
			if err != nil {
				return err
			}
			nColors++
			var tbits uint32
			switch {
			case nColors <= 2:
				tbits = 3
			case nColors <= 4:
				tbits = 2
			case nColors <= 16:
				tbits = 1
			}
			w = vlNTiles(w, tbits)
			if _, err := d.decodePix(int32(nColors), 1); err != nil {
				return err
			}
		default:
			return errBadVP8L
		}
	}
	// The top-level decodePix (topLevel=true): the colour cache, then the
	// Huffman groups, whose count comes from the meta image.
	useCC, err := d.read(1)
	if err != nil {
		return err
	}
	if useCC != 0 {
		ccBits, err := d.read(4)
		if err != nil {
			return err
		}
		if ccBits < 1 || 11 < ccBits {
			return errBadVP8L
		}
	}
	useMeta, err := d.read(1)
	if err != nil {
		return err
	}
	if useMeta == 0 {
		return nil // one Huffman group: x/image allocates a single hGroup
	}
	hBits, err := d.read(3)
	if err != nil {
		return err
	}
	hBits += 2
	hPix, err := d.decodePix(vlNTiles(w, hBits), vlNTiles(h, hBits))
	if err != nil {
		return err
	}
	maxIdx := 0
	for p := 0; p+1 < len(hPix); p += 4 {
		if i := int(hPix[p])<<8 | int(hPix[p+1]); i > maxIdx {
			maxIdx = i
		}
	}
	numTiles := len(hPix) / 4
	if maxIdx >= numTiles {
		// x/image would make([]hGroup, maxIdx+1) — more groups than the meta
		// image has tiles to reference, the decompression bomb.
		return fmt.Errorf("%w (VP8L meta prefix codes name group %d of %d tiles)", ErrTooBig, maxIdx, numTiles)
	}
	return nil
}

// vlNTiles is the number of 1<<bits-sized tiles that cover size pixels.
func vlNTiles(size int32, bits uint32) int32 { return (size + 1<<bits - 1) >> bits }

const (
	vlHuffGreen    = 0
	vlHuffRed      = 1
	vlHuffBlue     = 2
	vlHuffAlpha    = 3
	vlHuffDistance = 4
	vlNHuff        = 5

	vlNLiteralCodes  = 256
	vlNLengthCodes   = 24
	vlNDistanceCodes = 40

	vlColorCacheMultiplier = 0x1e35a7bd
)

var vlAlphabetSizes = [vlNHuff]uint32{
	vlNLiteralCodes + vlNLengthCodes,
	vlNLiteralCodes,
	vlNLiteralCodes,
	vlNLiteralCodes,
	vlNDistanceCodes,
}

// vlHGroup is the five Huffman trees used to decode pixel data.
type vlHGroup [vlNHuff]vlHTree

// decodePix decodes a sub-image's pixels (a transform tile image, a colour
// palette, or the meta prefix-code image) — always below the top level, so
// one Huffman group and no nested meta image. It returns the decoded RGBA
// bytes. w×h is bounded by the canvas (the caller derives it with
// vlNTiles), so 4·w·h is O(canvas).
func (d *vlDecoder) decodePix(w, h int32) ([]byte, error) {
	if w <= 0 || h <= 0 || int64(w)*int64(h) > int64(MaxPixels) {
		return nil, errBadVP8L
	}
	// Colour cache.
	var ccBits, ccShift uint32
	var ccEntries []uint32
	useCC, err := d.read(1)
	if err != nil {
		return nil, err
	}
	if useCC != 0 {
		if ccBits, err = d.read(4); err != nil {
			return nil, err
		}
		if ccBits < 1 || 11 < ccBits {
			return nil, errBadVP8L
		}
		ccShift = 32 - ccBits
		ccEntries = make([]uint32, 1<<ccBits)
	}
	// One Huffman group (no meta prefix codes below the top level).
	var hg vlHGroup
	for j := 0; j < vlNHuff; j++ {
		alphabetSize := vlAlphabetSizes[j]
		if j == 0 && ccBits > 0 {
			alphabetSize += 1 << ccBits
		}
		if err := d.decodeHuffmanTree(&hg[j], alphabetSize); err != nil {
			return nil, err
		}
	}
	// Pixels (vp8l/decode.go decodePix): literal, LZ77 back-reference, or
	// colour-cache lookup, filling a canvas-bounded buffer.
	pix := make([]byte, 4*int(w)*int(h))
	p, cachedP := 0, 0
	// x tracks the column for distanceMap's row arithmetic; a sub-image has
	// one Huffman group, so there is no per-tile group lookup and no need to
	// track the row.
	x := int32(0)
	for p < len(pix) {
		green, err := hg[vlHuffGreen].next(d)
		if err != nil {
			return nil, err
		}
		switch {
		case green < vlNLiteralCodes:
			red, err := hg[vlHuffRed].next(d)
			if err != nil {
				return nil, err
			}
			blue, err := hg[vlHuffBlue].next(d)
			if err != nil {
				return nil, err
			}
			alpha, err := hg[vlHuffAlpha].next(d)
			if err != nil {
				return nil, err
			}
			pix[p+0] = uint8(red)
			pix[p+1] = uint8(green)
			pix[p+2] = uint8(blue)
			pix[p+3] = uint8(alpha)
			p += 4
			x++
			if x == w {
				x = 0
			}
		case green < vlNLiteralCodes+vlNLengthCodes:
			length, err := d.lz77Param(green - vlNLiteralCodes)
			if err != nil {
				return nil, err
			}
			distSym, err := hg[vlHuffDistance].next(d)
			if err != nil {
				return nil, err
			}
			distCode, err := d.lz77Param(distSym)
			if err != nil {
				return nil, err
			}
			dist := vlDistanceMap(w, distCode)
			pEnd := p + 4*int(length)
			q := p - 4*int(dist)
			qEnd := pEnd - 4*int(dist)
			if p < 0 || len(pix) < pEnd || q < 0 || len(pix) < qEnd {
				return nil, errBadVP8L
			}
			for ; p < pEnd; p, q = p+1, q+1 {
				pix[p] = pix[q]
			}
			x += int32(length)
			for x >= w {
				x -= w
			}
		default:
			for ; cachedP < p; cachedP += 4 {
				argb := uint32(pix[cachedP+0])<<16 |
					uint32(pix[cachedP+1])<<8 |
					uint32(pix[cachedP+2])<<0 |
					uint32(pix[cachedP+3])<<24
				ccEntries[(argb*vlColorCacheMultiplier)>>ccShift] = argb
			}
			green -= vlNLiteralCodes + vlNLengthCodes
			if int(green) >= len(ccEntries) {
				return nil, errBadVP8L
			}
			argb := ccEntries[green]
			pix[p+0] = uint8(argb >> 16)
			pix[p+1] = uint8(argb >> 8)
			pix[p+2] = uint8(argb >> 0)
			pix[p+3] = uint8(argb >> 24)
			p += 4
			x++
			if x == w {
				x = 0
			}
		}
	}
	return pix, nil
}

// lz77Param returns the next LZ77 length or distance (vp8l/decode.go).
func (d *vlDecoder) lz77Param(symbol uint32) (uint32, error) {
	if symbol < 4 {
		return symbol + 1, nil
	}
	extraBits := (symbol - 2) >> 1
	offset := (2 + symbol&1) << extraBits
	n, err := d.read(extraBits)
	if err != nil {
		return 0, err
	}
	return offset + n + 1, nil
}

// vlDistanceMapTable and vlDistanceMap map an LZ77 distance code to a pixel
// offset (vp8l/decode.go).
var vlDistanceMapTable = [120]uint8{
	0x18, 0x07, 0x17, 0x19, 0x28, 0x06, 0x27, 0x29, 0x16, 0x1a,
	0x26, 0x2a, 0x38, 0x05, 0x37, 0x39, 0x15, 0x1b, 0x36, 0x3a,
	0x25, 0x2b, 0x48, 0x04, 0x47, 0x49, 0x14, 0x1c, 0x35, 0x3b,
	0x46, 0x4a, 0x24, 0x2c, 0x58, 0x45, 0x4b, 0x34, 0x3c, 0x03,
	0x57, 0x59, 0x13, 0x1d, 0x56, 0x5a, 0x23, 0x2d, 0x44, 0x4c,
	0x55, 0x5b, 0x33, 0x3d, 0x68, 0x02, 0x67, 0x69, 0x12, 0x1e,
	0x66, 0x6a, 0x22, 0x2e, 0x54, 0x5c, 0x43, 0x4d, 0x65, 0x6b,
	0x32, 0x3e, 0x78, 0x01, 0x77, 0x79, 0x53, 0x5d, 0x11, 0x1f,
	0x64, 0x6c, 0x42, 0x4e, 0x76, 0x7a, 0x21, 0x2f, 0x75, 0x7b,
	0x31, 0x3f, 0x63, 0x6d, 0x52, 0x5e, 0x00, 0x74, 0x7c, 0x41,
	0x4f, 0x10, 0x20, 0x62, 0x6e, 0x30, 0x73, 0x7d, 0x51, 0x5f,
	0x40, 0x72, 0x7e, 0x61, 0x6f, 0x50, 0x71, 0x7f, 0x60, 0x70,
}

func vlDistanceMap(w int32, code uint32) int32 {
	if int32(code) > int32(len(vlDistanceMapTable)) {
		return int32(code) - int32(len(vlDistanceMapTable))
	}
	distCode := int32(vlDistanceMapTable[code-1])
	yOffset := distCode >> 4
	xOffset := 8 - distCode&0xf
	if dd := yOffset*w + xOffset; dd >= 1 {
		return dd
	}
	return 1
}
