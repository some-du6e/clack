// SPDX-License-Identifier: Unlicense OR MIT

package gpu

import (
	"sync/atomic"

	"gioui.org/gpu/internal/driver"
	"gioui.org/internal/stroke"
)

// Clack patches 62 and 63, for tests on every backend: a stroke outline
// with a NaN point draws differently on each GPU (llvmpipe a bright column,
// others a gap or nothing), but the stroker that makes it runs on the CPU,
// the same everywhere.

var strokeNaNs atomic.Int64

// ClackStrokeNaNs is how many stroked paths the process's renderers have
// built whose outline has a NaN point.
func ClackStrokeNaNs() int64 { return strokeNaNs.Load() }

// ClackCoverageBits is the bits a channel of the path coverage textures
// the last renderer made holds: 32 (Clack patch 63, on GL where it can be
// blended) or 16 (half float: Direct3D 11, Metal, Vulkan, older GL); 0
// before any.
func ClackCoverageBits() int { return int(driver.ClackCoverageBits.Load()) }

func noteStrokeNaN(quads stroke.StrokeQuads) {
	for _, q := range quads {
		if q.Quad.From != q.Quad.From || q.Quad.Ctrl != q.Quad.Ctrl || q.Quad.To != q.Quad.To {
			strokeNaNs.Add(1)
			return
		}
	}
}
