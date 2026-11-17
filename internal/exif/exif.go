// Package exif reads the one EXIF field Clack applies: a JPEG's
// Orientation, tag 0x0112 in IFD0 of its APP1 "Exif" segment. Chromium
// applies it to an <img> (image-orientation: from-image, the default since
// Chrome 81): the image draws turned, and naturalWidth and naturalHeight
// report the turned size (JPEGImageDecoder reads the first Exif APP1;
// values outside 1 to 8 are the default). PNG eXIf and WebP EXIF chunks
// aren't read here.
package exif

import "encoding/binary"

// Orientation is data's EXIF Orientation, 1 to 8 (1 is upright): the one
// in the first APP1 Exif segment before the image data. 1 when data isn't
// a JPEG, has no such segment or tag, or the value is out of range.
func Orientation(data []byte) int {
	if len(data) < 4 || data[0] != 0xff || data[1] != 0xd8 {
		return 1
	}
	for i := 2; i+4 <= len(data); {
		if data[i] != 0xff {
			return 1
		}
		m := data[i+1]
		switch {
		case m == 0xff: // fill byte before a marker
			i++
			continue
		case m == 0xd8 || m == 0x01 || m >= 0xd0 && m <= 0xd7: // no length
			i += 2
			continue
		case m == 0xda || m == 0xd9: // start of scan, end of image: no Exif before them
			return 1
		}
		n := int(binary.BigEndian.Uint16(data[i+2:]))
		if n < 2 || i+2+n > len(data) {
			return 1
		}
		seg := data[i+4 : i+2+n]
		if m == 0xe1 && len(seg) >= 6 && string(seg[:6]) == "Exif\x00\x00" {
			return tiffOrientation(seg[6:])
		}
		i += 2 + n
	}
	return 1
}

// tiffOrientation reads IFD0's Orientation (a SHORT) from a TIFF header
// and its directory.
func tiffOrientation(t []byte) int {
	if len(t) < 8 {
		return 1
	}
	var bo binary.ByteOrder
	switch string(t[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return 1
	}
	if bo.Uint16(t[2:]) != 42 {
		return 1
	}
	off := int(bo.Uint32(t[4:]))
	if off < 8 || off+2 > len(t) {
		return 1
	}
	n := int(bo.Uint16(t[off:]))
	for k := 0; k < n; k++ {
		e := off + 2 + 12*k
		if e+12 > len(t) {
			return 1
		}
		if bo.Uint16(t[e:]) != 0x0112 {
			continue
		}
		if bo.Uint16(t[e+2:]) != 3 || bo.Uint32(t[e+4:]) != 1 { // SHORT, one value
			return 1
		}
		if v := int(bo.Uint16(t[e+8:])); v >= 1 && v <= 8 {
			return v
		}
		return 1
	}
	return 1
}

// Swaps reports whether orientation o turns the image a quarter (5 to 8),
// so the image shows with its width and height traded.
func Swaps(o int) bool { return o >= 5 && o <= 8 }

// Source is where orientation o draws the shown pixel (x, y) from, in an
// image stored w×h (the shown image is h×w when Swaps(o)).
func Source(o, w, h, x, y int) (sx, sy int) {
	switch o {
	case 2: // mirrored
		return w - 1 - x, y
	case 3: // turned 180°
		return w - 1 - x, h - 1 - y
	case 4: // mirrored vertically
		return x, h - 1 - y
	case 5: // transposed
		return y, x
	case 6: // turned 90° clockwise to show
		return y, h - 1 - x
	case 7: // transversed
		return w - 1 - y, h - 1 - x
	case 8: // turned 90° counter-clockwise to show
		return w - 1 - y, x
	}
	return x, y
}

// Segment is an APP1 Exif segment holding only an Orientation of o, to put
// after a JPEG's SOI (tests and the fake's prepared photos).
func Segment(o int) []byte {
	tiff := []byte{'M', 'M', 0, 42, 0, 0, 0, 8, // big-endian, IFD0 at 8
		0, 1, // one entry
		0x01, 0x12, 0, 3, 0, 0, 0, 1, byte(o >> 8), byte(o), 0, 0, // Orientation, SHORT, 1 value
		0, 0, 0, 0} // no next IFD
	payload := append([]byte("Exif\x00\x00"), tiff...)
	n := len(payload) + 2
	return append([]byte{0xff, 0xe1, byte(n >> 8), byte(n)}, payload...)
}

// WithOrientation is jpeg with Segment(o) inserted after its SOI.
func WithOrientation(jpeg []byte, o int) []byte {
	if len(jpeg) < 2 {
		return jpeg
	}
	out := append([]byte{}, jpeg[:2]...)
	out = append(out, Segment(o)...)
	return append(out, jpeg[2:]...)
}
