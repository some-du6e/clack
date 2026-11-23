// SPDX-License-Identifier: Unlicense OR MIT

package gpu

import (
	"sync/atomic"

	"gioui.org/gpu/internal/driver"
	"gioui.org/internal/stroke"
)

// Clack patch 62, for tests on every backend: a stroke outline
// with a NaN point draws differently on each GPU (llvmpipe a bright column,
// others a gap or nothing), but the stroker that makes it runs on the CPU,
// the same everywhere.

var strokeNaNs atomic.Int64

// ClackStrokeNaNs is how many stroked paths the process's renderers have
// built whose outline has a NaN point.
func ClackStrokeNaNs() int64 { return strokeNaNs.Load() }

// ClackCoverageBits is the bits a channel of the path coverage textures
// the last renderer made holds: 16 (half float, every backend's first
// choice) or 32 (a fallback where half float can't be rendered to); 0
// before any. In half float the stencil sums leave a column of under 1%
// under rings and filled circles (a known Gio issue: Clack patch 63, which
// moved GL to 32-bit float, was dropped for its cost), and the ring tests'
// off-band bar follows it.
func ClackCoverageBits() int { return int(driver.ClackCoverageBits.Load()) }

func noteStrokeNaN(quads stroke.StrokeQuads) {
	for _, q := range quads {
		if q.Quad.From != q.Quad.From || q.Quad.Ctrl != q.Quad.Ctrl || q.Quad.To != q.Quad.To {
			strokeNaNs.Add(1)
			return
		}
	}
}
