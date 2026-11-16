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
	// block16Bytes: one 8×8 block of progressive coefficients, as kept
	block16Bytes = int64(unsafe.Sizeof(block16{}))
)

// block16 is a block of progressive coefficients as the decoder keeps them
// between scans: int16, as libjpeg's JCOEF. A valid 8-bit JPEG's quantized
// coefficients take 11 bits and a sign, so they fit; the arithmetic is
// done on a block (int32), as upstream does it.
type block16 [blockSize]int16

func (c *block16) widen() (b block) {
	for i, v := range c {
		b[i] = int32(v)
	}
	return b
}

func (b *block) narrow() (c block16) {
	for i, v := range b {
		c[i] = int16(v)
	}
	return c
}

// freeCoeffs gives the progressive coefficients back to the budget: once
// reconstructProgressiveImage has written the image from them, nothing
// reads them again.
func (d *decoder) freeCoeffs() {
	if d.coeffBytes == 0 {
		return
	}
	d.progCoeffs = [maxComponents][]block16{}
	d.budget.Free(d.coeffBytes)
	d.coeffBytes = 0
}

// freePlanes gives the YCbCr image and the black plane back to the budget
// once applyBlack or convertToRGB has made the image returned from them.
func (d *decoder) freePlanes() {
	d.img3, d.blackPix = nil, nil
	d.budget.Free(d.imgBytes + d.blackBytes)
	d.imgBytes, d.blackBytes = 0, 0
}

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
