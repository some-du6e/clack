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

// WM_SETICON's message and icon kinds, and SendMessageTimeout's.
const (
	wmSetIcon       = 0x0080
	iconSmall       = 0
	iconBig         = 1
	smtoAbortIfHung = 0x0002
	iconSendWait    = 2000 // ms
)

var procSendMessageTimeoutW = user32.NewProc("SendMessageTimeoutW")

var winIcon struct {
	mu   sync.Mutex
	hwnd uintptr // the main window (0 once it's going)
	png  []byte  // SetAppIcon's, nil for Clack's own
	set  bool    // SetAppIcon has changed the window's icon
	// dirty: a change sendIcons hasn't made yet; sending: it runs
	dirty, sending bool
	// ours: the icons made here and not freed, freed once WM_SETICON
	// hands one back as the icon it replaced
	ours map[uintptr]bool
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
			winIcon.dirty = true
			startIconsLocked()
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
	winIcon.png, winIcon.set, winIcon.dirty = png, true, true
	startIconsLocked()
}

func startIconsLocked() {
	if !winIcon.sending && winIcon.hwnd != 0 {
		winIcon.sending = true
		go sendIcons()
	}
}

// sendIcons gives the window its latest icons, from a goroutine of its
// own: sent rather than posted, so WM_SETICON hands back the icon it
// replaced, which is freed then and not while the window may still show
// it, as several changes posted in a row could free; never from the UI
// goroutine, whose window thread may be waiting on it. A send that times
// out keeps both icons: a leak rather than a freed icon on screen.
func sendIcons() {
	for {
		winIcon.mu.Lock()
		if !winIcon.dirty || winIcon.hwnd == 0 {
			winIcon.sending = false
			winIcon.mu.Unlock()
			return
		}
		winIcon.dirty = false
		hwnd, b := winIcon.hwnd, winIcon.png
		winIcon.mu.Unlock()
		var big, small uintptr
		if b != nil {
			// the big icon at its full size (the taskbar scales it), the
			// small one at the title bar's size, both from the PNG as
			// Vista's icon resources hold them
			lx, _, _ := procGetSystemMetrics.Call(smCxIcon)
			sx, _, _ := procGetSystemMetrics.Call(smCxSmIcon)
			big, small = iconFromPNG(b, max(int(lx), 256)), iconFromPNG(b, int(sx))
		}
		for _, x := range [...]struct{ kind, icon uintptr }{{iconBig, big}, {iconSmall, small}} {
			winIcon.mu.Lock()
			if x.icon != 0 {
				if winIcon.ours == nil {
					winIcon.ours = map[uintptr]bool{}
				}
				winIcon.ours[x.icon] = true
			}
			winIcon.mu.Unlock()
			// a null icon falls back on the window class's: the exe's
			var prev uintptr
			r, _, _ := procSendMessageTimeoutW.Call(hwnd, wmSetIcon, x.kind, x.icon, smtoAbortIfHung, iconSendWait,
				uintptr(unsafe.Pointer(&prev)))
			if r == 0 || prev == 0 {
				continue
			}
			winIcon.mu.Lock()
			if winIcon.ours[prev] {
				delete(winIcon.ours, prev)
				procDestroyIcon.Call(prev)
			}
			winIcon.mu.Unlock()
		}
	}
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
