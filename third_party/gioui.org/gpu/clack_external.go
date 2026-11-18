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
	// ClackExternal is the texture to draw on the device and its key.
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

// resolveExternal is the texture drawing an external image's paint (ok),
// or the data drawing its still (!ok, data's src nil: nothing). The ask
// happens here, in uploadImages: once for each paint the frame shows. A
// texture whose key the cache has from the frame before is drawn again
// as it is; a new key wraps the texture (no upload, no mipmaps), and the
// old key's wrapper is released when a frame doesn't draw it
// (textureCache.frame), which on Metal and Direct3D comes after the GPU
// finished the frame that drew it last.
func (r *renderer) resolveExternal(cache *textureCache, e externalImage, data imageOpData) (driver.Texture, imageOpData, bool) {
	if x, ok := r.ctx.(driver.ClackExternal); ok {
		api, dev := x.ClackDevice()
		r.stats.Externals++
		native, key, ok := e.ClackExternal(api, dev)
		x.ClackForgetState()
		if ok {
			k := textureCacheKey{filter: data.filter, handle: externalKey{tex: native, key: key}}
			if t, ok := cache.get(k); ok {
				return t.(*texture).tex, data, true
			}
			filter := driver.FilterLinear
			if data.filter == filterNearest {
				filter = driver.FilterNearest
			}
			sz := data.src.Bounds().Size()
			if t, err := x.ClackWrap(native, sz.X, sz.Y, filter); err == nil {
				r.stats.ExternalWraps++
				cache.put(k, &texture{src: data.src, tex: t})
				return t, data, true
			}
		}
	}
	src, h := e.ClackStill()
	data.src, data.handle = src, h
	return nil, data, false
}

// clearTexture is a 1×1 transparent texture, drawn for an external image
// with neither a texture nor a still.
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
