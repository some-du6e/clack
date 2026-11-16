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
