// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package webp implements a decoder for WEBP images.
//
// WEBP is defined at:
// https://developers.google.com/speed/webp/docs/riff_container
//
// Clack: this is golang.org/x/image/webp v0.33.0's decoder with a
// vp8l.Budget that every allocation of a decode charges first, its
// lossless pictures and alpha planes decoded by the budgeted vp8l in
// ../vp8l, and no image.RegisterFormat (CLACK_PATCHES.md).
package webp

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"io"

	"golang.org/x/image/riff"
	"golang.org/x/image/vp8"

	"github.com/some-du6e/clack/internal/imgcache/internal/vp8l"
)

// Clack: containerBytes is what decode allocates besides its pictures:
// the RIFF reader and its chunk readers, and the image header it returns.
const containerBytes = 1 << 10

// Clack: what decode charges for the allocations vp8 makes by a lossy
// frame's header before it reads the frame: the vp8.Decoder (its
// probability tables and macroblock workspace), and, for a w×h frame of
// mbw×mbh 16-pixel macroblocks, the 4:2:0 YCbCr image in whole macroblocks
// (384 bytes each), a 4-byte loop-filter setting per macroblock and the
// 6-byte state of each column's macroblock above. vp8 also copies the
// frame's partitions out of the chunk, at most the chunk's own length (and
// 16 MiB a partition): bounded by the input, not amplified, and not
// charged.
const vp8DecoderBytes = 8 << 10

func vp8FrameBytes(w, h int) int64 {
	mbw, mbh := int64(w+15)>>4, int64(h+15)>>4
	return 388*mbw*mbh + 6*mbw + 1<<10
}

var errInvalidFormat = errors.New("webp: invalid format")

var (
	fccALPH = riff.FourCC{'A', 'L', 'P', 'H'}
	fccVP8  = riff.FourCC{'V', 'P', '8', ' '}
	fccVP8L = riff.FourCC{'V', 'P', '8', 'L'}
	fccVP8X = riff.FourCC{'V', 'P', '8', 'X'}
	fccWEBP = riff.FourCC{'W', 'E', 'B', 'P'}
)

func decode(r io.Reader, configOnly bool, b *vp8l.Budget) (image.Image, image.Config, error) {
	// Clack: the RIFF reader, its chunk readers and the image header
	if err := b.Charge(containerBytes); err != nil {
		return nil, image.Config{}, err
	}
	formType, riffReader, err := riff.NewReader(r)
	if err != nil {
		return nil, image.Config{}, err
	}
	if formType != fccWEBP {
		return nil, image.Config{}, errInvalidFormat
	}

	var (
		alpha          []byte
		alphaStride    int
		wantAlpha      bool
		seenVP8X       bool
		widthMinusOne  uint32
		heightMinusOne uint32
		buf            [10]byte
	)
	for {
		chunkID, chunkLen, chunkData, err := riffReader.Next()
		if err == io.EOF {
			err = errInvalidFormat
		}
		if err != nil {
			return nil, image.Config{}, err
		}

		switch chunkID {
		case fccALPH:
			if !wantAlpha {
				return nil, image.Config{}, errInvalidFormat
			}
			wantAlpha = false
			// Read the Pre-processing | Filter | Compression byte.
			if _, err := io.ReadFull(chunkData, buf[:1]); err != nil {
				if err == io.EOF {
					err = errInvalidFormat
				}
				return nil, image.Config{}, err
			}
			alpha, alphaStride, err = readAlpha(chunkData, widthMinusOne, heightMinusOne, buf[0]&0x03, b)
			if err != nil {
				return nil, image.Config{}, err
			}
			unfilterAlpha(alpha, alphaStride, (buf[0]>>2)&0x03)

		case fccVP8:
			if wantAlpha || int32(chunkLen) < 0 {
				return nil, image.Config{}, errInvalidFormat
			}
			if err := b.Charge(vp8DecoderBytes); err != nil { // Clack
				return nil, image.Config{}, err
			}
			d := vp8.NewDecoder()
			d.Init(chunkData, int(chunkLen))
			fh, err := d.DecodeFrameHeader()
			if err != nil {
				return nil, image.Config{}, err
			}
			if configOnly {
				return nil, image.Config{
					ColorModel: color.YCbCrModel,
					Width:      fh.Width,
					Height:     fh.Height,
				}, nil
			}
			// Clack: DecodeFrame allocates the frame by its header's size first
			if err := b.Charge(vp8FrameBytes(fh.Width, fh.Height)); err != nil {
				return nil, image.Config{}, err
			}
			m, err := d.DecodeFrame()
			if err != nil {
				return nil, image.Config{}, err
			}
			if alpha != nil {
				return &image.NYCbCrA{
					YCbCr:   *m,
					A:       alpha,
					AStride: alphaStride,
				}, image.Config{}, nil
			}
			return m, image.Config{}, nil

		case fccVP8L:
			if wantAlpha || alpha != nil {
				return nil, image.Config{}, errInvalidFormat
			}
			if configOnly {
				c, err := vp8l.DecodeConfig(chunkData)
				return nil, c, err
			}
			m, err := vp8l.Decode(chunkData, b)
			return m, image.Config{}, err

		case fccVP8X:
			if seenVP8X {
				return nil, image.Config{}, errInvalidFormat
			}
			seenVP8X = true
			if chunkLen != 10 {
				return nil, image.Config{}, errInvalidFormat
			}
			if _, err := io.ReadFull(chunkData, buf[:10]); err != nil {
				return nil, image.Config{}, err
			}
			const (
				animationBit    = 1 << 1
				xmpMetadataBit  = 1 << 2
				exifMetadataBit = 1 << 3
				alphaBit        = 1 << 4
				iccProfileBit   = 1 << 5
			)
			wantAlpha = (buf[0] & alphaBit) != 0
			widthMinusOne = uint32(buf[4]) | uint32(buf[5])<<8 | uint32(buf[6])<<16
			heightMinusOne = uint32(buf[7]) | uint32(buf[8])<<8 | uint32(buf[9])<<16
			if configOnly {
				if wantAlpha {
					return nil, image.Config{
						ColorModel: color.NYCbCrAModel,
						Width:      int(widthMinusOne) + 1,
						Height:     int(heightMinusOne) + 1,
					}, nil
				}
				return nil, image.Config{
					ColorModel: color.YCbCrModel,
					Width:      int(widthMinusOne) + 1,
					Height:     int(heightMinusOne) + 1,
				}, nil
			}
		}
	}
}

func readAlpha(chunkData io.Reader, widthMinusOne, heightMinusOne uint32, compression byte, b *vp8l.Budget) (
	alpha []byte, alphaStride int, err error) {

	switch compression {
	case 0:
		w := int(widthMinusOne) + 1
		h := int(heightMinusOne) + 1
		if err := b.Charge(int64(w) * int64(h)); err != nil { // Clack
			return nil, 0, err
		}
		alpha = make([]byte, w*h)
		if _, err := io.ReadFull(chunkData, alpha); err != nil {
			return nil, 0, err
		}
		return alpha, w, nil

	case 1:
		// Read the VP8L-compressed alpha values. First, synthesize a 5-byte VP8L header:
		// a 1-byte magic number, a 14-bit widthMinusOne, a 14-bit heightMinusOne,
		// a 1-bit (ignored, zero) alphaIsUsed and a 3-bit (zero) version.
		// TODO(nigeltao): be more efficient than decoding an *image.NRGBA just to
		// extract the green values to a separately allocated []byte. Fixing this
		// will require changes to the vp8l package's API.
		if widthMinusOne > 0x3fff || heightMinusOne > 0x3fff {
			return nil, 0, errors.New("webp: invalid format")
		}
		alphaImage, err := vp8l.Decode(io.MultiReader(
			bytes.NewReader([]byte{
				0x2f, // VP8L magic number.
				uint8(widthMinusOne),
				uint8(widthMinusOne>>8) | uint8(heightMinusOne<<6),
				uint8(heightMinusOne >> 2),
				uint8(heightMinusOne >> 10),
			}),
			chunkData,
		), b)
		if err != nil {
			return nil, 0, err
		}
		// The green values of the inner NRGBA image are the alpha values of the
		// outer NYCbCrA image.
		pix := alphaImage.(*image.NRGBA).Pix
		if err := b.Charge(int64(len(pix) / 4)); err != nil { // Clack
			return nil, 0, err
		}
		alpha = make([]byte, len(pix)/4)
		for i := range alpha {
			alpha[i] = pix[4*i+1]
		}
		return alpha, int(widthMinusOne) + 1, nil
	}
	return nil, 0, errInvalidFormat
}

func unfilterAlpha(alpha []byte, alphaStride int, filter byte) {
	if len(alpha) == 0 || alphaStride == 0 {
		return
	}
	switch filter {
	case 1: // Horizontal filter.
		for i := 1; i < alphaStride; i++ {
			alpha[i] += alpha[i-1]
		}
		for i := alphaStride; i < len(alpha); i += alphaStride {
			// The first column is equivalent to the vertical filter.
			alpha[i] += alpha[i-alphaStride]

			for j := 1; j < alphaStride; j++ {
				alpha[i+j] += alpha[i+j-1]
			}
		}

	case 2: // Vertical filter.
		// The first row is equivalent to the horizontal filter.
		for i := 1; i < alphaStride; i++ {
			alpha[i] += alpha[i-1]
		}

		for i := alphaStride; i < len(alpha); i++ {
			alpha[i] += alpha[i-alphaStride]
		}

	case 3: // Gradient filter.
		// The first row is equivalent to the horizontal filter.
		for i := 1; i < alphaStride; i++ {
			alpha[i] += alpha[i-1]
		}

		for i := alphaStride; i < len(alpha); i += alphaStride {
			// The first column is equivalent to the vertical filter.
			alpha[i] += alpha[i-alphaStride]

			// The interior is predicted on the three top/left pixels.
			for j := 1; j < alphaStride; j++ {
				c := int(alpha[i+j-alphaStride-1])
				b := int(alpha[i+j-alphaStride])
				a := int(alpha[i+j-1])
				x := a + b - c
				if x < 0 {
					x = 0
				} else if x > 255 {
					x = 255
				}
				alpha[i+j] += uint8(x)
			}
		}
	}
}

// Decode reads a WEBP image from r and returns it as an image.Image.
//
// Clack: everything the decode allocates is charged to b first (and to
// the budgeted vp8l for a lossless picture or alpha plane); a file whose
// decode would need more than b allows fails with an error wrapping
// vp8l.ErrBudget before the allocation that doesn't fit.
func Decode(r io.Reader, b *vp8l.Budget) (image.Image, error) {
	m, _, err := decode(r, false, b)
	if err != nil {
		return nil, err
	}
	return m, nil
}

// DecodeConfig returns the color model and dimensions of a WEBP image without
// decoding the entire image.
func DecodeConfig(r io.Reader) (image.Config, error) {
	_, c, err := decode(r, true, vp8l.NewBudget(containerBytes+vp8DecoderBytes)) // Clack: the headers only
	return c, err
}

// Clack: no init registering the format with package image: its registry
// can't pass a decode its budget. imgcache calls Decode itself.
