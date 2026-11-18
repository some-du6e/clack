// SPDX-License-Identifier: Unlicense OR MIT

package driver

// Clack patch 50: textures the backend didn't make (paint.NewExternalImageOp).

// ClackExternal is a Device that can draw a texture it didn't make:
// Direct3D 11's and Metal's. OpenGL and Vulkan don't implement it, and an
// external image draws its still there.
type ClackExternal interface {
	// ClackDevice is the device's API (paint.ExternalAPI's values) and
	// handle: an *ID3D11Device, an id<MTLDevice>.
	ClackDevice() (api uint8, dev uintptr)
	// ClackWrap is a Texture drawing native (an
	// ID3D11ShaderResourceView*, an id<MTLTexture>) with filter
	// (FilterLinear or FilterNearest, never mipmapped). It holds a
	// reference to native until it's released.
	ClackWrap(native uintptr, width, height int, filter TextureFilter) (Texture, error)
	// ClackForgetState forgets the state the backend's state cache (patch
	// 37) believes the API has: an external image's ask may have used the
	// device's context.
	ClackForgetState()
}

// ClackExternalTest is what tests see of a ClackExternal device.
type ClackExternalTest interface {
	// ClackNative is the native handle of a texture NewTexture made, as
	// ClackWrap takes it.
	ClackNative(t Texture) uintptr
	// ClackWrapped is how many wrapped textures aren't released yet.
	ClackWrapped() int
}

// The APIs ClackDevice reports, as paint.ExternalAPI's values.
const (
	ClackAPIDirect3D11 uint8 = 1
	ClackAPIMetal      uint8 = 2
)
