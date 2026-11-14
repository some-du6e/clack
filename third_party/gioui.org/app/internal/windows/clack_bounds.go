// SPDX-License-Identifier: Unlicense OR MIT

//go:build windows

package windows

// Clack patch (CLACK_PATCHES.md §25): what placing a window and listing
// the displays need (app.Bounds, app.Displays): EnumDisplayMonitors, a
// monitor's information and effective DPI, and the thread's DPI awareness
// for a caller that isn't a window's thread.

import (
	"unsafe"

	syscall "golang.org/x/sys/windows"
)

const (
	MONITOR_DEFAULTTONEAREST = 2
	MONITORINFOF_PRIMARY     = 1
)

var (
	_EnumDisplayMonitors = user32.NewProc("EnumDisplayMonitors")
)

// EnumDisplayMonitors calls cb (a syscall.NewCallback of a MONITORENUMPROC)
// for every display monitor, on this thread, and reports whether the
// enumeration ran.
func EnumDisplayMonitors(cb uintptr) bool {
	r, _, _ := _EnumDisplayMonitors.Call(0, 0, cb, 0)
	return r != 0
}

// MonitorInfoOf is GetMonitorInfo for a monitor handle.
func MonitorInfoOf(monitor syscall.Handle) (MonitorInfo, bool) {
	var mi MonitorInfo
	mi.cbSize = uint32(unsafe.Sizeof(mi))
	r, _, _ := _GetMonitorInfo.Call(uintptr(monitor), uintptr(unsafe.Pointer(&mi)))
	return mi, r != 0
}

// MonitorDPI is the monitor's effective DPI (GetDpiForMonitor, Windows 8.1
// and later), or the system's where the call is missing or fails.
func MonitorDPI(monitor syscall.Handle) int {
	if _GetDpiForMonitor.Find() == nil {
		var x, y uint32
		hr, _, _ := _GetDpiForMonitor.Call(uintptr(monitor), MDT_EFFECTIVE_DPI, uintptr(unsafe.Pointer(&x)), uintptr(unsafe.Pointer(&y)))
		if int32(hr) >= 0 && x > 0 {
			return int(x)
		}
	}
	return GetSystemDPI()
}

// PerMonitorDPIAware makes this thread per-monitor DPI aware (v2), as
// Gio's window threads are, so monitor and window rectangles come in
// physical pixels; restore puts the thread's previous awareness back. The
// caller keeps the goroutine on its thread in between (LockOSThread).
func PerMonitorDPIAware() (restore func()) {
	if _SetThreadDpiAwarenessContext.Find() != nil {
		return func() {}
	}
	old, _, _ := _SetThreadDpiAwarenessContext.Call(DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2)
	return func() {
		if old != 0 {
			_SetThreadDpiAwarenessContext.Call(old)
		}
	}
}
