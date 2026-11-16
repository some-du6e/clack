package png

// Clack: what the decoder charges its budget for (../CLACK_PATCHES.md).
// This file is Clack's, not upstream's.

// The fixed sizes the decoder charges, at least what Go allocates for each
// (budget_test.go measures them).
const (
	// decoderBytes: the decoder (its 768-byte tmp and 4 KB chunk-skip
	// buffer), its CRC hasher, and the small structs (an image header, an
	// error) a decode makes besides its buffers
	decoderBytes = 8 << 10
	// inflaterBytes: zlib.NewReader of the IDAT stream: the reader, a 4 KB
	// bufio.Reader, the flate decompressor and its 32 KB window, and one
	// block's Huffman link tables. (Each dynamic block's tables replace
	// the last's: garbage, never more than one block's live at once.)
	inflaterBytes = 192 << 10
	// paletteBytes: a 256-entry color.Palette and its boxed colours
	paletteBytes = 256*16 + 256*8
	// configBytes: what DecodeConfig's headers may charge (a palette and
	// its tRNS)
	configBytes = 2 * paletteBytes
)

// pixelBytes is how many bytes a pixel of the image readImagePass makes
// for d's colour type takes: grayscale and truecolor with a tRNS chunk
// decode to NRGBA (8-bit) or NRGBA64 (16-bit), not Gray or RGB.
func (d *decoder) pixelBytes() int64 {
	switch d.cb {
	case cbG1, cbG2, cbG4, cbG8:
		if d.useTransparent {
			return 4 // NRGBA
		}
		return 1 // Gray
	case cbP1, cbP2, cbP4, cbP8:
		return 1 // Paletted
	case cbGA8, cbTC8, cbTCA8:
		return 4 // NRGBA, or RGBA
	case cbG16:
		if d.useTransparent {
			return 8 // NRGBA64
		}
		return 2 // Gray16
	}
	return 8 // cbGA16, cbTC16, cbTCA16: NRGBA64, or RGBA64
}
