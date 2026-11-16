//go:build !stockgio

package imgcache

import "gioui.org/op/paint"

// liveOpsSupported: Gio has live images (patch 35, third_party/gioui.org/
// CLACK_PATCHES.md), so SetLiveOps can turn them on.
const liveOpsSupported = true

// newLiveImageOp is paint.NewLiveImageOp: the GPU draws l's frame due
// when it draws the window's frame.
func newLiveImageOp(l *live) paint.ImageOp { return paint.NewLiveImageOp(l) }
