//go:build js

package h264

// Stand-ins for the browser demo (h264.go): the names, fields and methods
// package huddle uses; nothing is ever decoded.

import "errors"

var errNoVideo = errors.New("video isn't part of the browser demo")

// Frame has the fields of goh264's that huddle reads.
type Frame struct {
	Width, Height      int
	CropLeft, CropTop  int
	ChromaFormatIDC    uint32
	BitDepthLuma       int
	VideoFullRangeFlag int32
	KeyFrame           bool
	YStride, CStride   int
	Y, Cb, Cr          []byte
}

type Decoder struct{}

func NewDecoder() *Decoder { return &Decoder{} }

func (*Decoder) DecodeFrames([]byte) ([]*Frame, error) { return nil, errNoVideo }
