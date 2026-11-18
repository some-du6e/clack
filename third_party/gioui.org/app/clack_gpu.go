// SPDX-License-Identifier: Unlicense OR MIT

package app

import (
	"sync/atomic"

	"gioui.org/gpu"
)

// Clack patch (CLACK_PATCHES.md §4): Chromium's ignore-gpu-blocklist for
// Gio. Gio's one GPU blocklist is X11's Vulkan (vulkanBuggy in os_x11.go:
// it doesn't work reliably on NVIDIA systems, gioui.org/issue/347). After
// IgnoreGPUBlocklist a new X11 window tries Vulkan first, and falls back
// to OpenGL ES as before when that fails. Elsewhere it changes nothing.
var gpuBlocklistIgnored atomic.Bool

// IgnoreGPUBlocklist lets windows created after it use the GPU backends Gio
// leaves out because of driver bugs.
func IgnoreGPUBlocklist() { gpuBlocklistIgnored.Store(true) }

// ClackGPUIdle lets the window's GPU drop what partial frames keep (Clack
// patch, CLACK_PATCHES.md §46: the window-sized frame texture): the driver
// calls it on the window's thread when the window stops drawing for a while
// (hidden, minimized, occluded). Its next frame is whole.
func (c *callbacks) ClackGPUIdle() {
	w := c.w
	if w.gpu == nil || w.ctx == nil {
		return
	}
	r, ok := w.gpu.(gpu.ClackFrameReleaser)
	if !ok {
		return
	}
	if err := w.ctx.Lock(); err != nil {
		return
	}
	r.ClackReleaseFrames()
	w.ctx.Unlock()
}

// clackGPUFrames is SetGPUFrameHook's hook (Clack patch, CLACK_PATCHES.md
// §46).
var clackGPUFrames atomic.Pointer[func(gpu.FrameStats)]

// SetGPUFrameHook has f called with the renderer's stats for every frame a
// window draws (gpu.FrameStats: partial or whole, its damage, its draws
// and API calls), on the goroutine that drew it (macOS: the main thread),
// right after the GPU took the frame; nil removes it. For tests and
// diagnostics: f must be quick.
func SetGPUFrameHook(f func(gpu.FrameStats)) {
	if f == nil {
		clackGPUFrames.Store(nil)
		return
	}
	clackGPUFrames.Store(&f)
}
