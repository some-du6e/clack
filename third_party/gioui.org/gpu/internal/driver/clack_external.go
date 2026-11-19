// SPDX-License-Identifier: Unlicense OR MIT

package driver

// Clack patch 51: textures the backend didn't make (paint.NewExternalImageOp).

// ClackExternal is a Device that can draw a texture it didn't make:
// Direct3D 11's and Metal's. OpenGL and Vulkan don't implement it, and an
// external image draws its still there.
type ClackExternal interface {
	// ClackDevice is the device's API (paint.ExternalAPI's values) and
	// handle: an *ID3D11Device, an id<MTLDevice>.
	ClackDevice() (api uint8, dev uintptr)
	// ClackWrap is a Texture drawing native (an
	// ID3D11ShaderResourceView*, an id<MTLTexture>) with filter
	// (FilterLinear or FilterNearest, never mipmapped). It takes over the
	// reference the ask handed out, and releases it when it's released;
	// on an error the reference is still the caller's (ClackDrop).
	ClackWrap(native uintptr, width, height int, filter TextureFilter) (Texture, error)
	// ClackDrop releases a reference an ask handed out that no wrapper
	// takes (the cache wrapped that texture and key already).
	ClackDrop(native uintptr)
	// ClackForgetState forgets the state the backend's state cache (patch
	// 37) believes the API has, after an ask (which may copy, sync and
	// query on the device's context, but leaves its pipeline state as it
	// found it).
	ClackForgetState()
}

// ClackExternalTest is what tests see of a ClackExternal device.
type ClackExternalTest interface {
	// ClackNative is the native handle of a texture NewTexture made, as
	// ClackWrap takes it.
	ClackNative(t Texture) uintptr
	// ClackRetain adds a reference to native (an ask hands out its own).
	ClackRetain(native uintptr)
	// ClackRefs is native's reference count.
	ClackRefs(native uintptr) int
	// ClackWrapped is how many wrapped textures aren't released yet.
	ClackWrapped() int
}

// The APIs ClackDevice reports, as paint.ExternalAPI's values.
const (
	ClackAPIDirect3D11 uint8 = 1
	ClackAPIMetal      uint8 = 2
)
