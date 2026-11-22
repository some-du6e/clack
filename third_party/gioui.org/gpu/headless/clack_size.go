// SPDX-License-Identifier: Unlicense OR MIT

package headless

import (
	"errors"
	"image"

	"gioui.org/gpu/internal/driver"
)

// Clack patch 52: a headless window's framebuffer takes a new size, its
// context, device and GPU (with the GPU's caches) kept. Clack's inspect
// tool renders one shot per pick, each its own size, through one window
// that a session keeps: a context, a device and a GPU for every shot cost
// up to a second on Mesa's llvmpipe.

// SetSize gives the window a framebuffer of width×height px. What it
// showed is gone: the next Frame draws into the new one.
func (w *Window) SetSize(width, height int) error {
	if width <= 0 || height <= 0 {
		return errors.New("headless: empty size")
	}
	if w.size == image.Pt(width, height) {
		return nil
	}
	return contextDo(w.ctx, func() error {
		t, err := w.dev.NewTexture(driver.TextureFormatSRGBA, width, height,
			driver.FilterNearest, driver.FilterNearest, driver.BufferBindingFramebuffer)
		if err != nil {
			return err
		}
		w.fboTex.Release()
		w.fboTex, w.size = t, image.Pt(width, height)
		return nil
	})
}
