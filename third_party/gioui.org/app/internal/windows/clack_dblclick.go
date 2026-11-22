// SPDX-License-Identifier: Unlicense OR MIT

//go:build windows

package windows

// Clack patch 48 (CLACK_PATCHES.md §48): the double-click settings
// Chromium counts clicks with (app/clack_dblclick_windows.go).

const (
	SM_CXDOUBLECLK = 36
	SM_CYDOUBLECLK = 37
)

var _GetDoubleClickTime = user32.NewProc("GetDoubleClickTime")

// GetDoubleClickTime is the Mouse settings' double-click time, ms.
func GetDoubleClickTime() int {
	r, _, _ := _GetDoubleClickTime.Call()
	return int(uint32(r))
}
