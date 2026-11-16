//go:build !stockgio

package imgcache

import "gioui.org/op/paint"

// newLiveImageOp is paint.NewLiveImageOp (Gio patch 35, third_party/
// gioui.org/CLACK_PATCHES.md): the GPU draws l's frame due when it draws
// the window's frame.
func newLiveImageOp(l *live) paint.ImageOp { return paint.NewLiveImageOp(l) }
