// SPDX-License-Identifier: Unlicense OR MIT

package headless

import (
	"image"

	"gioui.org/gpu"
)

var image1x1 = image.Rect(0, 0, 1, 1)

// ClackFrameStats reports what the last Frame cost the renderer and the
// graphics API (Clack patch 36).
func (w *Window) ClackFrameStats() gpu.FrameStats {
	var st gpu.FrameStats
	contextDo(w.ctx, func() error {
		if s, ok := w.gpu.(gpu.ClackStats); ok {
			st = s.ClackFrameStats()
		}
		return nil
	})
	return st
}

// ClackStatsDetail turns FrameStats.StencilPx on or off (Clack patch 36):
// every path keeps its quads' bounds on the CPU.
func (w *Window) ClackStatsDetail(on bool) {
	contextDo(w.ctx, func() error {
		if s, ok := w.gpu.(gpu.ClackStats); ok {
			s.ClackStatsDetail(on)
		}
		return nil
	})
}

// ClackSync waits until the GPU has finished the frames drawn so far (it
// reads one pixel back), so a frame's time includes its rasterization.
func (w *Window) ClackSync() error {
	return contextDo(w.ctx, func() error {
		var px [4]byte
		return w.fboTex.ReadPixels(image1x1, px[:], 4)
	})
}
