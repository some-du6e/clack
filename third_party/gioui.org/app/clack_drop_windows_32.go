// SPDX-License-Identifier: Unlicense OR MIT

//go:build windows && (386 || arm)

package app

import (
	syscall "golang.org/x/sys/windows"

	"gioui.org/app/internal/windows"
)

// dropCallbacks are IDropTarget's DragEnter, DragOver and Drop. On 32-bit
// Windows the POINTL argument takes two argument words, x then y.
func dropCallbacks() (enter, over, drop uintptr) {
	enter = syscall.NewCallback(func(this, data, keys, x, y, effect uintptr) uintptr {
		return dtDragEnter(this, data, uint32(keys), windows.Point{X: int32(x), Y: int32(y)}, effect)
	})
	over = syscall.NewCallback(func(this, keys, x, y, effect uintptr) uintptr {
		return dtDragOver(this, uint32(keys), windows.Point{X: int32(x), Y: int32(y)}, effect)
	})
	drop = syscall.NewCallback(func(this, data, keys, x, y, effect uintptr) uintptr {
		return dtDrop(this, data, uint32(keys), windows.Point{X: int32(x), Y: int32(y)}, effect)
	})
	return
}
