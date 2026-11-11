package composer

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"testing"
	"unicode/utf16"
)

// dibSpec builds a packed DIB like the ones Windows apps put on the
// clipboard.
type dibSpec struct {
	hdr      int // 40 (BITMAPINFOHEADER), 108 (V4) or 124 (V5)
	w, h     int
	topDown  bool
	bpp      int
	comp     uint32
	masks    []uint32 // in the header (V4/V5) or after it (40)
	after    []uint32 // extra masks after a V4/V5 header (some writers)
	palette  []color.NRGBA
	pixel    func(x, y int) uint32 // the raw pixel value
	clrUsed  int
	truncate int
}

func (d dibSpec) bytes() []byte {
	le := binary.LittleEndian
	stride := (d.w*d.bpp + 31) / 32 * 4
	hdr := make([]byte, d.hdr)
	le.PutUint32(hdr[0:], uint32(d.hdr))
	le.PutUint32(hdr[4:], uint32(d.w))
	h := int32(d.h)
	if d.topDown {
		h = -h
	}
	le.PutUint32(hdr[8:], uint32(h))
	le.PutUint16(hdr[12:], 1)
	le.PutUint16(hdr[14:], uint16(d.bpp))
	le.PutUint32(hdr[16:], d.comp)
	le.PutUint32(hdr[20:], uint32(stride*d.h))
	le.PutUint32(hdr[32:], uint32(d.clrUsed))
	out := hdr
	if d.hdr > 40 {
		for i, m := range d.masks {
			le.PutUint32(hdr[40+4*i:], m)
		}
		for _, m := range d.after {
			out = le.AppendUint32(out, m)
		}
	} else {
		for _, m := range d.masks {
			out = le.AppendUint32(out, m)
		}
	}
	for _, c := range d.palette {
		out = append(out, c.B, c.G, c.R, 0)
	}
	px := make([]byte, stride*d.h)
	for y := 0; y < d.h; y++ {
		ry := d.h - 1 - y
		if d.topDown {
			ry = y
		}
		row := px[ry*stride:]
		for x := 0; x < d.w; x++ {
			v := d.pixel(x, y)
			switch d.bpp {
			case 1, 4, 8:
				bit := x * d.bpp
				row[bit/8] |= byte(v) << (8 - d.bpp - bit%8)
			default:
				for k := 0; k < d.bpp/8; k++ {
					row[x*d.bpp/8+k] = byte(v >> (8 * k))
				}
			}
		}
	}
	out = append(out, px...)
	return out[:len(out)-d.truncate]
}

// testColors is the picture every case draws: four distinct colours in a
// 3x2 grid (3 wide, so 24-bit rows need padding).
var testColors = [2][3]color.NRGBA{
	{{0xff, 0, 0, 0xff}, {0, 0xff, 0, 0xff}, {0, 0, 0xff, 0xff}},
	{{0x12, 0x34, 0x56, 0xff}, {0xff, 0xff, 0xff, 0xff}, {0, 0, 0, 0xff}},
}

func bgr(c color.NRGBA) uint32 { return uint32(c.R)<<16 | uint32(c.G)<<8 | uint32(c.B) }

func decodeDIB(t *testing.T, dib []byte) image.Image {
	t.Helper()
	data, err := attDIBToPNG(dib)
	if err != nil {
		t.Fatalf("attDIBToPNG: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("not a PNG: %v", err)
	}
	return img
}

func wantPixels(t *testing.T, img image.Image, want func(x, y int) color.NRGBA) {
	t.Helper()
	b := img.Bounds()
	if b.Dx() != 3 || b.Dy() != 2 {
		t.Fatalf("size %v, want 3x2", b.Size())
	}
	for y := 0; y < 2; y++ {
		for x := 0; x < 3; x++ {
			got := color.NRGBAModel.Convert(img.At(b.Min.X+x, b.Min.Y+y)).(color.NRGBA)
			if w := want(x, y); got != w {
				t.Errorf("pixel (%d,%d) = %v, want %v", x, y, got, w)
			}
		}
	}
}

func opaque(x, y int) color.NRGBA { return testColors[y][x] }

func TestDIBToPNG24(t *testing.T) {
	for _, top := range []bool{false, true} {
		dib := dibSpec{hdr: 40, w: 3, h: 2, topDown: top, bpp: 24, comp: attBIRGB,
			pixel: func(x, y int) uint32 { return bgr(testColors[y][x]) }}.bytes()
		wantPixels(t, decodeDIB(t, dib), opaque)
	}
}

func TestDIBToPNG32(t *testing.T) {
	for _, top := range []bool{false, true} {
		// Alpha 0xff: opaque as drawn.
		dib := dibSpec{hdr: 40, w: 3, h: 2, topDown: top, bpp: 32, comp: attBIRGB,
			pixel: func(x, y int) uint32 { return 0xff000000 | bgr(testColors[y][x]) }}.bytes()
		wantPixels(t, decodeDIB(t, dib), opaque)

		// Alpha left 0 by the writer (the common case): R,G,B > A, so the
		// alpha is garbage and the bitmap is opaque (Chromium's rule).
		dib = dibSpec{hdr: 40, w: 3, h: 2, topDown: top, bpp: 32, comp: attBIRGB,
			pixel: func(x, y int) uint32 { return bgr(testColors[y][x]) }}.bytes()
		wantPixels(t, decodeDIB(t, dib), opaque)
	}
}

// A valid premultiplied 32-bit bitmap keeps its transparency.
func TestDIBToPNG32Premultiplied(t *testing.T) {
	dib := dibSpec{hdr: 40, w: 3, h: 2, bpp: 32, comp: attBIRGB,
		pixel: func(x, y int) uint32 {
			if x == 0 && y == 0 {
				return 0x80800000 // red at alpha 0x80, premultiplied
			}
			if x == 1 && y == 0 {
				return 0 // fully transparent
			}
			return 0xff000000 | bgr(testColors[y][x])
		}}.bytes()
	wantPixels(t, decodeDIB(t, dib), func(x, y int) color.NRGBA {
		switch {
		case x == 0 && y == 0:
			return color.NRGBA{0xff, 0, 0, 0x80}
		case x == 1 && y == 0:
			return color.NRGBA{}
		}
		return testColors[y][x]
	})
}

func TestDIBToPNGBitfields(t *testing.T) {
	bgra := []uint32{0x00ff0000, 0x0000ff00, 0x000000ff}
	rgba := []uint32{0x000000ff, 0x0000ff00, 0x00ff0000} // R in the low byte
	swap := func(c color.NRGBA) uint32 { return uint32(c.B)<<16 | uint32(c.G)<<8 | uint32(c.R) }
	for _, top := range []bool{false, true} {
		// BITMAPINFOHEADER: the three masks follow the header.
		dib := dibSpec{hdr: 40, w: 3, h: 2, topDown: top, bpp: 32, comp: attBIBitfields, masks: rgba,
			pixel: func(x, y int) uint32 { return swap(testColors[y][x]) }}.bytes()
		wantPixels(t, decodeDIB(t, dib), opaque)

		// BITMAPV5HEADER (CF_DIBV5) with an alpha mask: masks in the header.
		half := func(x, y int) color.NRGBA {
			if x == 2 && y == 1 {
				return color.NRGBA{} // transparent black
			}
			return testColors[y][x]
		}
		v5 := dibSpec{hdr: 124, w: 3, h: 2, topDown: top, bpp: 32, comp: attBIBitfields,
			masks: append(append([]uint32{}, bgra...), 0xff000000),
			pixel: func(x, y int) uint32 {
				c := half(x, y)
				return uint32(c.A)<<24 | bgr(c)
			}}
		wantPixels(t, decodeDIB(t, v5.bytes()), half)

		// The same with the masks repeated after the header.
		v5.after = bgra
		wantPixels(t, decodeDIB(t, v5.bytes()), half)

		// V4 header without an alpha mask: the top byte is still alpha.
		dib = dibSpec{hdr: 108, w: 3, h: 2, topDown: top, bpp: 32, comp: attBIBitfields, masks: bgra,
			pixel: func(x, y int) uint32 { return 0xff000000 | bgr(testColors[y][x]) }}.bytes()
		wantPixels(t, decodeDIB(t, dib), opaque)
	}

	// 16 bits per pixel, 5-6-5: channels scale to 8 bits.
	dib := dibSpec{hdr: 40, w: 3, h: 2, bpp: 16, comp: attBIBitfields, masks: []uint32{0xf800, 0x07e0, 0x001f},
		pixel: func(x, y int) uint32 {
			c := testColors[y][x]
			return uint32(c.R>>3)<<11 | uint32(c.G>>2)<<5 | uint32(c.B>>3)
		}}.bytes()
	wantPixels(t, decodeDIB(t, dib), func(x, y int) color.NRGBA {
		c := testColors[y][x]
		s := func(v uint8, bits uint) uint8 {
			m := uint32(1)<<bits - 1
			return uint8((uint32(v>>(8-bits))*255 + m/2) / m)
		}
		return color.NRGBA{s(c.R, 5), s(c.G, 6), s(c.B, 5), 0xff}
	})
}

func TestDIBToPNGPalette(t *testing.T) {
	pal := []color.NRGBA{testColors[0][0], testColors[0][1], testColors[0][2], testColors[1][0], testColors[1][1], testColors[1][2]}
	for _, bpp := range []int{4, 8} {
		dib := dibSpec{hdr: 40, w: 3, h: 2, bpp: bpp, comp: attBIRGB, palette: pal, clrUsed: len(pal),
			pixel: func(x, y int) uint32 { return uint32(y*3 + x) }}.bytes()
		wantPixels(t, decodeDIB(t, dib), opaque)
	}
}

func TestDIBToPNGBad(t *testing.T) {
	good := dibSpec{hdr: 40, w: 3, h: 2, bpp: 24, comp: attBIRGB, pixel: func(x, y int) uint32 { return 0 }}
	cases := map[string][]byte{
		"empty":     nil,
		"header":    good.bytes()[:30],
		"truncated": func() []byte { d := good; d.truncate = 1; return d.bytes() }(),
		"zero":      func() []byte { d := good; d.w = 0; return d.bytes() }(),
		"rle":       func() []byte { d := good; d.bpp, d.comp = 8, 1; return d.bytes() }(),
	}
	for name, b := range cases {
		if _, err := attDIBToPNG(b); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

// BI_PNG: the pixels are a PNG file, passed through.
func TestDIBToPNGEmbedded(t *testing.T) {
	var p bytes.Buffer
	img := image.NewNRGBA(image.Rect(0, 0, 3, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 3; x++ {
			img.SetNRGBA(x, y, testColors[y][x])
		}
	}
	if err := png.Encode(&p, img); err != nil {
		t.Fatal(err)
	}
	hdr := make([]byte, 40)
	binary.LittleEndian.PutUint32(hdr, 40)
	binary.LittleEndian.PutUint32(hdr[4:], 3)
	binary.LittleEndian.PutUint32(hdr[8:], 2)
	binary.LittleEndian.PutUint16(hdr[12:], 1)
	binary.LittleEndian.PutUint32(hdr[16:], attBIPNG)
	got, err := attDIBToPNG(append(hdr, p.Bytes()...))
	if err != nil || !bytes.Equal(got, p.Bytes()) {
		t.Fatalf("BI_PNG: %v, %d bytes, want the embedded PNG", err, len(got))
	}
}

func hdrop(wide bool, paths ...string) []byte {
	b := make([]byte, 20)
	binary.LittleEndian.PutUint32(b, 20)
	if wide {
		binary.LittleEndian.PutUint32(b[16:], 1)
	}
	for _, p := range paths {
		if wide {
			for _, u := range utf16.Encode([]rune(p)) {
				b = binary.LittleEndian.AppendUint16(b, u)
			}
			b = append(b, 0, 0)
		} else {
			b = append(append(b, p...), 0)
		}
	}
	if wide {
		return append(b, 0, 0)
	}
	return append(b, 0)
}

func TestParseHDROP(t *testing.T) {
	paths := []string{`C:\Users\karim\Desktop\a b.png`, `D:\notes\résumé 📎.txt`}
	got := attParseHDROP(hdrop(true, paths...))
	if len(got) != 2 || got[0] != paths[0] || got[1] != paths[1] {
		t.Errorf("wide: %q, want %q", got, paths)
	}
	got = attParseHDROP(hdrop(false, `C:\a.txt`, `C:\b.txt`))
	if len(got) != 2 || got[0] != `C:\a.txt` || got[1] != `C:\b.txt` {
		t.Errorf("ANSI: %q", got)
	}
	if got := attParseHDROP(hdrop(true)); len(got) != 0 {
		t.Errorf("empty list: %q", got)
	}
	if got := attParseHDROP([]byte{1, 2, 3}); got != nil {
		t.Errorf("short: %q", got)
	}
}

// Word, Excel and LibreOffice copies carry rich text next to a picture of
// it: on Windows the format names say so, as the targets do on Linux.
func TestRichCopyWindowsNames(t *testing.T) {
	if !attRichCopy([]string{"DataObject", "Rich Text Format", "HTML Format", "PNG"}) {
		t.Error("Word's copy isn't rich text")
	}
	if attRichCopy([]string{"PNG", "HTML Format"}) {
		t.Error("a browser's Copy image counts as rich text")
	}
}
