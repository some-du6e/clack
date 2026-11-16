package jpeg

// Clack: what the decoder charges its budget for (../CLACK_PATCHES.md).
// This file is Clack's, not upstream's.

import (
	"image"
	"unsafe"
)

// charge takes n bytes from the decode's budget, before they're allocated.
func (d *decoder) charge(n int64) error { return d.budget.Charge(n) }

// The sizes the decoder charges, at least what Go allocates for each
// (budget_test.go checks them against unsafe.Sizeof).
const (
	// decoderBytes: the decoder itself (its 4 KB byte buffer, the Huffman
	// and quantization tables) and the small structs (images' headers, a
	// SubImage) a decode makes besides its buffers
	decoderBytes = 16 << 10
	// blockBytes: one 8×8 block of progressive coefficients
	blockBytes = int64(unsafe.Sizeof(block{}))
)

// rgbaBytes is what image.NewRGBA (or NewCMYK) allocates for r: 4 bytes a
// pixel.
func rgbaBytes(r image.Rectangle) int64 { return 4 * int64(r.Dx()) * int64(r.Dy()) }

// ycbcrBytes is what image.NewYCbCr allocates for r at ratio: the Y plane
// and two chroma planes, sized as image's yCbCrSize does.
func ycbcrBytes(r image.Rectangle, ratio image.YCbCrSubsampleRatio) int64 {
	w, h := r.Dx(), r.Dy()
	cw, ch := w, h
	switch ratio {
	case image.YCbCrSubsampleRatio422:
		cw = (r.Max.X+1)/2 - r.Min.X/2
	case image.YCbCrSubsampleRatio420:
		cw = (r.Max.X+1)/2 - r.Min.X/2
		ch = (r.Max.Y+1)/2 - r.Min.Y/2
	case image.YCbCrSubsampleRatio440:
		ch = (r.Max.Y+1)/2 - r.Min.Y/2
	case image.YCbCrSubsampleRatio411:
		cw = (r.Max.X+3)/4 - r.Min.X/4
	case image.YCbCrSubsampleRatio410:
		cw = (r.Max.X+3)/4 - r.Min.X/4
		ch = (r.Max.Y+1)/2 - r.Min.Y/2
	}
	return int64(w)*int64(h) + 2*int64(cw)*int64(ch)
}
