// SPDX-License-Identifier: Unlicense OR MIT

package driver

import "sync/atomic"

// ClackCoverageBits is the bits a channel of the last float texture (path
// coverage, Clack patch 63) a backend made holds: 32 or 16, 0 before any.
// For tests: gpu.ClackCoverageBits.
var ClackCoverageBits atomic.Int32
