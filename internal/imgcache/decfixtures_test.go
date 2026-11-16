package imgcache

// Synthetic image bytes crafted so a decode's dangerous allocation is the
// dominant one: a grayscale PNG with tRNS (decodes to NRGBA, 4× the Gray),
// a 16-bit interlaced PNG (8 B/px plus an image per Adam7 pass), a
// progressive JPEG (per-component coefficient blocks) and a 4-component
// one (the black plane), and a GIF with a big canvas or many local palettes.
//
// The JPEG builders write a valid header up to and including the SOS
// marker, with no entropy-coded data: image/jpeg allocates the image, the
// 4th-component plane and the progressive coefficients as soon as it parses
// the SOS header, before reading a single scan byte, so a header alone
// drives exactly the allocations under test. Nothing here comes from Slack.

import (
	"bytes"
	"compress/lzw"
	"compress/zlib"
	"encoding/binary"
	"hash/crc32"
)

// ---- PNG ----

// pngColor is a PNG colour type + bit depth.
type pngColor struct {
	colorType byte
	depth     byte
	channels  int
}

var (
	pngGray8  = pngColor{0, 8, 1}
	pngGray16 = pngColor{0, 16, 1}
	pngRGB16  = pngColor{2, 16, 3}
	pngRGBA8  = pngColor{6, 8, 4}
)

// pngChunk appends a PNG chunk (length, type, data, CRC) to b.
func pngChunk(b []byte, typ string, data []byte) []byte {
	var hdr [8]byte
	binary.BigEndian.PutUint32(hdr[:4], uint32(len(data)))
	copy(hdr[4:], typ)
	b = append(b, hdr[:]...)
	b = append(b, data...)
	crc := crc32.NewIEEE()
	crc.Write(hdr[4:8])
	crc.Write(data)
	return binary.BigEndian.AppendUint32(b, crc.Sum32())
}

// adam7Passes is each Adam7 pass's width and height for a w×h image.
var adam7 = [7][4]int{{8, 8, 0, 0}, {8, 8, 4, 0}, {4, 8, 0, 4}, {4, 4, 2, 0}, {2, 4, 0, 2}, {2, 2, 1, 0}, {1, 2, 0, 1}}

// pngSolid builds a valid single-colour PNG of w×h: colour type and depth
// c, interlaced (Adam7) or not, with an optional tRNS chunk. The pixels are
// all zero, so every scanline filters to zero bytes and the IDAT is tiny —
// but the decode allocates the full image (and, interlaced, an image per
// pass) by the IHDR, which is the point.
func pngSolid(w, h int, c pngColor, interlace bool, trns []byte) []byte {
	out := append([]byte(nil), "\x89PNG\r\n\x1a\n"...)
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], uint32(w))
	binary.BigEndian.PutUint32(ihdr[4:], uint32(h))
	ihdr[8] = c.depth
	ihdr[9] = c.colorType
	if interlace {
		ihdr[12] = 1
	}
	out = pngChunk(out, "IHDR", ihdr)
	if trns != nil {
		out = pngChunk(out, "tRNS", trns)
	}
	bpp := int(c.depth) * c.channels
	rowBytes := func(pw int) int { return 1 + (bpp*pw+7)/8 }
	var raw []byte
	emit := func(pw, ph int) {
		if pw <= 0 || ph <= 0 {
			return
		}
		row := make([]byte, rowBytes(pw)) // filter 0 + zero samples
		for y := 0; y < ph; y++ {
			raw = append(raw, row...)
		}
	}
	if interlace {
		for _, p := range adam7 {
			pw := (w - p[2] + p[0] - 1) / p[0]
			ph := (h - p[3] + p[1] - 1) / p[1]
			emit(pw, ph)
		}
	} else {
		emit(w, h)
	}
	var zb bytes.Buffer
	zw := zlib.NewWriter(&zb)
	zw.Write(raw)
	zw.Close()
	out = pngChunk(out, "IDAT", zb.Bytes())
	return pngChunk(out, "IEND", nil)
}

// ---- JPEG ----

// jpegHeader builds a JPEG that ends at the SOS header (no scan data):
// SOI, an Adobe APP14 (for a 4-component image's colour model), SOF (sof2
// progressive or sof0 baseline), and an interleaved SOS naming every
// component. image/jpeg makes the image, the 4th-component plane and (for
// progressive) the coefficient blocks when it parses the SOS, then fails
// reading the absent scan data — so the header alone drives those
// allocations.
func jpegHeader(w, h, nComp int, progressive bool) []byte {
	seg := func(marker byte, payload []byte) []byte {
		n := len(payload) + 2
		return append([]byte{0xff, marker, byte(n >> 8), byte(n)}, payload...)
	}
	out := []byte{0xff, 0xd8} // SOI
	if nComp == 4 {
		// Adobe APP14: "Adobe", version, flags0, flags1, transform (2 = YCCK)
		out = append(out, seg(0xee, []byte{'A', 'd', 'o', 'b', 'e', 0, 100, 0, 0, 0, 0, 2})...)
	}
	sof := []byte{8, byte(h >> 8), byte(h), byte(w >> 8), byte(w), byte(nComp)}
	for i := 0; i < nComp; i++ {
		sof = append(sof, byte(i+1), 0x11, 0) // id, hv=1×1, quant table 0
	}
	marker := byte(0xc0) // SOF0 baseline
	if progressive {
		marker = 0xc2 // SOF2 progressive
	}
	out = append(out, seg(marker, sof)...)
	sos := []byte{byte(nComp)}
	for i := 0; i < nComp; i++ {
		sos = append(sos, byte(i+1), 0) // component, DC/AC table 0
	}
	sos = append(sos, 0, 0, 0) // Ss, Se, Ah/Al: a DC scan (zigStart 0)
	out = append(out, seg(0xda, sos)...)
	return out
}

// jpegDecoyHeader is jpegHeader with a COMment segment, before the real
// SOF, that contains a decoy 1×1 SOF0: image/jpeg skips the whole comment,
// so it parses the real SOF and allocates by it. A decoder that pre-parsed
// the header could be fooled into reading the decoy's tiny size.
func jpegDecoyHeader(w, h int) []byte {
	decoy := []byte{0xff, 0xc0, 0, 11, 8, 0, 1, 0, 1, 1, 0x11, 0} // 1×1 grayscale SOF0
	out := []byte{0xff, 0xd8}
	com := append([]byte{0xff, 0xfe, byte((len(decoy) + 2) >> 8), byte(len(decoy) + 2)}, decoy...)
	out = append(out, com...)
	return append(out, jpegHeader(w, h, 3, false)[2:]...) // the rest, past its own SOI
}

// progressiveJPEG is a complete, valid progressive JPEG of w×h with nComp
// components at 1×1 sampling (4:4:4): a quantization table, a DC Huffman
// table of one 1-bit code (difference category 0), one interleaved DC
// scan with every block's DC difference 0 (one 0 bit each), and EOI. It
// decodes, through image/jpeg's progressive path (every component's
// coefficient blocks, then the image rebuilt from them), to a flat grey
// image: the real allocations of a progressive photo of that size, from
// a file of about w·h·nComp/512 bytes.
func progressiveJPEG(w, h, nComp int) []byte {
	seg := func(marker byte, payload []byte) []byte {
		n := len(payload) + 2
		return append([]byte{0xff, marker, byte(n >> 8), byte(n)}, payload...)
	}
	out := []byte{0xff, 0xd8} // SOI
	dqt := make([]byte, 65)   // Pq/Tq 0, then 64 entries
	for i := 1; i < 65; i++ {
		dqt[i] = 1
	}
	out = append(out, seg(0xdb, dqt)...)
	sof := []byte{8, byte(h >> 8), byte(h), byte(w >> 8), byte(w), byte(nComp)}
	for i := 0; i < nComp; i++ {
		sof = append(sof, byte(i+1), 0x11, 0)
	}
	out = append(out, seg(0xc2, sof)...) // SOF2: progressive
	dht := make([]byte, 1+16+1)          // Tc/Th 0 (DC table 0), counts, values
	dht[1] = 1                           // one code of length 1
	dht[17] = 0                          // for category 0
	out = append(out, seg(0xc4, dht)...)
	sos := []byte{byte(nComp)}
	for i := 0; i < nComp; i++ {
		sos = append(sos, byte(i+1), 0)
	}
	sos = append(sos, 0, 0, 0) // Ss 0, Se 0, Ah/Al 0: the first DC scan
	out = append(out, seg(0xda, sos)...)
	blocks := ((w + 7) / 8) * ((h + 7) / 8) * nComp
	out = append(out, make([]byte, (blocks+7)/8)...) // one 0 bit a block
	return append(out, 0xff, 0xd9)                   // EOI
}

// manyFrameGIF is a GIF of frames frames, each the whole w×h screen of
// colour 0, its LZW data a few KB a frame: what a GIF of thousands of big
// frames is made of.
func manyFrameGIF(w, h, frames int) []byte {
	var lz bytes.Buffer
	lw := lzw.NewWriter(&lz, lzw.LSB, 2)
	lw.Write(make([]byte, w*h))
	lw.Close()
	var sub []byte
	for p := lz.Bytes(); len(p) > 0; {
		n := min(255, len(p))
		sub = append(sub, byte(n))
		sub = append(sub, p[:n]...)
		p = p[n:]
	}
	sub = append(sub, 0)
	out := []byte("GIF89a")
	out = append(out, byte(w), byte(w>>8), byte(h), byte(h>>8), 0x80, 0, 0) // a 2-colour global table
	out = append(out, 0, 0, 0, 255, 255, 255)
	for i := 0; i < frames; i++ {
		out = append(out, 0x21, 0xf9, 4, 0, 5, 0, 0, 0) // 50 ms
		out = append(out, 0x2c, 0, 0, 0, 0, byte(w), byte(w>>8), byte(h), byte(h>>8), 0)
		out = append(out, 2)
		out = append(out, sub...)
	}
	return append(out, 0x3b)
}
