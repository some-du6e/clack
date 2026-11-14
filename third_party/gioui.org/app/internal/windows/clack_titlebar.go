// SPDX-License-Identifier: Unlicense OR MIT

//go:build windows

package windows

// Clack patch (CLACK_PATCHES.md §22): what a window without its OS title
// bar needs (app.HiddenTitleBar): the caption buttons' hit-test codes, the
// non-client pointer messages, the frame's metrics at the window's DPI and
// the auto-hide taskbars a maximized window keeps clear of.

import (
	"unsafe"

	syscall "golang.org/x/sys/windows"
)

const (
	HTNOWHERE   = 0
	HTMINBUTTON = 8
	HTMAXBUTTON = 9
	HTBORDER    = 18
	HTCLOSE     = 20

	WM_NCMOUSELEAVE    = 0x02A2
	WM_NCPOINTERUPDATE = 0x0241
	WM_NCPOINTERDOWN   = 0x0242
	WM_NCPOINTERUP     = 0x0243
	WM_POINTERLEAVE    = 0x024A

	SM_CXPADDEDBORDER = 92

	// SHAppBarMessage
	ABM_GETAUTOHIDEBAREX = 0x0000000b
	ABE_LEFT             = 0
	ABE_TOP              = 1
	ABE_RIGHT            = 2
	ABE_BOTTOM           = 3
)

var (
	_GetSystemMetricsForDpi = user32.NewProc("GetSystemMetricsForDpi")
	_GetCursorPos           = user32.NewProc("GetCursorPos")
	_WindowFromPoint        = user32.NewProc("WindowFromPoint")
	_GetCapture             = user32.NewProc("GetCapture")
	_GetAncestor            = user32.NewProc("GetAncestor")
	_MonitorFromWindow      = user32.NewProc("MonitorFromWindow")

	shell32          = syscall.NewLazySystemDLL("shell32.dll")
	_SHAppBarMessage = shell32.NewProc("SHAppBarMessage")
)

// GetSystemMetricsForDpi is GetSystemMetrics at dpi (Windows 10 1607 and
// later); earlier systems answer at the system DPI.
func GetSystemMetricsForDpi(index, dpi int) int {
	if _GetSystemMetricsForDpi.Find() == nil {
		r, _, _ := _GetSystemMetricsForDpi.Call(uintptr(index), uintptr(dpi))
		return int(r)
	}
	return GetSystemMetrics(index)
}

// GetCursorPos is the cursor's screen position.
func GetCursorPos() (Point, bool) {
	var p Point
	r, _, _ := _GetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
	return p, r != 0
}

// WindowFromPoint is the top-level window at screen point p (GA_ROOT of
// the window under it), 0 for none.
func WindowFromPoint(p Point) syscall.Handle {
	// POINT goes by value: two int32s packed into one 64-bit argument on
	// amd64/arm64; on 386 they're two arguments.
	var h uintptr
	if unsafe.Sizeof(uintptr(0)) == 8 {
		h, _, _ = _WindowFromPoint.Call(uintptr(uint32(p.X)) | uintptr(uint32(p.Y))<<32)
	} else {
		h, _, _ = _WindowFromPoint.Call(uintptr(p.X), uintptr(p.Y))
	}
	if h == 0 {
		return 0
	}
	const gaRoot = 2
	r, _, _ := _GetAncestor.Call(h, gaRoot)
	return syscall.Handle(r)
}

// GetCapture is the window holding the mouse capture on this thread, or 0.
func GetCapture() syscall.Handle {
	r, _, _ := _GetCapture.Call()
	return syscall.Handle(r)
}

// MonitorFromWindow is the monitor the window is on (flags: MONITOR_*).
func MonitorFromWindow(hwnd syscall.Handle, flags uint32) syscall.Handle {
	r, _, _ := _MonitorFromWindow.Call(uintptr(hwnd), uintptr(flags))
	return syscall.Handle(r)
}

// appBarData is APPBARDATA.
type appBarData struct {
	cbSize           uint32
	hWnd             syscall.Handle
	uCallbackMessage uint32
	uEdge            uint32
	rc               Rect
	lParam           uintptr
}

// AutoHideTaskbar reports whether an auto-hide taskbar sits on edge
// (ABE_*) of monitor: SHAppBarMessage(ABM_GETAUTOHIDEBAREX), as
// Chromium's GetAppbarAutohideEdges asks it.
func AutoHideTaskbar(monitor syscall.Handle, edge uint32) bool {
	if monitor == 0 || _SHAppBarMessage.Find() != nil {
		return false
	}
	mi := MonitorInfo{}
	mi.cbSize = uint32(unsafe.Sizeof(mi))
	if r, _, _ := _GetMonitorInfo.Call(uintptr(monitor), uintptr(unsafe.Pointer(&mi))); r == 0 {
		return false
	}
	d := appBarData{uEdge: edge, rc: mi.Monitor}
	d.cbSize = uint32(unsafe.Sizeof(d))
	r, _, _ := _SHAppBarMessage.Call(ABM_GETAUTOHIDEBAREX, uintptr(unsafe.Pointer(&d)))
	return r != 0
}
