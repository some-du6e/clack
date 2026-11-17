// SPDX-License-Identifier: Unlicense OR MIT

package app

import "gioui.org/gpu"

// Red evidence only (branch damage-redraw-red1, never merged): patch 45's
// frame hook, which this window never calls.
func SetGPUFrameHook(f func(gpu.FrameStats)) {}
