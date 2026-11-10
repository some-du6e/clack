// Package woff2 decodes WOFF2 web fonts (W3C WOFF File Format 2.0,
// https://www.w3.org/TR/WOFF2/) into plain sfnt (TrueType/OpenType)
// bytes that go-text and Gio can parse.
//
// Slack serves its Roboto Mono, Noto Sans and Atkinson Hyperlegible Next
// faces as woff2 (slack-kit-gantry-v2-styles' @font-face rules), and
// go-text only reads WOFF 1. The decoder follows the spec's section 5
// (the brotli stream, the table directory) and section 5.1 / 5.4 (the glyf
// + loca and hmtx transforms). Font collections ("ttcf" flavour) aren't
// supported: Slack ships none.
package woff2

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"

	"github.com/andybalholm/brotli"
)

// MaxSize bounds the decompressed font (a corrupt or hostile file can't
// make the decoder allocate more).
const MaxSize = 64 << 20

// Signature is the first four bytes of a WOFF2 file.
const Signature = "wOF2"

// IsWOFF2 reports whether b starts with the WOFF2 signature.
func IsWOFF2(b []byte) bool { return len(b) >= 4 && string(b[:4]) == Signature }

// knownTags is the spec's table of tags a directory entry may name by
// index (flags & 0x3f; 63 means the tag follows as 4 bytes).
var knownTags = [63]string{
	"cmap", "head", "hhea", "hmtx", "maxp", "name", "OS/2", "post", "cvt ",
	"fpgm", "glyf", "loca", "prep", "CFF ", "VORG", "EBDT", "EBLC", "gasp",
	"hdmx", "kern", "LTSH", "PCLT", "VDMX", "vhea", "vmtx", "BASE", "GDEF",
	"GPOS", "GSUB", "EBSC", "JSTF", "MATH", "CBDT", "CBLC", "COLR", "CPAL",
	"SVG ", "sbix", "acnt", "avar", "bdat", "bloc", "bsln", "cvar", "fdsc",
	"feat", "fmtx", "fvar", "gvar", "hsty", "just", "lcar", "mort", "morx",
	"opbd", "prop", "trak", "Zapf", "Silf", "Glat", "Gloc", "Feat", "Sill",
}

type entry struct {
	tag         string
	transform   int // the transform version (flags >> 6)
	origLen     uint32
	transformed bool
	length      uint32 // bytes in the decompressed stream
	data        []byte // the table, reconstructed
}

// reader is a bounds-checked big-endian cursor.
type reader struct {
	b   []byte
	off int
	err error
}

func (r *reader) fail(what string) {
	if r.err == nil {
		r.err = fmt.Errorf("woff2: truncated %s", what)
	}
}

func (r *reader) take(n int, what string) []byte {
	if r.err != nil || n < 0 || r.off+n > len(r.b) {
		r.fail(what)
		return nil
	}
	s := r.b[r.off : r.off+n]
	r.off += n
	return s
}

func (r *reader) u8(what string) uint8 {
	if s := r.take(1, what); s != nil {
		return s[0]
	}
	return 0
}

func (r *reader) u16(what string) uint16 {
	if s := r.take(2, what); s != nil {
		return binary.BigEndian.Uint16(s)
	}
	return 0
}

func (r *reader) u32(what string) uint32 {
	if s := r.take(4, what); s != nil {
		return binary.BigEndian.Uint32(s)
	}
	return 0
}

// base128 reads a UIntBase128 (spec 4.1): at most 5 bytes, no leading
// zero byte, no overflow past 32 bits.
func (r *reader) base128(what string) uint32 {
	var v uint32
	for i := 0; i < 5; i++ {
		b := r.u8(what)
		if r.err != nil {
			return 0
		}
		if i == 0 && b == 0x80 {
			r.err = errors.New("woff2: UIntBase128 with a leading zero")
			return 0
		}
		if v&0xfe000000 != 0 {
			r.err = errors.New("woff2: UIntBase128 overflow")
			return 0
		}
		v = v<<7 | uint32(b&0x7f)
		if b&0x80 == 0 {
			return v
		}
	}
	r.err = errors.New("woff2: UIntBase128 longer than 5 bytes")
	return 0
}

// u255 reads a 255UInt16 (spec 4.2).
func (r *reader) u255(what string) uint16 {
	const wordCode, oneMore2, oneMore1, lowest = 253, 254, 255, 253
	switch c := r.u8(what); c {
	case wordCode:
		return r.u16(what)
	case oneMore1:
		return uint16(r.u8(what)) + lowest
	case oneMore2:
		return uint16(r.u8(what)) + lowest*2
	default:
		return uint16(c)
	}
}

// Decode turns a WOFF2 file into sfnt bytes.
func Decode(b []byte) ([]byte, error) {
	r := &reader{b: b}
	if string(r.take(4, "header")) != Signature {
		if r.err != nil {
			return nil, r.err
		}
		return nil, errors.New("woff2: not a WOFF2 file")
	}
	flavor := r.u32("header")
	r.u32("header") // length
	numTables := int(r.u16("header"))
	r.u16("header") // reserved
	totalSfnt := r.u32("header")
	totalCompressed := r.u32("header")
	r.take(4+4*5, "header") // version, metadata, private block
	if r.err != nil {
		return nil, r.err
	}
	if flavor == 0x74746366 { // "ttcf"
		return nil, errors.New("woff2: font collections aren't supported")
	}
	if numTables == 0 {
		return nil, errors.New("woff2: no tables")
	}
	if totalSfnt > MaxSize {
		return nil, errors.New("woff2: font too large")
	}

	es := make([]*entry, numTables)
	byTag := map[string]*entry{}
	var stream uint64
	for i := range es {
		flags := r.u8("table directory")
		e := &entry{transform: int(flags >> 6)}
		if t := flags & 0x3f; t == 63 {
			e.tag = string(r.take(4, "table tag"))
		} else {
			e.tag = knownTags[t]
		}
		e.origLen = r.base128("table length")
		e.length = e.origLen
		switch e.tag {
		case "glyf", "loca":
			e.transformed = e.transform == 0 // 3 is the null transform
		default:
			e.transformed = e.transform != 0
		}
		if e.transformed {
			e.length = r.base128("transform length")
		}
		if r.err != nil {
			return nil, r.err
		}
		if e.transformed && e.tag != "glyf" && e.tag != "loca" && !(e.tag == "hmtx" && e.transform == 1) {
			return nil, fmt.Errorf("woff2: unknown transform %d of %q", e.transform, e.tag)
		}
		if _, dup := byTag[e.tag]; dup {
			return nil, fmt.Errorf("woff2: table %q twice", e.tag)
		}
		if e.origLen > MaxSize {
			return nil, errors.New("woff2: table too large")
		}
		es[i], byTag[e.tag] = e, e
		stream += uint64(e.length)
	}
	if stream > MaxSize {
		return nil, errors.New("woff2: font too large")
	}
	comp := r.take(int(totalCompressed), "compressed stream")
	if r.err != nil {
		return nil, r.err
	}
	raw, err := io.ReadAll(io.LimitReader(brotli.NewReader(bytes.NewReader(comp)), int64(stream)+1))
	if err != nil {
		return nil, fmt.Errorf("woff2: brotli: %w", err)
	}
	if uint64(len(raw)) != stream {
		return nil, fmt.Errorf("woff2: brotli stream is %d bytes, the directory says %d", len(raw), stream)
	}
	off := 0
	for _, e := range es {
		e.data = raw[off : off+int(e.length)]
		off += int(e.length)
	}

	if g, l := byTag["glyf"], byTag["loca"]; g != nil || l != nil {
		if g == nil || l == nil || g.transformed != l.transformed {
			return nil, errors.New("woff2: glyf and loca must come (and be transformed) together")
		}
		if g.transformed {
			glyf, loca, xMins, err := reconstructGlyf(g.data, byTag["head"])
			if err != nil {
				return nil, err
			}
			g.data, l.data = glyf, loca
			if h := byTag["hmtx"]; h != nil && h.transformed {
				if h.data, err = reconstructHmtx(h.data, xMins, byTag["hhea"]); err != nil {
					return nil, err
				}
			}
		}
	} else if h := byTag["hmtx"]; h != nil && h.transformed {
		return nil, errors.New("woff2: transformed hmtx without glyf")
	}
	return writeSfnt(flavor, es), nil
}

// ---- glyf / loca (spec 5.1) ----

const (
	glyfOverlapSimple = 1 << 0 // optionFlags: an overlapSimpleBitmap follows the streams

	// simple glyph flags (OpenType glyf)
	onCurve     = 0x01
	xShort      = 0x02
	yShort      = 0x04
	xSamePos    = 0x10
	ySamePos    = 0x20
	overlapFlag = 0x40

	// composite glyph flags
	argWords       = 0x0001
	haveScale      = 0x0008
	moreComponents = 0x0020
	haveXYScale    = 0x0040
	have2x2        = 0x0080
	haveInstr      = 0x0100
)

func reconstructGlyf(b []byte, head *entry) (glyf, loca []byte, xMins []int16, err error) {
	r := &reader{b: b}
	r.u16("glyf header") // reserved
	opts := r.u16("glyf header")
	numGlyphs := int(r.u16("glyf header"))
	indexFormat := r.u16("glyf header")
	var sizes [7]uint32
	for i := range sizes {
		sizes[i] = r.u32("glyf header")
	}
	if r.err != nil {
		return nil, nil, nil, r.err
	}
	var streams [7]*reader
	for i, n := range sizes {
		if uint64(n) > uint64(len(b)) {
			return nil, nil, nil, errors.New("woff2: glyf stream too long")
		}
		streams[i] = &reader{b: r.take(int(n), "glyf streams")}
	}
	if r.err != nil {
		return nil, nil, nil, r.err
	}
	nContours, nPoints, flagS, glyphS, compS, bboxS, instrS := streams[0], streams[1], streams[2], streams[3], streams[4], streams[5], streams[6]
	var overlap []byte
	if opts&glyfOverlapSimple != 0 {
		overlap = r.take((numGlyphs+7)/8, "overlap bitmap")
		if r.err != nil {
			return nil, nil, nil, r.err
		}
	}
	bboxBitmap := bboxS.take(4*((numGlyphs+31)/32), "bbox bitmap")
	if bboxS.err != nil {
		return nil, nil, nil, bboxS.err
	}
	hasBBox := func(i int) bool { return bboxBitmap[i>>3]&(0x80>>(i&7)) != 0 }

	if head != nil && len(head.data) >= 54 {
		// head.indexToLocFormat must match the loca we write
		binary.BigEndian.PutUint16(head.data[50:], indexFormat)
	}
	out := make([]byte, 0, len(b)*2)
	offsets := make([]uint32, numGlyphs+1)
	xMins = make([]int16, numGlyphs)
	var xs, ys []int32
	var fl []byte
	for i := 0; i < numGlyphs; i++ {
		offsets[i] = uint32(len(out))
		nc := int16(nContours.u16("nContour stream"))
		if nContours.err != nil {
			return nil, nil, nil, nContours.err
		}
		switch {
		case nc == 0:
			if hasBBox(i) {
				return nil, nil, nil, errors.New("woff2: empty glyph with a bbox")
			}
		case nc == -1: // composite
			if !hasBBox(i) {
				return nil, nil, nil, errors.New("woff2: composite glyph without a bbox")
			}
			bbox := bboxS.take(8, "bbox stream")
			start := compS.off
			instr := false
			for {
				flags := compS.u16("composite stream")
				compS.u16("composite stream") // glyph index
				n := 2
				if flags&argWords != 0 {
					n = 4
				}
				switch {
				case flags&haveScale != 0:
					n += 2
				case flags&haveXYScale != 0:
					n += 4
				case flags&have2x2 != 0:
					n += 8
				}
				compS.take(n, "composite stream")
				if flags&haveInstr != 0 {
					instr = true
				}
				if compS.err != nil || flags&moreComponents == 0 {
					break
				}
			}
			if compS.err != nil || bboxS.err != nil {
				return nil, nil, nil, errors.Join(compS.err, bboxS.err)
			}
			out = binary.BigEndian.AppendUint16(out, 0xffff)
			out = append(out, bbox...)
			xMins[i] = int16(binary.BigEndian.Uint16(bbox))
			out = append(out, compS.b[start:compS.off]...)
			if instr {
				n := glyphS.u255("glyph stream")
				ins := instrS.take(int(n), "instruction stream")
				if glyphS.err != nil || instrS.err != nil {
					return nil, nil, nil, errors.Join(glyphS.err, instrS.err)
				}
				out = binary.BigEndian.AppendUint16(out, n)
				out = append(out, ins...)
			}
		case nc > 0: // simple
			ends := make([]uint16, nc)
			total := 0
			for c := range ends {
				total += int(nPoints.u255("nPoints stream"))
				if total > 0xffff {
					return nil, nil, nil, errors.New("woff2: too many points")
				}
				ends[c] = uint16(total - 1)
			}
			if nPoints.err != nil {
				return nil, nil, nil, nPoints.err
			}
			flags := flagS.take(total, "flag stream")
			if flagS.err != nil {
				return nil, nil, nil, flagS.err
			}
			xs, ys, fl = xs[:0], ys[:0], fl[:0]
			var x, y int32
			for _, f := range flags {
				dx, dy, err := triplet(f, glyphS)
				if err != nil {
					return nil, nil, nil, err
				}
				x, y = x+dx, y+dy
				xs, ys = append(xs, x), append(ys, y)
				pf := byte(onCurve)
				if f&0x80 != 0 {
					pf = 0
				}
				fl = append(fl, pf)
			}
			ni := glyphS.u255("glyph stream")
			ins := instrS.take(int(ni), "instruction stream")
			if glyphS.err != nil || instrS.err != nil {
				return nil, nil, nil, errors.Join(glyphS.err, instrS.err)
			}
			var x0, y0, x1, y1 int16
			if hasBBox(i) {
				bb := bboxS.take(8, "bbox stream")
				if bboxS.err != nil {
					return nil, nil, nil, bboxS.err
				}
				x0, y0 = int16(binary.BigEndian.Uint16(bb)), int16(binary.BigEndian.Uint16(bb[2:]))
				x1, y1 = int16(binary.BigEndian.Uint16(bb[4:])), int16(binary.BigEndian.Uint16(bb[6:]))
			} else if total > 0 {
				mnx, mny, mxx, mxy := xs[0], ys[0], xs[0], ys[0]
				for k := range xs {
					mnx, mxx = min(mnx, xs[k]), max(mxx, xs[k])
					mny, mxy = min(mny, ys[k]), max(mxy, ys[k])
				}
				x0, y0, x1, y1 = clamp16(mnx), clamp16(mny), clamp16(mxx), clamp16(mxy)
			}
			xMins[i] = x0
			if overlap != nil && overlap[i>>3]&(0x80>>(i&7)) != 0 && len(fl) > 0 {
				fl[0] |= overlapFlag
			}
			out = binary.BigEndian.AppendUint16(out, uint16(nc))
			for _, v := range []int16{x0, y0, x1, y1} {
				out = binary.BigEndian.AppendUint16(out, uint16(v))
			}
			for _, e := range ends {
				out = binary.BigEndian.AppendUint16(out, e)
			}
			out = binary.BigEndian.AppendUint16(out, ni)
			out = append(out, ins...)
			out = appendSimplePoints(out, fl, xs, ys)
		default:
			return nil, nil, nil, fmt.Errorf("woff2: glyph %d has %d contours", i, nc)
		}
		for len(out)%4 != 0 {
			out = append(out, 0)
		}
		if len(out) > MaxSize {
			return nil, nil, nil, errors.New("woff2: glyf too large")
		}
	}
	offsets[numGlyphs] = uint32(len(out))

	if indexFormat == 0 {
		if len(out) > 0x1fffe {
			return nil, nil, nil, errors.New("woff2: glyf too large for short loca")
		}
		loca = make([]byte, 0, 2*(numGlyphs+1))
		for _, o := range offsets {
			loca = binary.BigEndian.AppendUint16(loca, uint16(o/2))
		}
	} else {
		loca = make([]byte, 0, 4*(numGlyphs+1))
		for _, o := range offsets {
			loca = binary.BigEndian.AppendUint32(loca, o)
		}
	}
	return out, loca, xMins, nil
}

func clamp16(v int32) int16 {
	return int16(max(math.MinInt16, min(math.MaxInt16, v)))
}

// triplet decodes one point's (dx, dy) from the glyph stream for the
// flag byte f (spec 5.2's triplet encoding table, as computed by the
// reference decoder).
func triplet(f byte, s *reader) (dx, dy int32, err error) {
	sign := func(flag int, v int32) int32 {
		if flag&1 != 0 {
			return v
		}
		return -v
	}
	flag := int(f & 0x7f)
	var n int
	switch {
	case flag < 84:
		n = 1
	case flag < 120:
		n = 2
	case flag < 124:
		n = 3
	default:
		n = 4
	}
	in := s.take(n, "glyph stream")
	if s.err != nil {
		return 0, 0, s.err
	}
	switch {
	case flag < 10:
		dy = sign(flag, int32(flag&14)<<7+int32(in[0]))
	case flag < 20:
		dx = sign(flag, int32((flag-10)&14)<<7+int32(in[0]))
	case flag < 84:
		b0, b1 := int32(flag-20), int32(in[0])
		dx = sign(flag, 1+(b0&0x30)+(b1>>4))
		dy = sign(flag>>1, 1+(b0&0x0c)<<2+(b1&0x0f))
	case flag < 120:
		b0 := int32(flag - 84)
		dx = sign(flag, 1+(b0/12)<<8+int32(in[0]))
		dy = sign(flag>>1, 1+((b0%12)>>2)<<8+int32(in[1]))
	case flag < 124:
		b2 := int32(in[1])
		dx = sign(flag, int32(in[0])<<4+b2>>4)
		dy = sign(flag>>1, (b2&0x0f)<<8+int32(in[2]))
	default:
		dx = sign(flag, int32(in[0])<<8+int32(in[1]))
		dy = sign(flag>>1, int32(in[2])<<8+int32(in[3]))
	}
	return dx, dy, nil
}

// appendSimplePoints writes a simple glyph's flags and coordinates in
// the usual compact form (byte deltas where they fit, no repeats).
func appendSimplePoints(out, fl []byte, xs, ys []int32) []byte {
	flags := make([]byte, len(fl))
	var px, py int32
	for i := range fl {
		f := fl[i]
		dx, dy := xs[i]-px, ys[i]-py
		px, py = xs[i], ys[i]
		switch {
		case dx == 0:
			f |= xSamePos
		case dx > -256 && dx < 256:
			f |= xShort
			if dx > 0 {
				f |= xSamePos
			}
		}
		switch {
		case dy == 0:
			f |= ySamePos
		case dy > -256 && dy < 256:
			f |= yShort
			if dy > 0 {
				f |= ySamePos
			}
		}
		flags[i] = f
	}
	out = append(out, flags...)
	put := func(d int32, short, same byte, f byte) []byte {
		switch {
		case f&short != 0:
			if d < 0 {
				d = -d
			}
			return append(out, byte(d))
		case f&same != 0:
			return out
		default:
			return binary.BigEndian.AppendUint16(out, uint16(int16(d)))
		}
	}
	px = 0
	for i := range xs {
		out = put(xs[i]-px, xShort, xSamePos, flags[i])
		px = xs[i]
	}
	py = 0
	for i := range ys {
		out = put(ys[i]-py, yShort, ySamePos, flags[i])
		py = ys[i]
	}
	return out
}

// ---- hmtx (spec 5.4) ----

func reconstructHmtx(b []byte, xMins []int16, hhea *entry) ([]byte, error) {
	if hhea == nil || len(hhea.data) < 36 {
		return nil, errors.New("woff2: transformed hmtx without hhea")
	}
	numH := int(binary.BigEndian.Uint16(hhea.data[34:]))
	numGlyphs := len(xMins)
	if numH < 1 || numH > numGlyphs {
		return nil, errors.New("woff2: bad numberOfHMetrics")
	}
	r := &reader{b: b}
	flags := r.u8("hmtx")
	adv := make([]uint16, numH)
	for i := range adv {
		adv[i] = r.u16("hmtx advances")
	}
	lsb := make([]int16, numGlyphs)
	for i := 0; i < numH; i++ {
		if flags&1 == 0 {
			lsb[i] = int16(r.u16("hmtx lsb"))
		} else {
			lsb[i] = xMins[i]
		}
	}
	for i := numH; i < numGlyphs; i++ {
		if flags&2 == 0 {
			lsb[i] = int16(r.u16("hmtx lsb"))
		} else {
			lsb[i] = xMins[i]
		}
	}
	if r.err != nil {
		return nil, r.err
	}
	out := make([]byte, 0, 4*numH+2*(numGlyphs-numH))
	for i := 0; i < numGlyphs; i++ {
		if i < numH {
			out = binary.BigEndian.AppendUint16(out, adv[i])
		}
		out = binary.BigEndian.AppendUint16(out, uint16(lsb[i]))
	}
	return out, nil
}

// ---- sfnt writer ----

func checksum(b []byte) uint32 {
	var s uint32
	for i := 0; i < len(b); i += 4 {
		var w [4]byte
		copy(w[:], b[i:])
		s += binary.BigEndian.Uint32(w[:])
	}
	return s
}

func writeSfnt(flavor uint32, es []*entry) []byte {
	sorted := append([]*entry(nil), es...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].tag < sorted[j].tag })
	n := len(sorted)
	es2, sel := 1, 0
	for es2*2 <= n {
		es2 *= 2
		sel++
	}
	size := 12 + 16*n
	for _, e := range sorted {
		size += (len(e.data) + 3) &^ 3
	}
	out := make([]byte, 0, size)
	out = binary.BigEndian.AppendUint32(out, flavor)
	out = binary.BigEndian.AppendUint16(out, uint16(n))
	out = binary.BigEndian.AppendUint16(out, uint16(es2*16))
	out = binary.BigEndian.AppendUint16(out, uint16(sel))
	out = binary.BigEndian.AppendUint16(out, uint16(n*16-es2*16))
	off := 12 + 16*n
	headAt := -1
	for _, e := range sorted {
		if e.tag == "head" && len(e.data) >= 12 {
			binary.BigEndian.PutUint32(e.data[8:], 0) // checkSumAdjustment, set below
			headAt = off
		}
		out = append(out, e.tag...)
		out = binary.BigEndian.AppendUint32(out, checksum(e.data))
		out = binary.BigEndian.AppendUint32(out, uint32(off))
		out = binary.BigEndian.AppendUint32(out, uint32(len(e.data)))
		off += (len(e.data) + 3) &^ 3
	}
	for _, e := range sorted {
		out = append(out, e.data...)
		for len(out)%4 != 0 {
			out = append(out, 0)
		}
	}
	if headAt >= 0 {
		binary.BigEndian.PutUint32(out[headAt+8:], 0xB1B0AFBA-checksum(out))
	}
	return out
}
