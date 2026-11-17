// SPDX-License-Identifier: Unlicense OR MIT

package driver

// Clack patch 36: what a frame costs the graphics API.

// CallCounter is a Device that counts the calls it makes into its
// graphics API: cgo calls into Metal, COM calls into Direct3D 11, calls
// into OpenGL. A call its state cache found unnecessary (the API already
// has that state) counts as skipped.
type CallCounter interface {
	ClackCalls() Calls
}

// Calls are a device's running counts. Crossings are the calls from Go
// into C (cgo) or the system (syscall) it took: Metal's render commands
// are batched into one per render pass (Clack patch 37); elsewhere each
// API call is one.
type Calls struct {
	API, Skipped, Crossings int64
}

// ClackBatchCommands makes the Metal backend record a render pass's
// commands and run them in one cgo call when the pass ends (Clack patch
// 37). Tests turn it off to compare; set it between frames.
var ClackBatchCommands = true
