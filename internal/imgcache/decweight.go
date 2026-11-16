package imgcache

// Memory weights for a decode, computed from the image header so a
// reservation matches what the stdlib (or vendored) decoder really
// allocates. The cap (declimit.go) only bounds total decode memory if each
// reservation covers its decode's peak; 4·w·h badly under-counts
// progressive and CMYK JPEG (15-24 B/px) and 16-bit or interlaced PNG
// (8-16 B/px). These figures track the allocations in Go 1.24's
// image/jpeg and image/png (makeImg, progCoeffs, blackPix, applyBlack; the
// per-colour-type image and the Adam7 pass images) and x/image/bmp, so a
// test can check them against measured TotalAlloc.
//
// imgcache decodes only the formats Slack serves (PNG, JPEG, GIF, BMP,
// WebP). Anything else — TIFF above all, which is linked only because
// go-text/typesetting reads sbix font glyphs with it — is rejected rather
// than fed to a decoder whose allocation this can't bound from the header.

import (
	"encoding/binary"
	"image"
	"image/color"
)

// decodeWeight is the memory a decode of data may allocate at its peak,
// from the header. ok is false when the format isn't one imgcache decodes
// or the header can't be parsed; the caller then rejects data (at large
// sizes) rather than decode it unbounded. cfg and format come from
// image.DecodeConfig (format "" for the vendored WebP, handled by caller).
func decodeWeight(data []byte, cfg image.Config, format string) (int64, bool) {
	switch format {
	case "jpeg":
		return jpegWeight(data)
	case "png":
		return pngWeight(data)
	case "gif":
		// image.Decode returns the first frame, a Paletted of the logical
		// screen (1 B/px) plus a ≤256-colour palette and a like-sized decode
		// buffer. (Animated GIFs in the cache take the indexed path, not this.)
		return 2*px(cfg.Width, cfg.Height) + smallConst, true
	case "bmp":
		return modelBytes(cfg.ColorModel)*px(cfg.Width, cfg.Height) + smallConst, true
	}
	return 0, false
}

// resultBytes is the size of the image image.Decode returns, which stays
// live while the caller scales it or copies it to the clipboard. It is the
// final image only, not the transient decode peak (progressive coefficient
// blocks and the like are freed when Decode returns).
func resultBytes(data []byte, cfg image.Config, format string) int64 {
	switch format {
	case "jpeg":
		if w, h, n, _, comps, ok := jpegSOF(data); ok {
			if n == 4 {
				return 4 * px(w, h) // applyBlack returns an RGBA or CMYK
			}
			if n == 1 {
				return grayBytes(w, h)
			}
			return ycbcrBytes(w, h, comps)
		}
	case "png":
		if w, h, bpp, _, ok := pngIHDR(data); ok {
			return int64(bpp) * px(w, h)
		}
	case "gif":
		return px(cfg.Width, cfg.Height) + smallConst
	}
	return modelBytes(cfg.ColorModel) * px(cfg.Width, cfg.Height)
}

// px is w×h as an int64, never negative.
func px(w, h int) int64 {
	if w <= 0 || h <= 0 {
		return 0
	}
	return int64(w) * int64(h)
}

// smallConst covers a decode's fixed small structs (readers, headers,
// palettes, per-row scratch) that aren't proportional to the image.
const smallConst = 1 << 16

// modelBytes is the bytes per pixel of an image of colour model cm.
func modelBytes(cm color.Model) int64 {
	switch cm {
	case color.GrayModel, color.AlphaModel:
		return 1
	case color.Gray16Model, color.Alpha16Model:
		return 2
	case color.RGBAModel, color.NRGBAModel, color.CMYKModel:
		return 4
	case color.RGBA64Model, color.NRGBA64Model:
		return 8
	case color.YCbCrModel:
		return 3 // 4:4:4 upper bound
	case color.NYCbCrAModel:
		return 4
	}
	if _, ok := cm.(color.Palette); ok {
		return 1
	}
	return 4 // unknown: the common RGBA/NRGBA case
}

// ---- JPEG ----

// jpegSOF parses the first SOF0/SOF1/SOF2 frame header. It returns the
// width, height, component count, whether the frame is progressive (SOF2),
// and each component's (h, v) sampling factors. ok is false for a header
// Go's decoder wouldn't accept (precision ≠ 8, an unsupported component
// count, arithmetic coding) or a truncated one.
func jpegSOF(data []byte) (w, h, nComp int, progressive bool, comps [4][2]int, ok bool) {
	if len(data) < 2 || data[0] != 0xff || data[1] != 0xd8 { // SOI
		return
	}
	p := 2
	for p+4 <= len(data) {
		if data[p] != 0xff {
			return // not at a marker
		}
		m := data[p+1]
		p += 2
		if m == 0xd9 || m == 0x01 || (0xd0 <= m && m <= 0xd7) { // EOI, TEM, RSTn: no length
			continue
		}
		if p+2 > len(data) {
			return
		}
		n := int(binary.BigEndian.Uint16(data[p:]))
		if n < 2 || p+n > len(data) {
			return
		}
		seg := data[p+2 : p+n]
		switch m {
		case 0xc0, 0xc1, 0xc2: // SOF0 baseline, SOF1 extended, SOF2 progressive
			if len(seg) < 6 {
				return
			}
			if seg[0] != 8 { // Go supports 8-bit precision only
				return
			}
			h = int(seg[1])<<8 | int(seg[2])
			w = int(seg[3])<<8 | int(seg[4])
			nComp = int(seg[5])
			if nComp != 1 && nComp != 3 && nComp != 4 {
				return
			}
			if len(seg) < 6+3*nComp {
				return
			}
			for i := 0; i < nComp; i++ {
				hv := seg[7+3*i]
				ch, cv := int(hv>>4), int(hv&0x0f)
				if ch < 1 || ch > 4 || cv < 1 || cv > 4 {
					return
				}
				comps[i] = [2]int{ch, cv}
			}
			if nComp == 1 {
				comps[0] = [2]int{1, 1} // non-interleaved: effectively 1×1 (processSOF)
			}
			progressive = m == 0xc2
			if w <= 0 || h <= 0 {
				return
			}
			ok = true
			return
		}
		p += n
	}
	return
}

// jpegWeight is the peak a JPEG decode allocates: the YCbCr/Gray image
// (makeImg, by subsampling), a 4th-component plane (blackPix), the
// progressive coefficient blocks (256 B per 8×8 per component), and the
// CMYK/RGBA image applyBlack builds for a 4-component decode.
func jpegWeight(data []byte) (int64, bool) {
	w, h, nComp, progressive, comps, ok := jpegSOF(data)
	if !ok {
		return 0, false
	}
	h0, v0 := comps[0][0], comps[0][1]
	mxx := int64((w + 8*h0 - 1) / (8 * h0))
	myy := int64((h + 8*v0 - 1) / (8 * v0))
	var weight int64
	if nComp == 1 {
		weight = (8 * mxx) * (8 * myy) // NewGray(8*mxx, 8*myy)
	} else {
		weight = ycbcrBytes(w, h, comps)
	}
	if nComp == 4 {
		h3, v3 := comps[3][0], comps[3][1]
		weight += (8 * int64(h3) * mxx) * (8 * int64(v3) * myy) // blackPix
		weight += 4 * px(w, h)                                  // applyBlack: NewCMYK or NewRGBA
	}
	if progressive {
		for i := 0; i < nComp; i++ {
			weight += mxx * myy * int64(comps[i][0]) * int64(comps[i][1]) * 256 // []block, block=[64]int32
		}
	}
	return weight + smallConst, true
}

// ycbcrBytes is image.NewYCbCr's Pix length for a JPEG of these components:
// the padded (full-MCU) luma plane plus two chroma planes sized by the
// subsampling ratio.
func ycbcrBytes(w, h int, comps [4][2]int) int64 {
	h0, v0 := comps[0][0], comps[0][1]
	h1, v1 := comps[1][0], comps[1][1]
	if h1 == 0 || v1 == 0 {
		h1, v1 = h0, v0
	}
	mxx := int64((w + 8*h0 - 1) / (8 * h0))
	myy := int64((h + 8*v0 - 1) / (8 * v0))
	pw, ph := 8*int64(h0)*mxx, 8*int64(v0)*myy // padded luma dims
	hr, vr := h0/h1, v0/v1
	cw, cch := pw, ph
	switch {
	case hr >= 4:
		cw = (pw + 3) / 4
	case hr == 2:
		cw = (pw + 1) / 2
	}
	switch {
	case vr >= 2:
		cch = (ph + 1) / 2
	}
	return pw*ph + 2*cw*cch
}

// grayBytes is image.NewGray's Pix length for a 1-component JPEG (padded to
// whole 8×8 MCUs, h0=v0=1).
func grayBytes(w, h int) int64 {
	mxx := int64((w + 7) / 8)
	myy := int64((h + 7) / 8)
	return (8 * mxx) * (8 * myy)
}

// ---- PNG ----

// pngIHDR reads a PNG's IHDR: dimensions, the decoded image's bytes per
// pixel (by bit depth and colour type, matching image/png's output image),
// and whether it is Adam7-interlaced. ok is false for a malformed or
// unsupported header.
func pngIHDR(data []byte) (w, h, bpp int, interlaced bool, ok bool) {
	// 8-byte signature, then an IHDR chunk: 4-byte length, "IHDR", 13-byte body.
	const sig = "\x89PNG\r\n\x1a\n"
	if len(data) < 8+8+13 || string(data[:8]) != sig || string(data[12:16]) != "IHDR" {
		return
	}
	b := data[16:29]
	w = int(binary.BigEndian.Uint32(b[0:4]))
	h = int(binary.BigEndian.Uint32(b[4:8]))
	depth := int(b[8])
	colorType := int(b[9])
	interlaced = b[12] == 1
	if w <= 0 || h <= 0 {
		return
	}
	switch colorType {
	case 0: // grayscale
		if depth == 16 {
			bpp = 2 // Gray16
		} else {
			bpp = 1 // Gray
		}
	case 2: // truecolor
		if depth == 16 {
			bpp = 8 // RGBA64
		} else {
			bpp = 4 // RGBA
		}
	case 3: // paletted
		bpp = 1 // Paletted
	case 4: // grayscale+alpha
		if depth == 16 {
			bpp = 8 // NRGBA64
		} else {
			bpp = 4 // NRGBA
		}
	case 6: // truecolor+alpha
		if depth == 16 {
			bpp = 8 // NRGBA64
		} else {
			bpp = 4 // NRGBA
		}
	default:
		return
	}
	ok = true
	return
}

// pngWeight is the peak a PNG decode allocates: the decoded image (bytes
// per pixel by depth and colour type), doubled for an interlaced image,
// which allocates the full image plus one Adam7 pass image at a time (the
// passes together are about the image's size again).
func pngWeight(data []byte) (int64, bool) {
	w, h, bpp, interlaced, ok := pngIHDR(data)
	if !ok {
		return 0, false
	}
	n := int64(bpp) * px(w, h)
	if interlaced {
		n *= 2
	}
	return n + smallConst, true
}
