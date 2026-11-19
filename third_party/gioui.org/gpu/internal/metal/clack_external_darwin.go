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

// Clack patch 51: Metal draws a texture something else made on its device
// (paint.NewExternalImageOp).

// ClackDevice implements driver.ClackExternal: the id<MTLDevice>.
func (b *Backend) ClackDevice() (uint8, uintptr) {
	return driver.ClackAPIMetal, uintptr(b.dev)
}

// ClackWrap implements driver.ClackExternal: native is an id<MTLTexture>
// on b's device, of an _sRGB pixel format, whose reference the texture
// takes over; it samples it with the backend's shared sampler for the
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
	b.wrapped++
	return &Texture{backend: b, texture: C.CFTypeRef(native), sampler: s, width: width, height: height, external: true}, nil
}

// ClackDrop implements driver.ClackExternal: the texture's reference no
// wrapper takes.
func (b *Backend) ClackDrop(native uintptr) {
	if native != 0 {
		C.CFRelease(C.CFTypeRef(native))
		b.apiCall()
	}
}

// ClackForgetState implements driver.ClackExternal: an ask encodes
// nothing into Gio's command buffer, so there's nothing to forget.
func (b *Backend) ClackForgetState() {}

// ClackNative implements driver.ClackExternalTest: the id<MTLTexture>.
func (b *Backend) ClackNative(t driver.Texture) uintptr {
	return uintptr(t.(*Texture).texture)
}

// ClackRetain implements driver.ClackExternalTest.
func (b *Backend) ClackRetain(native uintptr) { C.CFRetain(C.CFTypeRef(native)) }

// ClackRefs implements driver.ClackExternalTest.
func (b *Backend) ClackRefs(native uintptr) int { return int(C.CFGetRetainCount(C.CFTypeRef(native))) }

// ClackWrapped implements driver.ClackExternalTest.
func (b *Backend) ClackWrapped() int { return b.wrapped }
