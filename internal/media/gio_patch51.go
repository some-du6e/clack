//go:build !stockgio

package media

import (
	"image"

	"gioui.org/op/paint"
)

// ExternalImages: Gio draws external images (patch 51,
// third_party/gioui.org/CLACK_PATCHES.md) and live ones (patch 35), so a
// player's frames show.
const ExternalImages = true

// ExternalTexture implements paint.ExternalImage.
func (h *FrameHandle) ExternalTexture(d paint.ExternalDevice) (paint.ExternalTexture, bool) {
	t, ok := h.f.Texture(Device{API: GPUAPI(d.API), Handle: d.Handle})
	return paint.ExternalTexture{Handle: t.Handle, Key: t.Key}, ok
}

func externalOp(h *FrameHandle, size image.Point, still paint.ImageOp) paint.ImageOp {
	return paint.NewExternalImageOp(h, size, still)
}

// liveOp is paint.NewLiveImageOp (patch 35): the fake's frames.
func liveOp(l *fakeFrames, still paint.ImageOp) paint.ImageOp {
	if op := paint.NewLiveImageOp(l); op.Size() != (image.Point{}) {
		return op
	}
	return still
}
