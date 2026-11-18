// SPDX-License-Identifier: Unlicense OR MIT

package metal

import (
	"errors"

	"gioui.org/gpu/internal/driver"
)

/*
#include <CoreFoundation/CoreFoundation.h>
#include <Metal/Metal.h>
*/
import "C"

// Clack patch 50: Metal draws a texture something else made on its device
// (paint.NewExternalImageOp).

// ClackDevice implements driver.ClackExternal: the id<MTLDevice>.
func (b *Backend) ClackDevice() (uint8, uintptr) {
	return driver.ClackAPIMetal, uintptr(b.dev)
}

// ClackWrap implements driver.ClackExternal: native is an id<MTLTexture>
// on b's device, of an _sRGB pixel format. The texture retains it until
// it's released, and samples it with the backend's shared sampler for the
// filter (no mipmaps).
func (b *Backend) ClackWrap(native uintptr, width, height int, filter driver.TextureFilter) (driver.Texture, error) {
	if native == 0 {
		return nil, errors.New("metal: ClackWrap of no texture")
	}
	f := C.MTLSamplerMinMagFilter(C.MTLSamplerMinMagFilterLinear)
	if filter == driver.FilterNearest {
		f = C.MTLSamplerMinMagFilterNearest
	}
	s := b.sampler(f, f, C.MTLSamplerMipFilterNotMipmapped)
	if s == 0 {
		return nil, errors.New("metal: [MTLDevice newSamplerStateWithDescriptor:] failed")
	}
	tex := C.CFTypeRef(native)
	C.CFRetain(tex)
	b.apiCall()
	b.wrapped++
	return &Texture{backend: b, texture: tex, sampler: s, width: width, height: height, external: true}, nil
}

// ClackForgetState implements driver.ClackExternal: an ask encodes
// nothing into Gio's command buffer, so there's nothing to forget.
func (b *Backend) ClackForgetState() {}

// ClackNative implements driver.ClackExternalTest: the id<MTLTexture>.
func (b *Backend) ClackNative(t driver.Texture) uintptr {
	return uintptr(t.(*Texture).texture)
}

// ClackWrapped implements driver.ClackExternalTest.
func (b *Backend) ClackWrapped() int { return b.wrapped }
