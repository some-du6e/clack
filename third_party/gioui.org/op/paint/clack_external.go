// SPDX-License-Identifier: Unlicense OR MIT

package paint

import "image"

// Clack patch 51 (CLACK_PATCHES.md §51): an image whose pixels are a GPU
// texture something else made, such as a video decoder's frame, drawn
// where its op is inside the window's own frame (under clips, opacity
// layers and transforms, below and above the ops around it).

// ExternalAPI is the graphics API of the device drawing a frame.
type ExternalAPI uint8

const (
	// ExternalNone is a device that can't draw external textures
	// (OpenGL, Vulkan): it never asks, and draws the still.
	ExternalNone ExternalAPI = iota
	// ExternalDirect3D11: Handle is an *ID3D11Device.
	ExternalDirect3D11
	// ExternalMetal: Handle is an id<MTLDevice>.
	ExternalMetal
)

// ExternalDevice is the device drawing the frame.
type ExternalDevice struct {
	API    ExternalAPI
	Handle uintptr
}

// ExternalTexture is a texture on the device drawing the frame: an
// ID3D11ShaderResourceView* of an _SRGB format on Direct3D 11, an
// id<MTLTexture> of an _sRGB pixel format on Metal, of premultiplied
// alpha. Key names its pixels: the same Key is the same pixels (nothing is
// wrapped again), new pixels need a new Key.
type ExternalTexture struct {
	Handle uintptr
	Key    uint64
}

// ExternalImage hands the GPU its texture while a frame is drawn.
type ExternalImage interface {
	// ExternalTexture is the texture to draw now, with a reference the
	// GPU takes over (AddRef'd, CFRetain'd: taken under the image's own
	// lock, so a Close freeing it can't come between). The GPU asks once
	// for every paint of the image the frame shows (none for one outside
	// the viewport or its clip, as live images, patch 35), on the
	// goroutine drawing the frame: the window's thread on Windows, the
	// main thread on macOS, a goroutine of its own for a headless window.
	// It must return at once. It may copy, sync and query on the device's
	// immediate context, leaving its pipeline state as it found it. The
	// texture's pixels must not change until the window's next frame
	// begins. ok false draws the still.
	ExternalTexture(dev ExternalDevice) (t ExternalTexture, ok bool)
}

// NewExternalImageOp is an ImageOp drawing e's texture stretched over
// size pixels, or still (stretched the same way) while e has no texture
// for the device drawing the frame, and on devices that can't draw
// external textures. A zero or uniform still draws nothing. e must be
// comparable (a pointer). size must stay the same while e is drawn: the
// paint's bounds and texture coordinates come from it.
func NewExternalImageOp(e ExternalImage, size image.Point, still ImageOp) ImageOp {
	if e == nil || size.X <= 0 || size.Y <= 0 {
		return ImageOp{}
	}
	// a header with no pixels: the GPU reads only its bounds, and never
	// uploads it
	src := &image.RGBA{Rect: image.Rectangle{Max: size}}
	return ImageOp{Filter: still.Filter, src: src, handle: externalHandle{e: e, still: still}}
}

// externalHandle is an external ImageOp's handle. The GPU asks it for the
// texture (gpu's texHandle), and draws the still when it has none.
type externalHandle struct {
	e     ExternalImage
	still ImageOp
}

// ClackExternal is the texture to draw on the device (api: an
// ExternalAPI) and its key.
func (h externalHandle) ClackExternal(api uint8, dev uintptr) (tex uintptr, key uint64, ok bool) {
	t, ok := h.e.ExternalTexture(ExternalDevice{API: ExternalAPI(api), Handle: dev})
	if !ok || t.Handle == 0 {
		return 0, 0, false
	}
	return t.Handle, t.Key, true
}

// ClackStill is the still's image and texture key; nil for a zero or
// uniform still (nothing is drawn).
func (h externalHandle) ClackStill() (*image.RGBA, any) {
	if h.still.uniform || h.still.src == nil || h.still.src.Bounds().Empty() {
		return nil, nil
	}
	return h.still.src, h.still.handle
}
