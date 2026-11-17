// Package webptest writes synthetic animated WebP files for tests: solid
// lossless (VP8L) pictures, ANMF frames around any picture chunks, and the
// RIFF container. Nothing here comes from Slack.
package webptest

import (
	"encoding/binary"
	"image/color"
)

// bits writes a VP8L bitstream, least significant bit first.
type bits struct {
	b    []byte
	acc  uint64
	nacc uint
}

func (w *bits) put(v uint32, n uint) {
	w.acc |= uint64(v) << w.nacc
	w.nacc += n
	for w.nacc >= 8 {
		w.b = append(w.b, byte(w.acc))
		w.acc >>= 8
		w.nacc -= 8
	}
}

func (w *bits) bytes() []byte {
	if w.nacc > 0 {
		w.b = append(w.b, byte(w.acc))
		w.acc, w.nacc = 0, 0
	}
	return w.b
}

// VP8LSolid is a VP8L bitstream (no chunk header) of a w×h picture of
// one colour: no transforms, no colour cache, and five prefix codes of one
// symbol each, so every pixel takes zero bits.
func VP8LSolid(w, h int, c color.NRGBA) []byte {
	var b bits
	b.put(0x2f, 8)
	b.put(uint32(w-1), 14)
	b.put(uint32(h-1), 14)
	alpha := uint32(0)
	if c.A != 0xff {
		alpha = 1
	}
	b.put(alpha, 1)
	b.put(0, 3) // version
	b.put(0, 1) // no transform
	b.put(0, 1) // no colour cache
	b.put(0, 1) // no meta prefix codes
	// the five codes: green, red, blue, alpha, distance
	for _, sym := range []uint8{c.G, c.R, c.B, c.A, 0} {
		b.put(1, 1) // simple code
		b.put(0, 1) // one symbol
		b.put(1, 1) // of 8 bits
		b.put(uint32(sym), 8)
	}
	return b.bytes()
}

// putMSB writes n's low bits most-significant first, the order a VP8L
// decoder reads a canonical prefix code (the first bit read is the code's
// high bit). put is least-significant first, so a canonical code can't go
// out whole.
func (w *bits) putMSB(v uint32, n uint) {
	for i := int(n) - 1; i >= 0; i-- {
		w.put((v>>uint(i))&1, 1)
	}
}

// vp8lFlatByteCode writes a VP8L prefix code over an alphabet of
// alphabetSize symbols in which every symbol 0–255 has length 8 (a flat
// byte code, Kraft sum 256·2⁻⁸ = 1) and any symbol ≥256 length 0. Its
// canonical code for byte v is v itself, so a literal pixel channel of
// value v is emitted with putMSB(v, 8). It lets a test write arbitrary
// literal pixels, which one-symbol codes (vp8lSimpleTree) cannot.
func (w *bits) vp8lFlatByteCode(alphabetSize int) {
	w.put(0, 1) // not a simple code
	// The code-length code: 12 lengths in codeLengthCodeOrder
	// (17,18,0,1,2,3,4,5,16,6,7,8), giving symbols 0 and 8 length 1 and the
	// rest 0 — a two-symbol code where length-8 is "8" and length-0 is "0".
	w.put(12-4, 4)
	for _, l := range []uint32{0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 1} {
		w.put(l, 3)
	}
	w.put(0, 1) // no max-symbol limit: read a length for every symbol
	// symbol 8 (code-length 8) has canonical code 1, symbol 0 (length 0) code 0
	for i := 0; i < alphabetSize; i++ {
		if i < 256 {
			w.put(1, 1) // length 8
		} else {
			w.put(0, 1) // length 0: unused
		}
	}
}

// vp8lLiteralGroup writes the five prefix codes of a Huffman group whose
// green/red/blue/alpha are flat byte codes (so any RGBA pixel is a literal)
// and whose distance code is a one-symbol placeholder (no LZ77 is emitted).
// ccBits > 0 widens green's alphabet by the colour cache, as the format
// requires, though these pixels never use the cache.
func (w *bits) vp8lLiteralGroup(ccBits int) {
	green := 280
	if ccBits > 0 { // the cache widens green's alphabet, as x/image's does
		green += 1 << ccBits
	}
	w.vp8lFlatByteCode(green) // green: literals, length codes, cache
	w.vp8lFlatByteCode(256)   // red
	w.vp8lFlatByteCode(256)   // blue
	w.vp8lFlatByteCode(256)   // alpha
	w.vp8lSimpleTree(0)       // distance (unused)
}

// vp8lLiteralPixels emits len(pix)/4 literal RGBA pixels (red, green, blue,
// alpha bytes) through a group vp8lLiteralGroup wrote.
func (w *bits) vp8lLiteralPixels(pix []byte) {
	for p := 0; p+3 < len(pix); p += 4 {
		w.putMSB(uint32(pix[p+1]), 8) // green first (decodePix reads it first)
		w.putMSB(uint32(pix[p+0]), 8) // red
		w.putMSB(uint32(pix[p+2]), 8) // blue
		w.putMSB(uint32(pix[p+3]), 8) // alpha
	}
}

// VP8LLiteral is a w×h VP8L bitstream (no chunk header) of literal RGBA
// pixels (4 bytes each, red-green-blue-alpha): no transforms, no colour
// cache, no meta prefix codes, one group of flat byte codes. Unlike
// VP8LSolid it carries a different colour per pixel, so a test can build a
// picture whose decode fills a known-size pixel buffer.
func VP8LLiteral(w, h int, pix []byte) []byte {
	var b bits
	b.put(0x2f, 8)
	b.put(uint32(w-1), 14)
	b.put(uint32(h-1), 14)
	b.put(0, 1) // alpha hint
	b.put(0, 3) // version
	b.put(0, 1) // no transform
	b.put(0, 1) // no colour cache
	b.put(0, 1) // no meta prefix codes
	b.vp8lLiteralGroup(0)
	b.vp8lLiteralPixels(pix)
	return b.bytes()
}

// VP8LLiteralCC is VP8LLiteral with the top-level colour cache declared at
// ccBits (1–11). The pixels are still literals (they never look the cache
// up), but the decoder allocates the 4·2^ccBits-entry cache and widens
// green's alphabet, so a test can measure the colour-cache allocation.
func VP8LLiteralCC(w, h, ccBits int, pix []byte) []byte {
	var b bits
	b.put(0x2f, 8)
	b.put(uint32(w-1), 14)
	b.put(uint32(h-1), 14)
	b.put(0, 1) // alpha hint
	b.put(0, 3) // version
	b.put(0, 1) // no transform
	b.put(1, 1) // use a colour cache
	b.put(uint32(ccBits), 4)
	b.put(0, 1) // no meta prefix codes
	b.vp8lLiteralGroup(ccBits)
	b.vp8lLiteralPixels(pix)
	return b.bytes()
}

// VP8LManyGroups is a w×h VP8L bitstream (no chunk header) whose meta
// prefix-code image names nGroups distinct Huffman groups: with tiles of
// 1<<hBits pixels, tile i (row-major) names group i (capped at nGroups−1),
// so a decoder that builds a group per named index allocates nGroups of
// them. The stream ends right after the meta image, before any group's
// codes: enough for the decoder to size and allocate the group array (the
// bomb this exercises) and then fail reading the first group. w×h must
// give at least nGroups tiles.
func VP8LManyGroups(w, h, hBits, nGroups int) []byte {
	var b bits
	b.put(0x2f, 8)
	b.put(uint32(w-1), 14)
	b.put(uint32(h-1), 14)
	b.put(0, 1) // alpha hint
	b.put(0, 3) // version
	b.put(0, 1) // no transform
	b.put(0, 1) // no colour cache
	b.put(1, 1) // use meta prefix codes
	b.put(uint32(hBits-2), 3)
	mw, mh := (w+(1<<hBits)-1)>>hBits, (h+(1<<hBits)-1)>>hBits
	pix := make([]byte, 4*mw*mh)
	for i := 0; i < mw*mh; i++ {
		g := i
		if g > nGroups-1 {
			g = nGroups - 1
		}
		pix[4*i+0] = byte(g >> 8) // red: group index high byte
		pix[4*i+1] = byte(g)      // green: group index low byte
	}
	// the meta image is itself a VP8L sub-image: one group, literal pixels
	b.put(0, 1) // the meta image uses no colour cache
	b.vp8lLiteralGroup(0)
	b.vp8lLiteralPixels(pix)
	return b.bytes()
}

// VP8LMetaSolids is a complete w×h VP8L bitstream (no chunk header) using
// meta prefix codes: tile i (row-major, tiles of 1<<hBits pixels) names
// Huffman group tileGroup[i], and group g is a solid colour[g] (one-symbol
// codes, so every pixel of a tile takes zero bits). It writes a group body
// for every index 0..max(tileGroup) in index order, as a real stream does.
// A decoder that keeps only the groups tiles name, renumbering them, must
// still paint each tile its own group's colour — including when tileGroup
// names them out of the order their bodies appear, which this can express
// and libwebp's dense in-order numbering cannot.
func VP8LMetaSolids(w, h, hBits int, tileGroup []int, colour []color.NRGBA) []byte {
	var b bits
	b.put(0x2f, 8)
	b.put(uint32(w-1), 14)
	b.put(uint32(h-1), 14)
	b.put(0, 1) // alpha hint
	b.put(0, 3) // version
	b.put(0, 1) // no transform
	b.put(0, 1) // no colour cache (top level)
	b.put(1, 1) // use meta prefix codes
	b.put(uint32(hBits-2), 3)
	mw, mh := (w+(1<<hBits)-1)>>hBits, (h+(1<<hBits)-1)>>hBits
	pix := make([]byte, 4*mw*mh)
	maxIdx := 0
	for i := 0; i < mw*mh; i++ {
		g := tileGroup[i]
		if g > maxIdx {
			maxIdx = g
		}
		pix[4*i+0] = byte(g >> 8) // red: index high byte
		pix[4*i+1] = byte(g)      // green: index low byte
	}
	b.put(0, 1) // the meta image uses no colour cache
	b.vp8lLiteralGroup(0)
	b.vp8lLiteralPixels(pix)
	// a group body for every index 0..maxIdx, in index order
	for g := 0; g <= maxIdx; g++ {
		c := color.NRGBA{}
		if g < len(colour) {
			c = colour[g]
		}
		for _, sym := range []uint8{c.G, c.R, c.B, c.A, 0} {
			b.vp8lSimpleTree(sym)
		}
	}
	// the top-level pixels take zero bits (every group's codes are one-symbol)
	return b.bytes()
}

// VP8LManyGroupsFull is a complete w×h VP8L bitstream (no chunk header)
// whose meta image names nGroups distinct Huffman groups, every group a
// full flat byte code (green, red, blue and alpha each a 256-symbol code,
// so each group's trees allocate the most a byte-literal code can). Unlike
// VP8LManyGroups it includes every group's body and the top-level pixels
// (all transparent black), so it decodes and the decoder reads and
// allocates each group's trees. It lets a test vary nGroups to measure the
// per-group tree-node allocation exactly. w×h must give at least nGroups
// tiles.
func VP8LManyGroupsFull(w, h, hBits, nGroups int) []byte {
	var b bits
	b.put(0x2f, 8)
	b.put(uint32(w-1), 14)
	b.put(uint32(h-1), 14)
	b.put(0, 1) // alpha hint
	b.put(0, 3) // version
	b.put(0, 1) // no transform
	b.put(0, 1) // no colour cache (top level)
	b.put(1, 1) // use meta prefix codes
	b.put(uint32(hBits-2), 3)
	mw, mh := (w+(1<<hBits)-1)>>hBits, (h+(1<<hBits)-1)>>hBits
	meta := make([]byte, 4*mw*mh)
	for i := 0; i < mw*mh; i++ {
		g := i
		if g > nGroups-1 {
			g = nGroups - 1
		}
		meta[4*i+0] = byte(g >> 8)
		meta[4*i+1] = byte(g)
	}
	b.put(0, 1) // the meta image uses no colour cache
	b.vp8lLiteralGroup(0)
	b.vp8lLiteralPixels(meta)
	// every group's body: a full flat byte code (no colour cache)
	for g := 0; g < nGroups; g++ {
		b.vp8lLiteralGroup(0)
	}
	// top-level pixels: transparent black literals (every group decodes them)
	for i := 0; i < w*h; i++ {
		b.putMSB(0, 8) // green
		b.putMSB(0, 8) // red
		b.putMSB(0, 8) // blue
		b.putMSB(0, 8) // alpha
	}
	return b.bytes()
}

// vp8lSimpleTree writes a one-symbol VP8L prefix code (simple, 8-bit
// symbol): the symbol then decodes with zero bits, as VP8LSolid relies on.
func (w *bits) vp8lSimpleTree(symbol uint8) {
	w.put(1, 1) // simple code
	w.put(0, 1) // one symbol
	w.put(1, 1) // of 8 bits
	w.put(uint32(symbol), 8)
}

// VP8LMeta is a 1×1 VP8L bitstream (no chunk header) that uses meta prefix
// codes: one tile (hBits 2) whose single meta pixel names Huffman group
// `group`. x/image sizes its group array make([]hGroup, group+1) by that
// index, so a group beyond the one tile (anything ≥ 1 here) is the
// decompression bomb the size gate can't see. When complete is true it
// also writes group 0's codes and the one pixel, so a valid picture
// (group 0) decodes to the colour c; otherwise the stream ends right after
// the meta image — enough for the validator, and for x/image to allocate
// the oversized group array before it fails on the missing codes.
func VP8LMeta(group int, c color.NRGBA, complete bool) []byte {
	var b bits
	// header: magic, width−1=0, height−1=0 (1×1), alpha hint, version
	b.put(0x2f, 8)
	b.put(0, 14)
	b.put(0, 14)
	b.put(0, 1)
	b.put(0, 3)
	b.vp8lMetaBody(group, c, complete)
	return b.bytes()
}

// VP8LMetaBody is VP8LMeta without the 5-byte header, for a compression-1
// ALPH alpha plane: x/image decodes that with a header it synthesizes from
// the picture's size (readAlpha), so the stored bytes start at the body.
// The plane is 1×1 (one tile), and group is the index its meta pixel names.
func VP8LMetaBody(group int) []byte {
	var b bits
	b.vp8lMetaBody(group, color.NRGBA{}, false)
	return b.bytes()
}

// vp8lMetaBody writes everything after the VP8L header for a 1×1 picture
// that uses meta prefix codes naming Huffman group `group`.
func (b *bits) vp8lMetaBody(group int, c color.NRGBA, complete bool) {
	b.put(0, 1) // no transform
	// top-level decodePix: no colour cache, then the Huffman groups
	b.put(0, 1) // no colour cache
	b.put(1, 1) // use meta prefix codes
	b.put(0, 3) // hBits raw 0 → tile size 1<<2 (nTiles(1,2)=1 tile)
	// the meta image (a 1×1 sub-image): no colour cache, then five simple
	// codes whose green/red give the named group index (red<<8|green)
	b.put(0, 1)
	b.vp8lSimpleTree(uint8(group))      // green = group low byte
	b.vp8lSimpleTree(uint8(group >> 8)) // red = group high byte
	b.vp8lSimpleTree(0)                 // blue
	b.vp8lSimpleTree(0)                 // alpha
	b.vp8lSimpleTree(0)                 // distance
	// the single meta pixel decodes with zero bits (one-symbol codes)
	if complete {
		// group 0's codes and the one top-level pixel, so x/image decodes a
		// 1×1 image of colour c (meaningful only when group == 0)
		b.vp8lSimpleTree(c.G)
		b.vp8lSimpleTree(c.R)
		b.vp8lSimpleTree(c.B)
		b.vp8lSimpleTree(c.A)
		b.vp8lSimpleTree(0)
	}
}

// VP8Header is a lossy VP8 key-frame bitstream header (no chunk header)
// claiming a w×h picture, padded to at least padTo bytes. The body isn't
// a real frame: it's for the size checks, where a picture whose header
// disagrees with its frame or canvas is rejected before any decode.
func VP8Header(w, h, padTo int) []byte {
	n := padTo
	if n < 16 {
		n = 16
	}
	b := make([]byte, n)
	tag := 1<<4 | 1<<5 // a shown key frame, version 0, first partition 1 byte
	b[0], b[1], b[2] = byte(tag), byte(tag>>8), byte(tag>>16)
	b[3], b[4], b[5] = 0x9d, 0x01, 0x2a // the start code
	b[6], b[7] = byte(w), byte(w>>8)    // 14-bit width (top 2 bits scaling)
	b[8], b[9] = byte(h), byte(h>>8)    // 14-bit height
	return b
}

// Simple is a still WebP in the simple format: a bare VP8 or VP8L chunk.
func Simple(chunk []byte) []byte { return riff(append([]byte("WEBP"), chunk...)) }

// Still is a still WebP in the extended (VP8X) format: a canvas of cw×ch
// with the feature flags (0x10 ALPHA, 0x08 EXIF, …), then the chunks.
func Still(cw, ch, flags int, chunks ...[]byte) []byte {
	x := []byte{byte(flags), 0, 0, 0}
	x = le24(x, cw-1)
	x = le24(x, ch-1)
	body := append([]byte("WEBP"), Chunk("VP8X", x)...)
	for _, c := range chunks {
		body = append(body, c...)
	}
	return riff(body)
}

// riff wraps a form body (starting "WEBP") in the RIFF container.
func riff(body []byte) []byte {
	b := append([]byte("RIFF\x00\x00\x00\x00"), body...)
	binary.LittleEndian.PutUint32(b[4:], uint32(len(body)))
	return b
}

// Chunk is a RIFF chunk: its FourCC, length and payload, padded to even.
func Chunk(id string, payload []byte) []byte {
	b := make([]byte, 8, 8+len(payload)+1)
	copy(b, id)
	binary.LittleEndian.PutUint32(b[4:], uint32(len(payload)))
	b = append(b, payload...)
	if len(payload)&1 != 0 {
		b = append(b, 0)
	}
	return b
}

func le24(b []byte, v int) []byte { return append(b, byte(v), byte(v>>8), byte(v>>16)) }

// Frame is one ANMF frame.
type Frame struct {
	X, Y, W, H int // X and Y even (WebP stores them halved)
	DurMS      int
	NoBlend    bool // replaces its rectangle instead of drawing over it
	Dispose    bool // cleared to transparent once the next frame is due
	Chunks     [][]byte
}

// Solid is a frame of one colour (a VP8L picture).
func Solid(x, y, w, h, durMS int, c color.NRGBA) Frame {
	return Frame{X: x, Y: y, W: w, H: h, DurMS: durMS, Chunks: [][]byte{Chunk("VP8L", VP8LSolid(w, h, c))}}
}

func (f Frame) anmf() []byte {
	var p []byte
	p = le24(p, f.X/2)
	p = le24(p, f.Y/2)
	p = le24(p, f.W-1)
	p = le24(p, f.H-1)
	p = le24(p, f.DurMS)
	var fl byte
	if f.NoBlend {
		fl |= 0x02
	}
	if f.Dispose {
		fl |= 0x01
	}
	p = append(p, fl)
	for _, c := range f.Chunks {
		p = append(p, c...)
	}
	return Chunk("ANMF", p)
}

// Anim is an animated WebP of a w×h canvas: loop is ANIM's loop count
// (the number of passes, 0 forever).
func Anim(w, h, loop int, frames ...Frame) []byte {
	x := []byte{0x02 | 0x10, 0, 0, 0} // animation, alpha
	x = le24(x, w-1)
	x = le24(x, h-1)
	anim := []byte{0, 0, 0, 0, byte(loop), byte(loop >> 8)} // a transparent background
	body := append([]byte("WEBP"), Chunk("VP8X", x)...)
	body = append(body, Chunk("ANIM", anim)...)
	for _, f := range frames {
		body = append(body, f.anmf()...)
	}
	b := append([]byte("RIFF\x00\x00\x00\x00"), body...)
	binary.LittleEndian.PutUint32(b[4:], uint32(len(body)))
	return b
}

// ExplodesDurations are Explodes' frame durations in milliseconds.
var ExplodesDurations = []int{70, 30, 40, 50, 60, 30, 40, 70, 50, 30, 60, 40, 50}

// explodesRects are Explodes' frame rectangles (x, y, w, h; x and y even).
var explodesRects = [][4]int{{0, 0, 32, 32}, {14, 14, 2, 2}, {6, 6, 21, 21}, {4, 8, 24, 16},
	{10, 2, 12, 28}, {0, 12, 32, 8}, {8, 8, 16, 16}, {2, 2, 28, 28},
	{12, 0, 8, 32}, {16, 16, 14, 14}, {0, 0, 18, 18}, {6, 20, 20, 10}, {10, 10, 12, 12}}

// Explodes is a stand-in for the :explodes: custom emoji, which Slack
// serves as an animated WebP behind a .png URL: synthetic pixels in the
// real file's shape. A 32×32 canvas with alpha, looping forever, of 13
// frames of 30 to 70 ms (ExplodesDurations); the first covers the canvas
// and replaces it (no blending), the rest are smaller rectangles (2×2,
// 21×21, …) blended over what's there, none disposed, every third one
// translucent. The real file's pictures are lossy (9 VP8, 4 ALPH+VP8);
// these are lossless (VP8L, all webptest writes), which a player decodes
// once either way.
func Explodes() []byte {
	frames := make([]Frame, len(explodesRects))
	for i, r := range explodesRects {
		c := color.NRGBA{R: uint8(255 - 15*i), G: uint8(40 + 16*i), B: uint8(20 * (i % 4)), A: 255}
		if i > 0 && i%3 == 0 {
			c.A = 160
		}
		frames[i] = Solid(r[0], r[1], r[2], r[3], ExplodesDurations[i], c)
		frames[i].NoBlend = i == 0
	}
	return Anim(32, 32, 0, frames...)
}
