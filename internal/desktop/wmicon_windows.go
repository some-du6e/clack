//go:build windows

package desktop

import (
	"bytes"
	"image"
	"image/png"
	"sync"
	"unsafe"

	"gioui.org/app"
	"gioui.org/io/event"
	xdraw "golang.org/x/image/draw"
)

// WM_SETICON's message and icon kinds.
const (
	wmSetIcon = 0x0080
	iconSmall = 0
	iconBig   = 1
)

var winIcon struct {
	mu         sync.Mutex
	hwnd       uintptr // the main window (0 once it's going)
	png        []byte  // SetAppIcon's, nil for Clack's own
	set        bool    // SetAppIcon has changed the window's icon
	big, small uintptr // the icons set from png
	// the icons before those: a posted WM_SETICON may still be on its way
	// with them, so they go only when the next change replaces them
	oldBig, oldSmall uintptr
}

// SetWindowIcon is the main window's handle for SetAppIcon; the window's
// own icon is the exe's icon resource #1 (Gio's window class) until then.
// Pass every window event.
func SetWindowIcon(ev event.Event, pngs ...[]byte) {
	e, ok := ev.(app.Win32ViewEvent)
	if !ok {
		return
	}
	winIcon.mu.Lock()
	defer winIcon.mu.Unlock()
	if e.HWND == 0 {
		winIcon.hwnd = 0 // the window is going
		return
	}
	if e.HWND != winIcon.hwnd {
		winIcon.hwnd = e.HWND
		if winIcon.set { // a window made again keeps the icon a plugin chose
			applyWinIconLocked()
		}
	}
}

// SetAppIcon is the window's (and so the taskbar button's) icon from now
// on (CustomAppIcon: Slick's window setIcon): png, a PNG, or Clack's own,
// the exe's icon resource, for nil. defaults aren't needed here.
func SetAppIcon(png []byte, _ ...[]byte) {
	winIcon.mu.Lock()
	defer winIcon.mu.Unlock()
	if png == nil && !winIcon.set {
		return // never changed: the window has Clack's own already
	}
	winIcon.png, winIcon.set = png, true
	applyWinIconLocked()
}

// applyWinIconLocked posts the window its icons: posted, never sent, as
// the caller isn't the window's thread, which may be waiting on it (a sent
// message would wait for that thread to pump messages).
func applyWinIconLocked() {
	if winIcon.hwnd == 0 {
		return
	}
	for _, h := range []uintptr{winIcon.oldBig, winIcon.oldSmall} {
		if h != 0 {
			procDestroyIcon.Call(h)
		}
	}
	winIcon.oldBig, winIcon.oldSmall = winIcon.big, winIcon.small
	winIcon.big, winIcon.small = 0, 0
	if winIcon.png != nil {
		// the big icon at its full size (the taskbar scales it), the small
		// one at the title bar's size, both from the PNG as Vista's icon
		// resources hold them
		lx, _, _ := procGetSystemMetrics.Call(smCxIcon)
		sx, _, _ := procGetSystemMetrics.Call(smCxSmIcon)
		winIcon.big = iconFromPNG(winIcon.png, max(int(lx), 256))
		winIcon.small = iconFromPNG(winIcon.png, int(sx))
	}
	// a null icon falls back on the window class's: the exe's
	procPostMessageW.Call(winIcon.hwnd, wmSetIcon, iconBig, winIcon.big)
	procPostMessageW.Call(winIcon.hwnd, wmSetIcon, iconSmall, winIcon.small)
}

// iconFromPNG is an HICON of b at size px (the PNG scaled to it first when
// it's larger), or 0.
func iconFromPNG(b []byte, size int) uintptr {
	if size <= 0 {
		size = 16
	}
	m, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		return 0
	}
	if s := m.Bounds().Size(); s.X > size || s.Y > size {
		w, h := size, s.Y*size/max(s.X, 1)
		if h > size {
			w, h = s.X*size/max(s.Y, 1), size
		}
		dst := image.NewNRGBA(image.Rect(0, 0, max(w, 1), max(h, 1)))
		xdraw.CatmullRom.Scale(dst, dst.Bounds(), m, m.Bounds(), xdraw.Src, nil)
		var buf bytes.Buffer
		if png.Encode(&buf, dst) != nil {
			return 0
		}
		b = buf.Bytes()
	}
	h, _, _ := procCreateIconFromResourceEx.Call(uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)), 1, 0x00030000,
		uintptr(size), uintptr(size), lrDefaultColor)
	return h
}
