//go:build stockgio

package imgcache

import "gioui.org/op/paint"

// Stock Gio v0.10.3 has no live images (see gio_patch35.go): SetLiveOps
// stays off, so Get hands out the frame due when it's called.
const liveOpsSupported = false

// newLiveImageOp is never called with live ops off; it's the still.
func newLiveImageOp(l *live) paint.ImageOp { return l.last }
