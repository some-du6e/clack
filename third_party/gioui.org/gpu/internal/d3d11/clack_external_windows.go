// SPDX-License-Identifier: Unlicense OR MIT

package d3d11

import (
	"errors"
	"math"
	"unsafe"

	"gioui.org/gpu/internal/driver"
	"gioui.org/internal/d3d11"
)

// Clack patch 50: Direct3D 11 draws a shader resource view something else
// made on its device (paint.NewExternalImageOp).

// ClackDevice implements driver.ClackExternal: the *ID3D11Device.
func (b *Backend) ClackDevice() (uint8, uintptr) {
	return driver.ClackAPIDirect3D11, uintptr(unsafe.Pointer(b.dev))
}

// ClackWrap implements driver.ClackExternal: native is an
// ID3D11ShaderResourceView* of a texture on b's device, of an _SRGB
// format. The texture holds a reference to the view until it's released;
// its sampler (clamped, no mipmaps) is the backend's.
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
			MinLOD:        -math.MaxFloat32,
			MaxLOD:        math.MaxFloat32,
		})
		b.calls.API++
		if err != nil {
			return nil, err
		}
		b.extSamplers[i] = s
	}
	// native is a COM pointer, not Go memory
	view := *(**d3d11.ShaderResourceView)(unsafe.Pointer(&native))
	d3d11.IUnknownAddRef(unsafe.Pointer(view), view.Vtbl.AddRef)
	b.calls.API++
	b.wrapped++
	return &Texture{backend: b, sampler: s, resView: view, width: width, height: height, bindings: driver.BufferBindingTexture, external: true}, nil
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

// ClackWrapped implements driver.ClackExternalTest.
func (b *Backend) ClackWrapped() int { return b.wrapped }
