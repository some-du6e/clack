package composer

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/png"
	"math/bits"
	"unicode/utf16"
)

// Windows clipboard data, decoded on every platform so the conversions are
// tested on linux CI. attach_windows.go reads the bytes.

// DIB compressions (wingdi.h).
const (
	attBIRGB            = 0
	attBIBitfields      = 3
	attBIPNG            = 5
	attBIAlphaBitfields = 6
)

var errAttDIB = errors.New("the clipboard's bitmap is in a format Clack can't read")

// attDIBToPNG turns a packed DIB (CF_DIB or CF_DIBV5: a BITMAPINFOHEADER,
// BITMAPV4HEADER or BITMAPV5HEADER, then masks or a colour table, then the
// pixels) into a PNG, as Chromium's ClipboardWin::ReadPng does when the
// clipboard has no "PNG" format.
func attDIBToPNG(dib []byte) ([]byte, error) {
	if len(dib) >= 40 && binary.LittleEndian.Uint32(dib[16:]) == attBIPNG {
		// The pixels are a whole PNG file.
		hs := int(binary.LittleEndian.Uint32(dib))
		if hs < 40 || hs > len(dib) || !bytes.HasPrefix(dib[hs:], []byte("\x89PNG\r\n\x1a\n")) {
			return nil, errAttDIB
		}
		return dib[hs:], nil
	}
	img, err := attDIBImage(dib)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// attDIBImage decodes a packed DIB. Rows are bottom-up unless the height is
// negative. Like Chromium (ClipboardWin::ReadBitmapInternal), a bitmap
// under 32 bits per pixel is opaque, and a 32-bit one's alpha is taken as
// premultiplied unless some pixel has a colour channel above its alpha:
// then the alpha is garbage (many apps leave it 0) and the bitmap is
// opaque too.
func attDIBImage(dib []byte) (image.Image, error) {
	le := binary.LittleEndian
	if len(dib) < 40 {
		return nil, errAttDIB
	}
	hs := int(le.Uint32(dib))
	if hs < 40 || hs > len(dib) {
		return nil, errAttDIB
	}
	w := int(int32(le.Uint32(dib[4:])))
	h := int(int32(le.Uint32(dib[8:])))
	bpp := int(le.Uint16(dib[14:]))
	comp := le.Uint32(dib[16:])
	clrUsed := int(le.Uint32(dib[32:]))
	topDown := h < 0
	if topDown {
		h = -h
	}
	if w <= 0 || h <= 0 || w > 1<<16 || h > 1<<16 || int64(w)*int64(h) > 1<<27 {
		return nil, errAttDIB
	}
	stride := (w*bpp + 31) / 32 * 4
	size := stride * h

	off := hs
	var masks [4]uint32 // r, g, b, a
	var palette []color.NRGBA
	switch {
	case bpp == 1 || bpp == 4 || bpp == 8:
		if comp != attBIRGB {
			return nil, errAttDIB // RLE
		}
		n := clrUsed
		if n == 0 || n > 1<<bpp {
			n = 1 << bpp
		}
		if off+4*n > len(dib) {
			return nil, errAttDIB
		}
		for i := 0; i < n; i++ {
			q := dib[off+4*i:]
			palette = append(palette, color.NRGBA{q[2], q[1], q[0], 0xff})
		}
		off += 4 * n
	case bpp == 16 || bpp == 24 || bpp == 32:
		switch comp {
		case attBIRGB:
			if bpp == 16 {
				masks = [4]uint32{0x7c00, 0x03e0, 0x001f, 0}
			} else {
				masks = [4]uint32{0xff0000, 0xff00, 0xff, 0}
			}
			if bpp == 32 {
				masks[3] = 0xff000000
			}
		case attBIBitfields, attBIAlphaBitfields:
			if bpp == 24 {
				return nil, errAttDIB
			}
			n := 3
			if comp == attBIAlphaBitfields {
				n = 4
			}
			if hs == 40 {
				// BITMAPINFOHEADER: the masks follow the header.
				if off+4*n > len(dib) {
					return nil, errAttDIB
				}
				for i := 0; i < n; i++ {
					masks[i] = le.Uint32(dib[off+4*i:])
				}
				off += 4 * n
			} else {
				// V2 and later carry the masks (V3 and later the alpha
				// mask too). Some writers repeat the three after the
				// header anyway; the pixels are then 12 bytes later.
				for i := 0; i < 4 && 40+4*i+4 <= hs; i++ {
					masks[i] = le.Uint32(dib[40+4*i:])
				}
				if len(dib) == off+12+size {
					off += 12
				}
			}
			if bpp == 32 && masks[3] == 0 && masks[0]|masks[1]|masks[2] == 0xffffff {
				// No alpha mask: the top byte is alpha, as in BI_RGB.
				masks[3] = 0xff000000
			}
		default:
			return nil, errAttDIB
		}
	default:
		return nil, errAttDIB
	}
	if off+size > len(dib) {
		return nil, errAttDIB
	}
	px := dib[off : off+size]
	row := func(y int) []byte {
		if topDown {
			return px[y*stride:]
		}
		return px[(h-1-y)*stride:]
	}

	if palette != nil {
		img := image.NewNRGBA(image.Rect(0, 0, w, h))
		for y := 0; y < h; y++ {
			r := row(y)
			for x := 0; x < w; x++ {
				bit := x * bpp
				i := int(r[bit/8]>>(8-bpp-bit%8)) & (1<<bpp - 1)
				c := color.NRGBA{A: 0xff}
				if i < len(palette) {
					c = palette[i]
				}
				img.SetNRGBA(x, y, c)
			}
		}
		return img, nil
	}

	var ch [4]attMask
	for i, m := range masks {
		ch[i] = attMaskOf(m)
	}
	bytesPP := bpp / 8
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	valid := bpp == 32 && masks[3] != 0
	for y := 0; y < h; y++ {
		r := row(y)
		o := img.Pix[y*img.Stride:]
		for x := 0; x < w; x++ {
			var v uint32
			for k := 0; k < bytesPP; k++ {
				v |= uint32(r[x*bytesPP+k]) << (8 * k)
			}
			c := [4]uint8{ch[0].get(v), ch[1].get(v), ch[2].get(v), 0xff}
			if masks[3] != 0 {
				c[3] = ch[3].get(v)
				if c[0] > c[3] || c[1] > c[3] || c[2] > c[3] {
					valid = false
				}
			}
			copy(o[x*4:x*4+4], c[:])
		}
	}
	if !valid {
		for i := 3; i < len(img.Pix); i += 4 {
			img.Pix[i] = 0xff
		}
	}
	return img, nil
}

// attMask reads one channel out of a pixel and scales it to 8 bits.
type attMask struct {
	mask  uint32
	shift int
	max   uint32
}

func attMaskOf(m uint32) attMask {
	if m == 0 {
		return attMask{}
	}
	s := bits.TrailingZeros32(m)
	return attMask{mask: m, shift: s, max: m >> s}
}

func (m attMask) get(v uint32) uint8 {
	if m.max == 0 {
		return 0
	}
	x := (v & m.mask) >> m.shift
	return uint8((uint64(x)*255 + uint64(m.max)/2) / uint64(m.max))
}

// attParseHDROP reads the paths out of a CF_HDROP (a DROPFILES header, then
// the paths, each NUL-terminated, then one more NUL), as DragQueryFileW
// would.
func attParseHDROP(b []byte) []string {
	le := binary.LittleEndian
	if len(b) < 20 {
		return nil
	}
	off := int(le.Uint32(b))
	wide := le.Uint32(b[16:]) != 0
	if off < 20 || off > len(b) {
		return nil
	}
	var paths []string
	if wide {
		var cur []uint16
		for i := off; i+1 < len(b); i += 2 {
			c := le.Uint16(b[i:])
			if c != 0 {
				cur = append(cur, c)
				continue
			}
			if len(cur) == 0 {
				break
			}
			paths = append(paths, string(utf16.Decode(cur)))
			cur = cur[:0]
		}
		return paths
	}
	start := off
	for i := off; i < len(b); i++ {
		if b[i] != 0 {
			continue
		}
		if i == start {
			break
		}
		paths = append(paths, string(b[start:i]))
		start = i + 1
	}
	return paths
}
