// SPDX-License-Identifier: Unlicense OR MIT

package app

import "sync/atomic"

// Clack patch (CLACK_PATCHES.md §4): Chromium's ignore-gpu-blocklist for
// Gio. Gio's one GPU blocklist is X11's Vulkan (vulkanBuggy in os_x11.go:
// it doesn't work reliably on NVIDIA systems, gioui.org/issue/347). After
// IgnoreGPUBlocklist a new X11 window tries Vulkan first, and falls back
// to OpenGL ES as before when that fails. Elsewhere it changes nothing.
var gpuBlocklistIgnored atomic.Bool

// IgnoreGPUBlocklist lets windows created after it use the GPU backends Gio
// leaves out because of driver bugs.
func IgnoreGPUBlocklist() { gpuBlocklistIgnored.Store(true) }
