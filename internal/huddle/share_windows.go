//go:build windows

package huddle

import (
	"errors"
	"image"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows screen capture through GDI, what WebRTC's ScreenCapturerWinGdi
// and its PrintWindow window capturer do: the picker's screens are the
// monitors, its windows the visible top-level, uncloaked, titled ones
// (WebRTC's GetWindowList rules); a screen is BitBlt'd from the desktop
// DC, a window drawn with PrintWindow(PW_RENDERFULLCONTENT), the cursor
// drawn in, at 15 fps. Clack is per-monitor DPI aware (its manifest), so
// all of it is in physical pixels. The border and the mini panel are
// excluded from capture (SetWindowDisplayAffinity WDA_EXCLUDEFROMCAPTURE,
// Windows 10 2004+).
func init() { shareOS = gdiShares{} }

const borderSupported = true

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	gdi32    = windows.NewLazySystemDLL("gdi32.dll")
	dwmapi   = windows.NewLazySystemDLL("dwmapi.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	pEnumDisplayMonitors       = user32.NewProc("EnumDisplayMonitors")
	pGetMonitorInfoW           = user32.NewProc("GetMonitorInfoW")
	pEnumWindows               = user32.NewProc("EnumWindows")
	pIsWindowVisible           = user32.NewProc("IsWindowVisible")
	pIsIconic                  = user32.NewProc("IsIconic")
	pIsWindow                  = user32.NewProc("IsWindow")
	pGetWindowLongPtrW         = user32.NewProc("GetWindowLongPtrW")
	pGetWindowTextW            = user32.NewProc("GetWindowTextW")
	pGetWindowTextLengthW      = user32.NewProc("GetWindowTextLengthW")
	pGetWindowThreadProcessID  = user32.NewProc("GetWindowThreadProcessId")
	pGetWindow                 = user32.NewProc("GetWindow")
	pGetWindowRect             = user32.NewProc("GetWindowRect")
	pGetDC                     = user32.NewProc("GetDC")
	pReleaseDC                 = user32.NewProc("ReleaseDC")
	pPrintWindow               = user32.NewProc("PrintWindow")
	pGetCursorInfo             = user32.NewProc("GetCursorInfo")
	pGetIconInfo               = user32.NewProc("GetIconInfo")
	pDrawIconEx                = user32.NewProc("DrawIconEx")
	pSetWindowPos              = user32.NewProc("SetWindowPos")
	pSetWindowDisplayAffinity  = user32.NewProc("SetWindowDisplayAffinity")
	pRegisterClassExW          = user32.NewProc("RegisterClassExW")
	pCreateWindowExW           = user32.NewProc("CreateWindowExW")
	pDefWindowProcW            = user32.NewProc("DefWindowProcW")
	pGetMessageW               = user32.NewProc("GetMessageW")
	pTranslateMessage          = user32.NewProc("TranslateMessage")
	pDispatchMessageW          = user32.NewProc("DispatchMessageW")
	pShowWindow                = user32.NewProc("ShowWindow")
	pSetLayeredWindowAttribute = user32.NewProc("SetLayeredWindowAttributes")
	pBeginPaint                = user32.NewProc("BeginPaint")
	pEndPaint                  = user32.NewProc("EndPaint")
	pFillRect                  = user32.NewProc("FillRect")
	pGetClientRect             = user32.NewProc("GetClientRect")
	pInvalidateRect            = user32.NewProc("InvalidateRect")
	pGetDpiForWindow           = user32.NewProc("GetDpiForWindow")

	pCreateCompatibleDC = gdi32.NewProc("CreateCompatibleDC")
	pCreateDIBSection   = gdi32.NewProc("CreateDIBSection")
	pSelectObject       = gdi32.NewProc("SelectObject")
	pDeleteObject       = gdi32.NewProc("DeleteObject")
	pDeleteDC           = gdi32.NewProc("DeleteDC")
	pBitBlt             = gdi32.NewProc("BitBlt")
	pStretchBlt         = gdi32.NewProc("StretchBlt")
	pSetStretchBltMode  = gdi32.NewProc("SetStretchBltMode")
	pSetBrushOrgEx      = gdi32.NewProc("SetBrushOrgEx")
	pCreateSolidBrush   = gdi32.NewProc("CreateSolidBrush")
	pGdiFlush           = gdi32.NewProc("GdiFlush")

	pDwmGetWindowAttribute      = dwmapi.NewProc("DwmGetWindowAttribute")
	pQueryFullProcessImageNameW = kernel32.NewProc("QueryFullProcessImageNameW")
	pGetModuleHandleW           = kernel32.NewProc("GetModuleHandleW")
)

const (
	srcCopy          = 0x00CC0020
	captureBlt       = 0x40000000
	halftone         = 4
	pwRenderFull     = 2
	gwlExStyle       = ^uintptr(19) // -20
	gwOwner          = 4
	wsExToolWindow   = 0x00000080
	wsExAppWindow    = 0x00040000
	dwmaCloaked      = 14
	dwmaExtFrame     = 9
	cursorShowing    = 1
	diNormal         = 3
	wdaExcludeCap    = 0x11
	swpNoActivate    = 0x0010
	swpShowWindow    = 0x0040
	swHide           = 0
	swShowNoActivate = 4
)

var hwndTopmost = ^uintptr(0) // (HWND)-1

type wRect struct{ Left, Top, Right, Bottom int32 }

func (r wRect) rect() image.Rectangle {
	return image.Rect(int(r.Left), int(r.Top), int(r.Right), int(r.Bottom))
}

type monitorInfoEx struct {
	CbSize        uint32
	Monitor, Work wRect
	Flags         uint32
	Device        [32]uint16
}

type bitmapInfoHeader struct {
	Size          uint32
	Width, Height int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPels, YPels  int32
	ClrUsed       uint32
	ClrImportant  uint32
}

type cursorInfo struct {
	CbSize uint32
	Flags  uint32
	Cursor uintptr
	X, Y   int32
}

type iconInfo struct {
	FIcon      int32
	XHot, YHot uint32
	Mask       uintptr
	Color      uintptr
}

// The enumeration callbacks are made once: a Go process can only make a
// limited number (syscall.NewCallback), and the border asks for the
// monitors ten times a second. enumMu guards their results.
var (
	enumMu    sync.Mutex
	enumMons  []image.Rectangle
	enumWins  []uintptr
	monitorCb = syscall.NewCallback(func(hm, hdc, rc, lp uintptr) uintptr {
		mi := monitorInfoEx{CbSize: uint32(unsafe.Sizeof(monitorInfoEx{}))}
		if r, _, _ := pGetMonitorInfoW.Call(hm, uintptr(unsafe.Pointer(&mi))); r != 0 {
			enumMons = append(enumMons, mi.Monitor.rect())
		}
		return 1
	})
	windowCb = syscall.NewCallback(func(h, lp uintptr) uintptr {
		if pickable(h) {
			enumWins = append(enumWins, h)
		}
		return 1
	})
	borderProcCb = syscall.NewCallback(borderProc)
)

// monitors lists the monitors' rectangles in EnumDisplayMonitors order.
func monitors() []image.Rectangle {
	enumMu.Lock()
	defer enumMu.Unlock()
	enumMons = nil
	pEnumDisplayMonitors.Call(0, 0, monitorCb, 0)
	return append([]image.Rectangle(nil), enumMons...)
}

// pickableWindows lists the picker's windows, top of the z-order first.
func pickableWindows() []uintptr {
	enumMu.Lock()
	defer enumMu.Unlock()
	enumWins = nil
	pEnumWindows.Call(windowCb, 0)
	return append([]uintptr(nil), enumWins...)
}

func windowText(h uintptr) string {
	n, _, _ := pGetWindowTextLengthW.Call(h)
	if n == 0 {
		return ""
	}
	buf := make([]uint16, n+1)
	pGetWindowTextW.Call(h, uintptr(unsafe.Pointer(&buf[0])), n+1)
	return windows.UTF16ToString(buf)
}

func cloaked(h uintptr) bool {
	var v uint32
	r, _, _ := pDwmGetWindowAttribute.Call(h, dwmaCloaked, uintptr(unsafe.Pointer(&v)), 4)
	return r == 0 && v != 0
}

// frameOf is a window's visible frame (DWM's extended frame bounds, which
// leave out the invisible resize borders), else its window rect.
func frameOf(h uintptr) (image.Rectangle, bool) {
	var r wRect
	if ok, _, _ := pDwmGetWindowAttribute.Call(h, dwmaExtFrame, uintptr(unsafe.Pointer(&r)), unsafe.Sizeof(r)); ok == 0 {
		return r.rect(), true
	}
	if ok, _, _ := pGetWindowRect.Call(h, uintptr(unsafe.Pointer(&r))); ok != 0 {
		return r.rect(), true
	}
	return image.Rectangle{}, false
}

func appOf(h uintptr) string {
	var pid uint32
	pGetWindowThreadProcessID.Call(h, uintptr(unsafe.Pointer(&pid)))
	p, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(p)
	buf := make([]uint16, 1024)
	n := uint32(len(buf))
	if r, _, _ := pQueryFullProcessImageNameW.Call(uintptr(p), 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n))); r == 0 {
		return ""
	}
	b := filepath.Base(windows.UTF16ToString(buf[:n]))
	return strings.TrimSuffix(b, filepath.Ext(b))
}

// pickable is WebRTC's GetWindowList filter: visible, top-level and
// unowned (or WS_EX_APPWINDOW), no tool window, not cloaked by DWM (other
// virtual desktops, suspended UWP apps), not minimized, with a title.
func pickable(h uintptr) bool {
	if v, _, _ := pIsWindowVisible.Call(h); v == 0 {
		return false
	}
	if v, _, _ := pIsIconic.Call(h); v != 0 {
		return false
	}
	ex, _, _ := pGetWindowLongPtrW.Call(h, gwlExStyle)
	owner, _, _ := pGetWindow.Call(h, gwOwner)
	if owner != 0 && ex&wsExAppWindow == 0 || ex&wsExToolWindow != 0 {
		return false
	}
	if cloaked(h) || windowText(h) == "" {
		return false
	}
	r, ok := frameOf(h)
	return ok && r.Dx() >= 50 && r.Dy() >= 50
}

type gdiShares struct{}

func (gdiShares) SystemAudio() bool { return false }

func (gdiShares) List(kind ShareKind, thumb image.Point) ([]ShareSource, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if kind == ShareScreen {
		ms := monitors()
		names := screenNames(max(1, len(ms)))
		var out []ShareSource
		for i, r := range ms {
			s := ShareSource{ID: shareSourceID(ShareScreen, uint64(i)), Name: names[i], DisplayID: strconv.Itoa(i), Bounds: r}
			tw, th := fitIn(r.Dx(), r.Dy(), thumb)
			if g, err := newGDIGrab(0, r, tw, th); err == nil {
				f := &CameraFrame{}
				if g.grab(f, false) == nil {
					s.Thumb = g.rgba()
				}
				g.close()
			}
			out = append(out, s)
		}
		return out, nil
	}
	var out []ShareSource
	for _, h := range pickableWindows() {
		r, _ := frameOf(h)
		s := ShareSource{ID: shareSourceID(ShareWindow, uint64(h)), Name: windowText(h), AppName: appOf(h), Bounds: r}
		tw, th := fitIn(r.Dx(), r.Dy(), thumb)
		if g, err := newGDIGrab(h, r, tw, th); err == nil {
			if g.grab(&CameraFrame{}, false) == nil {
				s.Thumb = g.rgba()
			}
			g.close()
		}
		out = append(out, s)
	}
	return out, nil
}

func (gdiShares) Open(src ShareSource, audio bool, logf func(string, ...any)) (ScreenCapture, error) {
	c := &gdiCapture{stop: make(chan struct{}), logf: logf}
	if src.Kind() == ShareWindow {
		c.win = uintptr(src.nativeID())
		if ok, _, _ := pIsWindow.Call(c.win); ok == 0 {
			return nil, ErrShareNotFound
		}
	} else {
		ms := monitors()
		i := int(src.nativeID())
		if i >= len(ms) {
			return nil, ErrShareNotFound
		}
		c.area = ms[i]
	}
	return c, nil
}

// gdiGrab is one capture target's DCs: the source drawn (BitBlt from the
// desktop, or PrintWindow) into a DIB of its own size, then scaled
// (HALFTONE StretchBlt) into a DIB of the output size when they differ.
type gdiGrab struct {
	win            uintptr
	area           image.Rectangle // a screen's area, or the window's frame
	src, dst       uintptr         // memory DCs
	srcBmp, dstBmp uintptr
	srcOld, dstOld uintptr
	srcBits        unsafe.Pointer
	dstBits        unsafe.Pointer
	full           image.Point // the DIB a window is printed into (its window rect)
	cut            image.Point // the visible frame's offset in it
	dw, dh         int
}

func dib(dc uintptr, w, h int) (bmp uintptr, bits unsafe.Pointer, err error) {
	bi := bitmapInfoHeader{Size: uint32(unsafe.Sizeof(bitmapInfoHeader{})), Width: int32(w), Height: -int32(h), Planes: 1, BitCount: 32}
	var p unsafe.Pointer
	b, _, _ := pCreateDIBSection.Call(dc, uintptr(unsafe.Pointer(&bi)), 0, uintptr(unsafe.Pointer(&p)), 0, 0)
	if b == 0 || p == nil {
		return 0, nil, errors.New("CreateDIBSection failed")
	}
	return b, p, nil
}

func newGDIGrab(win uintptr, area image.Rectangle, dw, dh int) (*gdiGrab, error) {
	if area.Dx() < 2 || area.Dy() < 2 || dw < 2 || dh < 2 {
		return nil, ErrShareNotFound
	}
	g := &gdiGrab{win: win, area: area, dw: dw, dh: dh}
	sdc, _, _ := pGetDC.Call(0)
	defer pReleaseDC.Call(0, sdc)
	g.src, _, _ = pCreateCompatibleDC.Call(sdc)
	g.dst, _, _ = pCreateCompatibleDC.Call(sdc)
	full := area.Size()
	if win != 0 {
		var wr wRect
		pGetWindowRect.Call(win, uintptr(unsafe.Pointer(&wr)))
		full = wr.rect().Size()
		g.cut = area.Min.Sub(wr.rect().Min)
	}
	g.full = full
	var err error
	if g.srcBmp, g.srcBits, err = dib(g.src, full.X, full.Y); err != nil {
		g.close()
		return nil, err
	}
	g.srcOld, _, _ = pSelectObject.Call(g.src, g.srcBmp)
	if g.dstBmp, g.dstBits, err = dib(g.dst, dw, dh); err != nil {
		g.close()
		return nil, err
	}
	g.dstOld, _, _ = pSelectObject.Call(g.dst, g.dstBmp)
	pSetStretchBltMode.Call(g.dst, halftone)
	pSetBrushOrgEx.Call(g.dst, 0, 0, 0)
	return g, nil
}

func (g *gdiGrab) close() {
	if g.src != 0 {
		if g.srcOld != 0 {
			pSelectObject.Call(g.src, g.srcOld)
		}
		pDeleteDC.Call(g.src)
	}
	if g.dst != 0 {
		if g.dstOld != 0 {
			pSelectObject.Call(g.dst, g.dstOld)
		}
		pDeleteDC.Call(g.dst)
	}
	for _, b := range []uintptr{g.srcBmp, g.dstBmp} {
		if b != 0 {
			pDeleteObject.Call(b)
		}
	}
	*g = gdiGrab{}
}

// grab draws the target (with the cursor when asked) into the output DIB
// and converts it to NV12 in f.
func (g *gdiGrab) grab(f *CameraFrame, cursor bool) error {
	w, h := g.area.Dx(), g.area.Dy()
	if g.win != 0 {
		if ok, _, _ := pPrintWindow.Call(g.win, g.src, pwRenderFull); ok == 0 {
			return errors.New("PrintWindow failed")
		}
	} else {
		sdc, _, _ := pGetDC.Call(0)
		ok, _, _ := pBitBlt.Call(g.src, 0, 0, uintptr(w), uintptr(h), sdc, uintptr(g.area.Min.X), uintptr(g.area.Min.Y), srcCopy|captureBlt)
		pReleaseDC.Call(0, sdc)
		if ok == 0 {
			return errors.New("BitBlt failed")
		}
	}
	if cursor {
		g.drawCursor()
	}
	pStretchBlt.Call(g.dst, 0, 0, uintptr(g.dw), uintptr(g.dh), g.src, uintptr(g.cut.X), uintptr(g.cut.Y), uintptr(w), uintptr(h), srcCopy)
	pGdiFlush.Call()
	pix := unsafe.Slice((*byte)(g.dstBits), g.dw*g.dh*4)
	bgraToNV12(f, pix, g.dw*4, g.dw, g.dh)
	return nil
}

// drawCursor draws the pointer where it is on the target.
func (g *gdiGrab) drawCursor() {
	ci := cursorInfo{CbSize: uint32(unsafe.Sizeof(cursorInfo{}))}
	if ok, _, _ := pGetCursorInfo.Call(uintptr(unsafe.Pointer(&ci))); ok == 0 || ci.Flags&cursorShowing == 0 || ci.Cursor == 0 {
		return
	}
	var ii iconInfo
	if ok, _, _ := pGetIconInfo.Call(ci.Cursor, uintptr(unsafe.Pointer(&ii))); ok != 0 {
		for _, b := range []uintptr{ii.Mask, ii.Color} {
			if b != 0 {
				pDeleteObject.Call(b)
			}
		}
	}
	x := int(ci.X) - int(ii.XHot) - g.area.Min.X + g.cut.X
	y := int(ci.Y) - int(ii.YHot) - g.area.Min.Y + g.cut.Y
	pDrawIconEx.Call(g.src, uintptr(x), uintptr(y), ci.Cursor, 0, 0, 0, 0, diNormal)
}

// rgba is the output DIB as an RGBA image (thumbnails).
func (g *gdiGrab) rgba() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, g.dw, g.dh))
	pix := unsafe.Slice((*byte)(g.dstBits), g.dw*g.dh*4)
	for i := 0; i < len(pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = pix[i+2], pix[i+1], pix[i], 0xff
	}
	return img
}

// gdiCapture grabs a monitor or a window at shareFPS on its own locked
// OS thread (GDI objects belong to their thread).
type gdiCapture struct {
	win  uintptr
	area image.Rectangle
	logf func(string, ...any)
	stop chan struct{}
	once sync.Once
	done chan struct{}
}

func (c *gdiCapture) Start(onFrame func(*CameraFrame), _ func([]int16), onEnd func(error)) error {
	c.done = make(chan struct{})
	started := make(chan error, 1)
	go func() {
		defer close(c.done)
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		var g *gdiGrab
		defer func() {
			if g != nil {
				g.close()
			}
		}()
		target := func() (image.Rectangle, bool) {
			if c.win == 0 {
				return c.area, true
			}
			if ok, _, _ := pIsWindow.Call(c.win); ok == 0 {
				return image.Rectangle{}, false
			}
			return frameOf(c.win)
		}
		r, ok := target()
		if !ok {
			started <- ErrShareNotFound
			return
		}
		started <- nil
		f := &CameraFrame{}
		t := time.NewTicker(time.Second / shareFPS)
		defer t.Stop()
		for {
			select {
			case <-c.stop:
				return
			case <-t.C:
			}
			if c.win != 0 {
				nr, ok := target()
				if !ok {
					if onEnd != nil {
						onEnd(ErrShareNotFound)
					}
					return
				}
				if v, _, _ := pIsIconic.Call(c.win); v != 0 {
					continue // minimized: nothing to draw
				}
				if nr.Size() != r.Size() && g != nil {
					g.close()
					g = nil
				}
				r = nr
			}
			if g == nil {
				dw, dh := capSize(r.Dx(), r.Dy())
				var err error
				if g, err = newGDIGrab(c.win, r, dw, dh); err != nil {
					g = nil
					continue
				}
			}
			if err := g.grab(f, true); err != nil {
				continue
			}
			f.At = time.Now()
			onFrame(f)
		}
	}()
	return <-started
}

func (c *gdiCapture) Close() {
	c.once.Do(func() {
		close(c.stop)
		if c.done != nil {
			<-c.done
		}
	})
}

// Windows has no screen recording permission to ask for.
func screenAccess() bool   { return true }
func requestScreenAccess() {}

const screenSettingsURL = ""

// ---- the presenter's border (BIqb: width 6, colour 0, 255/265, 0.2, 1) ----

// The border is one layered, click-through, topmost tool window over the
// target whose inside is the colour key (so it's see-through), painted on
// its own thread's message loop; a goroutine keeps it on the target.
var border struct {
	once   sync.Once
	hwnd   uintptr
	mu     sync.Mutex
	src    ShareSource
	follow chan struct{}
}

const borderKey = 0x00FF00FF                // magenta (COLORREF): the see-through inside
const borderColour = 0<<0 | 245<<8 | 51<<16 // rgb(0, 245, 51): 0, 255/265, 0.2

func borderWindow() uintptr {
	border.once.Do(func() {
		ready := make(chan uintptr, 1)
		go func() {
			runtime.LockOSThread()
			cls, _ := windows.UTF16PtrFromString("ClackShareBorder")
			inst, _, _ := pGetModuleHandleW.Call(0)
			wc := struct {
				Size, Style                 uint32
				Proc                        uintptr
				ClsExtra, WndExtra          int32
				Instance, Icon, Cursor, Bkg uintptr
				Menu, Class                 *uint16
				IconSm                      uintptr
			}{Proc: borderProcCb, Class: cls, Instance: inst}
			wc.Size = uint32(unsafe.Sizeof(wc))
			pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
			const exStyle = 0x00080000 | 0x00000020 | 0x00000008 | wsExToolWindow | 0x08000000                                // LAYERED|TRANSPARENT|TOPMOST|TOOLWINDOW|NOACTIVATE
			h, _, _ := pCreateWindowExW.Call(exStyle, uintptr(unsafe.Pointer(cls)), 0, 0x80000000, 0, 0, 1, 1, 0, 0, inst, 0) // WS_POPUP
			if h != 0 {
				pSetLayeredWindowAttribute.Call(h, borderKey, 0, 1) // LWA_COLORKEY
				pSetWindowDisplayAffinity.Call(h, wdaExcludeCap)
			}
			ready <- h
			var msg [48]byte // MSG
			for {
				if r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&msg[0])), 0, 0, 0); int32(r) <= 0 {
					return
				}
				pTranslateMessage.Call(uintptr(unsafe.Pointer(&msg[0])))
				pDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg[0])))
			}
		}()
		border.hwnd = <-ready
	})
	return border.hwnd
}

func borderProc(h, msg, wp, lp uintptr) uintptr {
	switch msg {
	case 0x0084: // WM_NCHITTEST: clicks go through
		return ^uintptr(0) // HTTRANSPARENT
	case 0x0014: // WM_ERASEBKGND
		return 1
	case 0x000F: // WM_PAINT
		var ps [72]byte // PAINTSTRUCT
		dc, _, _ := pBeginPaint.Call(h, uintptr(unsafe.Pointer(&ps[0])))
		var rc wRect
		pGetClientRect.Call(h, uintptr(unsafe.Pointer(&rc)))
		key, _, _ := pCreateSolidBrush.Call(borderKey)
		ink, _, _ := pCreateSolidBrush.Call(borderColour)
		pFillRect.Call(dc, uintptr(unsafe.Pointer(&rc)), key)
		bw := int32(6)
		if pGetDpiForWindow.Find() == nil { // Windows 10 1607+
			if dpi, _, _ := pGetDpiForWindow.Call(h); dpi > 0 {
				bw = int32(6 * int(dpi) / 96)
			}
		}
		for _, b := range []wRect{{rc.Left, rc.Top, rc.Right, rc.Top + bw}, {rc.Left, rc.Bottom - bw, rc.Right, rc.Bottom},
			{rc.Left, rc.Top, rc.Left + bw, rc.Bottom}, {rc.Right - bw, rc.Top, rc.Right, rc.Bottom}} {
			pFillRect.Call(dc, uintptr(unsafe.Pointer(&b)), ink)
		}
		pDeleteObject.Call(key)
		pDeleteObject.Call(ink)
		pEndPaint.Call(h, uintptr(unsafe.Pointer(&ps[0])))
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(h, msg, wp, lp)
	return r
}

func borderTarget(src ShareSource) (image.Rectangle, bool) {
	if src.Kind() == ShareWindow {
		h := uintptr(src.nativeID())
		if ok, _, _ := pIsWindow.Call(h); ok == 0 {
			return image.Rectangle{}, false
		}
		if v, _, _ := pIsIconic.Call(h); v != 0 {
			return image.Rectangle{}, false
		}
		return frameOf(h)
	}
	ms := monitors()
	if i := int(src.nativeID()); i < len(ms) {
		return ms[i], true
	}
	return image.Rectangle{}, false
}

func showBorder(src ShareSource) {
	h := borderWindow()
	if h == 0 {
		return
	}
	border.mu.Lock()
	if border.follow != nil {
		close(border.follow)
	}
	stop := make(chan struct{})
	border.src, border.follow = src, stop
	border.mu.Unlock()
	go func() {
		var last image.Rectangle
		t := time.NewTicker(100 * time.Millisecond)
		defer t.Stop()
		for first := true; ; first = false {
			if !first {
				select {
				case <-stop:
					return
				case <-t.C:
				}
			}
			r, ok := borderTarget(src)
			if !ok {
				pShowWindow.Call(h, swHide)
				last = image.Rectangle{}
				continue
			}
			if r != last {
				last = r
				pSetWindowPos.Call(h, hwndTopmost, uintptr(r.Min.X), uintptr(r.Min.Y), uintptr(r.Dx()), uintptr(r.Dy()), swpNoActivate|swpShowWindow)
				pInvalidateRect.Call(h, 0, 1)
			}
		}
	}()
}

func hideBorder() {
	border.mu.Lock()
	if border.follow != nil {
		close(border.follow)
		border.follow = nil
	}
	h := border.hwnd
	border.mu.Unlock()
	if h != 0 {
		pShowWindow.Call(h, swHide)
	}
}

// setupPanel puts Gio's window for the mini panel at r, topmost, without
// taking the focus, and out of captures.
func setupPanel(h PanelHandle, r image.Rectangle) {
	if h.View == 0 {
		return
	}
	pSetWindowDisplayAffinity.Call(h.View, wdaExcludeCap)
	pSetWindowPos.Call(h.View, hwndTopmost, uintptr(r.Min.X), uintptr(r.Min.Y), uintptr(r.Dx()), uintptr(r.Dy()), swpNoActivate|swpShowWindow)
}
