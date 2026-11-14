//go:build windows

package ui

import (
	"unsafe"

	"gioui.org/app"
	"gioui.org/io/event"
	"golang.org/x/sys/windows"
)

// The window's DWM attributes follow what it shows: while the introduction
// or the sign-in card is up, the flat backdrop's colour (introBase) with
// light text; signed in, the app theme (dark: Windows' dark mode; light:
// the default). The main window has no OS caption since Gio patch 22
// (titlebar.go), but dark mode still themes its border and its system
// menu (Alt+Space, a right-click on the top nav), as Electron's
// SetDarkModeForWindow does, and the caption colours apply to any window
// that keeps its caption.
//
// DWMWA_USE_IMMERSIVE_DARK_MODE (20) is Windows 10 20H1 and later;
// DWMWA_CAPTION_COLOR (35) and DWMWA_TEXT_COLOR (36) are Windows 11. An
// older build answers E_INVALIDARG and keeps its caption: every error is
// ignored, it is cosmetic.

var procDwmSetWindowAttribute = windows.NewLazySystemDLL("dwmapi.dll").NewProc("DwmSetWindowAttribute")

const (
	dwmwaUseImmersiveDarkMode = 20
	dwmwaCaptionColor         = 35
	dwmwaTextColor            = 36
	dwmwaColorDefault         = 0xFFFFFFFF
)

// captionLook is what the caption shows.
type captionLook uint8

const (
	captionUnset   captionLook = iota
	captionWelcome             // the introduction and sign-in: introBase
	captionDark                // signed in, dark theme
	captionLight               // signed in, light theme
)

// captionState is the window's HWND and the look last applied to it (UI
// goroutine only).
type captionState struct {
	hwnd    uintptr
	applied captionLook
}

// syncCaption runs for every window event: it picks up the HWND from
// Gio's Win32ViewEvent and, when the look the window wants changed,
// applies it on the window's thread (Window.Run: the native loop serves
// it while it waits for this goroutine, so it never deadlocks).
func (a *App) syncCaption(ev event.Event) {
	c := &a.caption
	if e, ok := ev.(app.Win32ViewEvent); ok {
		c.hwnd, c.applied = e.HWND, captionUnset
	}
	if c.hwnd == 0 || a.win == nil {
		return
	}
	want := captionLight
	switch {
	case a.login != nil || a.intro != nil:
		want = captionWelcome
	case a.th.Dark:
		want = captionDark
	}
	if want == c.applied {
		return
	}
	c.applied = want
	hwnd := c.hwnd
	a.win.Run(func() { setCaption(hwnd, want) })
}

// mainHWND is the main window's HWND from Gio's Win32ViewEvent (0 before
// it, and after the window is destroyed). UI goroutine.
func (a *App) mainHWND() uintptr { return a.caption.hwnd }

// setCaption sets the DWM attributes for look l (the window's thread).
func setCaption(hwnd uintptr, l captionLook) {
	if procDwmSetWindowAttribute.Find() != nil {
		return // no dwmapi (not a desktop Windows)
	}
	set := func(attr uint32, v uint32) {
		_, _, _ = procDwmSetWindowAttribute.Call(hwnd, uintptr(attr), uintptr(unsafe.Pointer(&v)), unsafe.Sizeof(v))
	}
	dark, caption, text := uint32(0), uint32(dwmwaColorDefault), uint32(dwmwaColorDefault)
	switch l {
	case captionWelcome:
		dark, caption, text = 1, colorRef(introBase.R, introBase.G, introBase.B), colorRef(0xe8, 0xe8, 0xe8)
	case captionDark:
		dark = 1
	}
	set(dwmwaUseImmersiveDarkMode, dark)
	set(dwmwaCaptionColor, caption)
	set(dwmwaTextColor, text)
}

// colorRef is a Win32 COLORREF: 0x00BBGGRR.
func colorRef(r, g, b uint8) uint32 { return uint32(r) | uint32(g)<<8 | uint32(b)<<16 }
