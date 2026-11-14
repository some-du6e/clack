//go:build windows

package ui

import (
	"gioui.org/app"
	"golang.org/x/sys/windows"
)

// The Windows main window is frameless like Slack's (titlebar.go): Gio
// patch 22 takes the OS title bar away and keeps the frame, and the top
// nav draws Windows' caption buttons and is the caption.

// mainMinW and mainMinH are Slack's main window minimum on Windows
// (win32 main.bundle.cjs @1144029: d = linux ? 300 : 400, f = linux ?
// 300 : 668).
const mainMinW, mainMinH = 668, 400

// mainWindowChrome is the main window's options for its frame.
func mainWindowChrome() []app.Option { return []app.Option{app.HiddenTitleBar(true)} }

// wcoEnable turns the caption buttons on for Run's window. Windows 10
// draws them square (Electron's WinIconPainter), 11 rounded
// (Win11IconPainter: base::win::GetVersion() >= WIN11, build 22000).
func (a *App) wcoEnable() {
	a.wco.on = true
	a.wco.win10 = windows.RtlGetVersion().BuildNumber < 22000
}

var procPostMessage = windows.NewLazySystemDLL("user32.dll").NewProc("PostMessageW")

const wmSysCommand = 0x0112

// postSysCommand posts WM_SYSCOMMAND sc to the main window, as Windows'
// own caption buttons send it (any goroutine).
func (a *App) postSysCommand(sc uint32) {
	if h := a.caption.hwnd; h != 0 {
		_, _, _ = procPostMessage.Call(h, wmSysCommand, uintptr(sc), 0)
	}
}
