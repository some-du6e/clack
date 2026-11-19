// SPDX-License-Identifier: Unlicense OR MIT

package d3d11

import (
	"errors"
	"syscall"
	"unsafe"

	"gioui.org/gpu/internal/driver"
	"gioui.org/internal/d3d11"
)

// Clack patch 51: Direct3D 11 draws a shader resource view something else
// made on its device (paint.NewExternalImageOp).

// ClackDevice implements driver.ClackExternal: the *ID3D11Device.
func (b *Backend) ClackDevice() (uint8, uintptr) {
	return driver.ClackAPIDirect3D11, uintptr(unsafe.Pointer(b.dev))
}

// view is native as the shader resource view it is (a COM pointer, not Go
// memory).
func view(native uintptr) *d3d11.ShaderResourceView {
	return *(**d3d11.ShaderResourceView)(unsafe.Pointer(&native))
}

// ClackWrap implements driver.ClackExternal: native is an
// ID3D11ShaderResourceView* of a texture on b's device, of an _SRGB
// format, whose reference the texture takes over; its sampler (clamped,
// mip level 0 only) is the backend's.
func (b *Backend) ClackWrap(native uintptr, width, height int, filter driver.TextureFilter) (driver.Texture, error) {
	if native == 0 {
		return nil, errors.New("d3d11: ClackWrap of no view")
	}
	i, f := 0, uint32(d3d11.FILTER_MIN_MAG_LINEAR_MIP_POINT)
	if filter == driver.FilterNearest {
		i, f = 1, d3d11.FILTER_MIN_MAG_MIP_POINT
	}
	s := b.extSamplers[i]
	if s == nil {
		var err error
		s, err = b.dev.CreateSamplerState(&d3d11.SAMPLER_DESC{
			Filter:        f,
			AddressU:      d3d11.TEXTURE_ADDRESS_CLAMP,
			AddressV:      d3d11.TEXTURE_ADDRESS_CLAMP,
			AddressW:      d3d11.TEXTURE_ADDRESS_CLAMP,
			MaxAnisotropy: 1,
			MinLOD:        0,
			MaxLOD:        0, // the view's first level, whatever its count
		})
		b.calls.API++
		if err != nil {
			return nil, err
		}
		b.extSamplers[i] = s
	}
	b.wrapped++
	return &Texture{backend: b, sampler: s, resView: view(native), width: width, height: height, bindings: driver.BufferBindingTexture, external: true}, nil
}

// ClackDrop implements driver.ClackExternal: the view's reference no
// wrapper takes.
func (b *Backend) ClackDrop(native uintptr) {
	if native == 0 {
		return
	}
	v := view(native)
	d3d11.IUnknownRelease(unsafe.Pointer(v), v.Vtbl.Release)
	b.calls.API++
}

// ClackForgetState implements driver.ClackExternal: the state cache sets
// everything again, as after BeginFrame.
func (b *Backend) ClackForgetState() {
	b.set = ctxState{}
}

// ClackNative implements driver.ClackExternalTest: the texture's shader
// resource view.
func (b *Backend) ClackNative(t driver.Texture) uintptr {
	return uintptr(unsafe.Pointer(t.(*Texture).resView))
}

// ClackRetain implements driver.ClackExternalTest.
func (b *Backend) ClackRetain(native uintptr) {
	v := view(native)
	d3d11.IUnknownAddRef(unsafe.Pointer(v), v.Vtbl.AddRef)
}

// ClackRefs implements driver.ClackExternalTest: the count AddRef answers,
// less the reference it took.
func (b *Backend) ClackRefs(native uintptr) int {
	v := view(native)
	n, _, _ := syscall.SyscallN(v.Vtbl.AddRef, native)
	syscall.SyscallN(v.Vtbl.Release, native)
	return int(n) - 1
}

// ClackWrapped implements driver.ClackExternalTest.
func (b *Backend) ClackWrapped() int { return b.wrapped }
