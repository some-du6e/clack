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

// Calls are a device's running counts.
type Calls struct {
	API, Skipped int64
}
