// SPDX-License-Identifier: Unlicense OR MIT

//go:build windows && !386 && !arm

package app

import (
	syscall "golang.org/x/sys/windows"

	"gioui.org/app/internal/windows"
)

// dropCallbacks are IDropTarget's DragEnter, DragOver and Drop. On 64-bit
// Windows (amd64, arm64) the POINTL argument, 8 bytes, arrives by value in
// one register: x in the low half, y in the high half.
func dropCallbacks() (enter, over, drop uintptr) {
	enter = syscall.NewCallback(func(this, data, keys, pt, effect uintptr) uintptr {
		return dtDragEnter(this, data, uint32(keys), pointl(pt), effect)
	})
	over = syscall.NewCallback(func(this, keys, pt, effect uintptr) uintptr {
		return dtDragOver(this, uint32(keys), pointl(pt), effect)
	})
	drop = syscall.NewCallback(func(this, data, keys, pt, effect uintptr) uintptr {
		return dtDrop(this, data, uint32(keys), pointl(pt), effect)
	})
	return
}

func pointl(v uintptr) windows.Point {
	return windows.Point{X: int32(uint32(v)), Y: int32(uint32(uint64(v) >> 32))}
}
