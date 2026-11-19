package media

import (
	"image"

	"gioui.org/op/paint"
)

// GPUAPI is the graphics API of the device drawing a frame: Gio patch
// 50's paint.ExternalAPI.
type GPUAPI uint8

const (
	GPUNone GPUAPI = iota
	GPUDirect3D11
	GPUMetal
)

// Device is the device drawing a frame: an *ID3D11Device, an
// id<MTLDevice>.
type Device struct {
	API    GPUAPI
	Handle uintptr
}

// Texture is a frame on that device: an ID3D11ShaderResourceView* of an
// _SRGB format, an id<MTLTexture> of an _sRGB pixel format. Key names its
// pixels: a new frame needs a new Key.
type Texture struct {
	Handle uintptr
	Key    uint64
}

// Frames is a backend player's frames as the GPU asks for them.
type Frames interface {
	// Texture is the frame to draw on dev now, with a reference the GPU
	// takes over (paint.ExternalImage's rule: taken under the player's
	// lock, so Close can't free it between), on the goroutine drawing the
	// window's frame; it must return at once. false: none yet on this
	// device (the still shows).
	Texture(dev Device) (Texture, bool)
}

// FrameHandle is what a backend player hands Gio for its frames: one per
// player, so its ops keep one identity.
type FrameHandle struct{ f Frames }

// NewFrameHandle is f's handle.
func NewFrameHandle(f Frames) *FrameHandle { return &FrameHandle{f: f} }

// Op is the op drawing the frame stretched over size pixels, or still
// while there's none: an external image (Gio patch 51), or still on stock
// Gio (gio_patch51_stock.go).
func (h *FrameHandle) Op(still paint.ImageOp, size image.Point) paint.ImageOp {
	return externalOp(h, size, still)
}
