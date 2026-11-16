//go:build stockgio

package imgcache

import "gioui.org/op/paint"

// Stock Gio v0.10.3 has no live images (see gio_patch35.go): a "live"
// ImageOp is the frame due when Get was called, as without SetLiveOps.
func newLiveImageOp(l *live) paint.ImageOp { return l.Image() }
