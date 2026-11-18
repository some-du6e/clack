// SPDX-License-Identifier: Unlicense OR MIT

package gpu

import (
	"image"

	"gioui.org/gpu/internal/driver"
)

// Clack patch 50: external images (paint.NewExternalImageOp), textures the
// renderer didn't make, such as a video decoder's frames.

// externalImage is an external ImageOp's handle, with plain types so gpu
// doesn't import paint (as liveImage).
type externalImage interface {
	// ClackExternal is the texture to draw on the device, with a reference
	// the caller takes over, and its key.
	ClackExternal(api uint8, dev uintptr) (tex uintptr, key uint64, ok bool)
	// ClackStill is what to draw without one: an image and its key, or
	// nil for nothing.
	ClackStill() (*image.RGBA, any)
}

// externalKey is a wrapped texture's cache key: the native texture and
// the key naming its pixels.
type externalKey struct {
	tex uintptr
	key uint64
}

// stillDepth bounds the stills resolved for one paint: an external image's
// still can itself be external or live (a viewer's player over the inline
// player's last frame).
const stillDepth = 4

// resolveImage is the texture of a paint whose image is external or live
// (true), or the data to upload as Gio's own (false): an external image's
// texture or else its still, a live image's image now, each in turn. What
// can't be uploaded (a header with no pixels, a still too deep) draws a
// transparent texture.
func (r *renderer) resolveImage(cache *textureCache, data imageOpData) (driver.Texture, imageOpData, bool) {
	for i := 0; i <= stillDepth; i++ {
		switch h := data.handle.(type) {
		case externalImage:
			if i == stillDepth {
				return r.clearTexture(), data, true
			}
			if t, ok := r.resolveExternal(cache, h, data); ok {
				return t, data, true
			}
			src, sh := h.ClackStill()
			if src == nil {
				return r.clearTexture(), data, true
			}
			data.src, data.handle = src, sh
			continue
		case liveImage:
			if i == stillDepth {
				return r.clearTexture(), data, true
			}
			src, lh := h.LiveImage()
			if src == nil || lh == nil {
				break // the image it was made with
			}
			data.src, data.handle = src, lh
			continue
		}
		break
	}
	if !uploadable(data.src) {
		return r.clearTexture(), data, true
	}
	return nil, data, false
}

// uploadable is whether src has the pixels its bounds say (an external
// op's source is a header without any).
func uploadable(src *image.RGBA) bool {
	if src == nil || src.Bounds().Empty() {
		return false
	}
	b := src.Bounds()
	return len(src.Pix) >= src.PixOffset(b.Max.X, b.Max.Y-1)
}

// resolveExternal is the texture drawing an external image's paint, false
// for none (the still draws). The ask happens here, in uploadImages: once
// for each paint the frame shows. The ask hands out a reference: a texture
// whose key the cache has from the frame before is drawn again as it is
// and the reference dropped; a new key wraps the texture with it (no
// upload, no mipmaps), and the old key's wrapper is released when a frame
// doesn't draw it (textureCache.frame), which on Metal and Direct3D comes
// after the GPU finished the frame that drew it last.
func (r *renderer) resolveExternal(cache *textureCache, e externalImage, data imageOpData) (driver.Texture, bool) {
	x, ok := r.ctx.(driver.ClackExternal)
	if !ok {
		return nil, false
	}
	api, dev := x.ClackDevice()
	r.stats.Externals++
	native, key, ok := e.ClackExternal(api, dev)
	x.ClackForgetState()
	if !ok {
		return nil, false
	}
	k := textureCacheKey{filter: data.filter, handle: externalKey{tex: native, key: key}}
	if t, ok := cache.get(k); ok {
		x.ClackDrop(native)
		return t.(*texture).tex, true
	}
	filter := driver.FilterLinear
	if data.filter == filterNearest {
		filter = driver.FilterNearest
	}
	sz := data.src.Bounds().Size()
	t, err := x.ClackWrap(native, sz.X, sz.Y, filter)
	if err != nil {
		x.ClackDrop(native)
		return nil, false
	}
	r.stats.ExternalWraps++
	cache.put(k, &texture{src: data.src, tex: t})
	return t, true
}

// clearTexture is a 1×1 transparent texture, drawn for an image with
// nothing to draw.
func (r *renderer) clearTexture() driver.Texture {
	if r.clear1x1 != nil {
		return r.clear1x1
	}
	t, err := r.ctx.NewTexture(driver.TextureFormatSRGBA, 1, 1, driver.FilterNearest, driver.FilterNearest, driver.BufferBindingTexture)
	if err != nil {
		panic(err)
	}
	t.Upload(image.Point{}, image.Pt(1, 1), make([]byte, 4), 4)
	r.clear1x1 = t
	return t
}
