// SPDX-License-Identifier: Unlicense OR MIT

//go:build windows

package app

import (
	"image"
	"runtime"
	"sync"

	syscall "golang.org/x/sys/windows"

	"gioui.org/app/internal/windows"
)

// Clack patch (CLACK_PATCHES.md §25) on Windows: the displays are
// EnumDisplayMonitors' monitors (Chromium's ScreenWin reads them the same
// way: GetMonitorInfo's rcMonitor and rcWork, the primary flag,
// GetDpiForMonitor's effective DPI), in physical pixels, which needs a
// per-monitor DPI aware thread; a window's display is
// MonitorFromWindow(MONITOR_DEFAULTTONEAREST)'s, as getDisplayMatching
// is MonitorFromRect's with that flag.

// monitorList is what one EnumDisplayMonitors call collects: the callback
// is made once (syscall.NewCallback's are never freed), and the
// enumeration runs on the caller's thread, one at a time.
var monitorList struct {
	once sync.Once
	cb   uintptr
	mu   sync.Mutex
	got  []Display
}

func displays() []Display {
	l := &monitorList
	l.once.Do(func() { l.cb = syscall.NewCallback(monitorEnumProc) })
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	restore := windows.PerMonitorDPIAware()
	defer restore()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.got = nil
	ok := windows.EnumDisplayMonitors(l.cb)
	ds := l.got
	l.got = nil
	if !ok {
		return nil
	}
	return ds
}

// monitorEnumProc is the MONITORENUMPROC: each monitor, as a Display.
func monitorEnumProc(monitor, hdc, clip, data uintptr) uintptr {
	if d, ok := monitorDisplay(syscall.Handle(monitor)); ok {
		monitorList.got = append(monitorList.got, d)
	}
	return 1 // go on
}

// monitorDisplay is a monitor as a Display.
func monitorDisplay(monitor syscall.Handle) (Display, bool) {
	if monitor == 0 {
		return Display{}, false
	}
	mi, ok := windows.MonitorInfoOf(monitor)
	if !ok {
		return Display{}, false
	}
	return Display{
		Bounds:   winRectImage(mi.Monitor),
		WorkArea: winRectImage(mi.WorkArea),
		Scale:    float32(windows.MonitorDPI(monitor)) / 96,
		Primary:  mi.Flags&windows.MONITORINFOF_PRIMARY != 0,
	}, true
}

func winRectImage(r windows.Rect) image.Rectangle {
	return image.Rect(int(r.Left), int(r.Top), int(r.Right), int(r.Bottom))
}

// placement is Config.Bounds and Config.Display for the window now: its
// frame and its monitor while it is on screen (visible and not
// minimized), nothing otherwise. The window's thread.
func (w *window) placement() (image.Rectangle, Display) {
	if !windows.IsWindowVisible(w.hwnd) || windows.IsIconic(w.hwnd) {
		return image.Rectangle{}, Display{}
	}
	d, _ := monitorDisplay(windows.MonitorFromWindow(w.hwnd, windows.MONITOR_DEFAULTTONEAREST))
	return winRectImage(windows.GetWindowRect(w.hwnd)), d
}
