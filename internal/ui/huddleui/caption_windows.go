//go:build windows

package huddleui

import (
	"unsafe"

	"gioui.org/app"
	"gioui.org/io/event"
	"golang.org/x/sys/windows"
)

// DWMWA_USE_IMMERSIVE_DARK_MODE (20) is Windows 10 20H1 and later;
// DWMWA_CAPTION_COLOR (35) and DWMWA_TEXT_COLOR (36) are Windows 11. An
// older build answers E_INVALIDARG and keeps its caption: every error is
// ignored, it is cosmetic.
var procDwmSetWindowAttribute = windows.NewLazySystemDLL("dwmapi.dll").NewProc("DwmSetWindowAttribute")

const (
	dwmwaUseImmersiveDarkMode = 20
	dwmwaCaptionColor         = 35
	dwmwaTextColor            = 36
)

// captionState is a window's HWND and the look last applied to it (the
// window's event goroutine only).
type captionState struct {
	hwnd    uintptr
	applied captionLook
	set     bool
}

// sync runs for every window event: it picks up the HWND from Gio's
// Win32ViewEvent and, when the look changed, applies it on the window's
// thread (Window.Run: the native loop serves it while it waits for this
// goroutine, so it never deadlocks).
func (c *captionState) sync(ev event.Event, w *app.Window, want captionLook) {
	if e, ok := ev.(app.Win32ViewEvent); ok {
		c.hwnd, c.set = e.HWND, false
	}
	if c.hwnd == 0 || w == nil || (c.set && c.applied == want) {
		return
	}
	c.applied, c.set = want, true
	hwnd := c.hwnd
	w.Run(func() { setCaption(hwnd, want) })
}

// setCaption sets the DWM attributes for l (the window's thread).
func setCaption(hwnd uintptr, l captionLook) {
	if procDwmSetWindowAttribute.Find() != nil {
		return // no dwmapi (not a desktop Windows)
	}
	set := func(attr uint32, v uint32) {
		_, _, _ = procDwmSetWindowAttribute.Call(hwnd, uintptr(attr), uintptr(unsafe.Pointer(&v)), unsafe.Sizeof(v))
	}
	dark := uint32(0)
	if l.dark {
		dark = 1
	}
	set(dwmwaUseImmersiveDarkMode, dark)
	set(dwmwaCaptionColor, colorRef(l.bg.R, l.bg.G, l.bg.B))
	set(dwmwaTextColor, colorRef(l.text.R, l.text.G, l.text.B))
}

// colorRef is a Win32 COLORREF: 0x00BBGGRR.
func colorRef(r, g, b uint8) uint32 { return uint32(r) | uint32(g)<<8 | uint32(b)<<16 }
