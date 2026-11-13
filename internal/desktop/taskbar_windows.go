//go:build windows

package desktop

import (
	"errors"
	"fmt"
	"image"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The taskbar button: Slack's unread badge as its overlay icon, the frame
// flash on a notification, and the window calls Slack's flash makes on the
// way (Electron's showInactive and minimize). Plain Win32 and COM through
// syscalls, as Electron and Chromium make them (read in Electron main
// 98ffb9e7, Chromium main 979ef2aa):
//
//   - SetTaskbarOverlay: TaskbarHost::SetOverlayIcon (taskbar_host.cc),
//     ITaskbarList3 from CoCreateInstance(CLSID_TaskbarList,
//     CLSCTX_INPROC_SERVER) and HrInit, kept for the next call, and the
//     icon from IconUtil::CreateHICONFromSkBitmap (ui/gfx/win/icon_util.cc).
//   - FlashFrame: NativeWindowViews::FlashFrame and
//     HWNDMessageHandler::FlashFrame.
//   - ShowInactive and Minimize: HWNDMessageHandler::Show(kInactive) and
//     ::Minimize.
//   - SystemIdleTime: powerMonitor.getSystemIdleTime, ui::CalculateIdleTime
//     (ui/base/idle/idle_win.cc).
//
// All of them but SystemIdleTime take the window's HWND and belong on the
// window's own thread, where Gio has initialized OLE (a single-threaded
// apartment), as Electron calls them on its UI thread.

var (
	ole32 = windows.NewLazySystemDLL("ole32.dll")
	gdi32 = windows.NewLazySystemDLL("gdi32.dll")

	procCoCreateInstance   = ole32.NewProc("CoCreateInstance")
	procCreateDIBSection   = gdi32.NewProc("CreateDIBSection")
	procCreateBitmap       = gdi32.NewProc("CreateBitmap")
	procDeleteObject       = gdi32.NewProc("DeleteObject")
	procCreateIconIndirect = user32.NewProc("CreateIconIndirect")
	procDestroyIcon        = user32.NewProc("DestroyIcon")
	procFlashWindowEx      = user32.NewProc("FlashWindowEx")
	procIsWindowVisible    = user32.NewProc("IsWindowVisible")
	procIsZoomed           = user32.NewProc("IsZoomed")
	procIsWindowArranged   = user32.NewProc("IsWindowArranged") // Windows 10 1809 and later
	procShowWindow         = user32.NewProc("ShowWindow")
	procGetLastInputInfo   = user32.NewProc("GetLastInputInfo")
	procGetTickCount       = kernel32.NewProc("GetTickCount")
)

var (
	clsidTaskbarList = guid(0x56FDF344, 0xFD6D, 0x11d0, 0x95, 0x8A, 0x00, 0x60, 0x97, 0xC9, 0xA0, 0x90)
	iidITaskbarList3 = guid(0xEA1AFB91, 0x9E28, 0x4B86, 0x90, 0xE9, 0x9E, 0x9F, 0x8A, 0x5E, 0xEF, 0xAF)
)

// ITaskbarList3's vtable: IUnknown (0–2), ITaskbarList (HrInit 3 …
// SetActiveAlt 7), ITaskbarList2 (MarkFullscreenWindow 8), then
// SetProgressValue 9 … SetOverlayIcon 18.
const (
	vtTaskbarHrInit         = 3
	vtTaskbarSetOverlayIcon = 18
)

const (
	flashwStop       = 0
	flashwAll        = 3 // FLASHW_CAPTION | FLASHW_TRAY
	swShowNoActivate = 4
	swShowNA         = 8
	wmSysCommand     = 0x0112
	scMinimize       = 0xF020
)

// newTaskbar creates an ITaskbarList3 for the calling thread. It's made
// per call, not kept: a COM object from an apartment-threaded thread is
// only valid on that thread while its apartment lives, and a package-wide
// one kept from the first window's thread was called from a later
// window's thread once that one had gone (an access violation in
// SetOverlayIcon, flake-check's TestCloseToTrayWindow, run 43450803946).
// Thread ids are reused, so a per-thread cache isn't safe either; badge
// changes are rare, so creating it each time costs nothing that matters.
func newTaskbar() (*rtObject, error) {
	// Gio's window thread is an OLE apartment already; elsewhere (a test's
	// own thread) this makes it one. S_FALSE and RPC_E_CHANGED_MODE are fine.
	_ = windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED)
	var p *rtObject
	hr, _, _ := procCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsidTaskbarList)), 0, windows.CLSCTX_INPROC_SERVER,
		uintptr(unsafe.Pointer(&iidITaskbarList3)), uintptr(unsafe.Pointer(&p)))
	if failed(hr) || p == nil {
		return nil, fmt.Errorf("CoCreateInstance(CLSID_TaskbarList): %w", HResultError(hr))
	}
	if hr := p.call(vtTaskbarHrInit); failed(hr) {
		p.release()
		return nil, fmt.Errorf("ITaskbarList3::HrInit: %w", HResultError(hr))
	}
	return p, nil
}

// SetTaskbarOverlay is Slack's vc for count: the badge image as hwnd's
// taskbar overlay with description as its accessible text, or, for 0,
// the overlay cleared (createEmpty: a null icon and ""). The window's
// thread.
func SetTaskbarOverlay(hwnd uintptr, count int, description string) error {
	taskbar, err := newTaskbar()
	if err != nil {
		return err
	}
	defer taskbar.release()
	var icon uintptr
	if count > 0 {
		img, err := BadgeOverlay(count)
		if err != nil {
			return err
		}
		icon, err = hiconFromBitmap(img)
		if err != nil {
			return err
		}
		defer procDestroyIcon.Call(icon)
	} else {
		description = ""
	}
	desc, err := windows.UTF16PtrFromString(description)
	if err != nil {
		return err
	}
	if hr := taskbar.call(vtTaskbarSetOverlayIcon, hwnd, icon, uintptr(unsafe.Pointer(desc))); failed(hr) {
		return fmt.Errorf("ITaskbarList3::SetOverlayIcon: %w", HResultError(hr))
	}
	return nil
}

type bitmapV5Header struct {
	Size                            uint32
	Width, Height                   int32
	Planes, BitCount                uint16
	Compression, SizeImage          uint32
	XPelsPerMeter, YPelsPerMeter    int32
	ClrUsed, ClrImportant           uint32
	RedMask, GreenMask, BlueMask    uint32
	AlphaMask, CSType               uint32
	Endpoints                       [9]int32
	GammaRed, GammaGreen, GammaBlue uint32
	Intent, ProfileData             uint32
	ProfileSize, Reserved           uint32
}

type iconInfo struct {
	Icon               int32
	XHotspot, YHotspot uint32
	Mask, Color        windows.Handle
}

const lcsWindowsColorSpace = 0x57696E20 // 'Win '

// hiconFromBitmap is IconUtil::CreateHICONFromSkBitmap: a top-down 32-bit
// BITMAPV5 DIB with the premultiplied pixels copied in as BGRA, and a
// monochrome mask (all transparent when no pixel has alpha, else left as
// CreateBitmap makes it).
func hiconFromBitmap(img *image.RGBA) (uintptr, error) {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	hdr := bitmapV5Header{Width: int32(w), Height: -int32(h), Planes: 1, BitCount: 32,
		RedMask: 0x00FF0000, GreenMask: 0x0000FF00, BlueMask: 0x000000FF, AlphaMask: 0xFF000000, CSType: lcsWindowsColorSpace}
	hdr.Size = uint32(unsafe.Sizeof(hdr))
	var bits unsafe.Pointer
	dib, _, _ := procCreateDIBSection.Call(0, uintptr(unsafe.Pointer(&hdr)), 0 /* DIB_RGB_COLORS */, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if dib == 0 || bits == nil {
		return 0, errors.New("CreateDIBSection failed")
	}
	defer procDeleteObject.Call(dib)
	px := unsafe.Slice((*byte)(bits), w*h*4)
	hasAlpha := false
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			s := img.PixOffset(x, y)
			d := (y*w + x) * 4
			px[d+0], px[d+1], px[d+2], px[d+3] = img.Pix[s+2], img.Pix[s+1], img.Pix[s+0], img.Pix[s+3]
			if img.Pix[s+3] != 0 {
				hasAlpha = true
			}
		}
	}
	var maskBits *byte
	if !hasAlpha {
		line := (w + 0xF) / 16 * 2
		m := make([]byte, line*h)
		for i := range m {
			m[i] = 0xFF
		}
		maskBits = &m[0]
	}
	mono, _, _ := procCreateBitmap.Call(uintptr(w), uintptr(h), 1, 1, uintptr(unsafe.Pointer(maskBits)))
	if mono == 0 {
		return 0, errors.New("CreateBitmap failed")
	}
	defer procDeleteObject.Call(mono)
	ii := iconInfo{Icon: 1, Mask: windows.Handle(mono), Color: windows.Handle(dib)}
	icon, _, err := procCreateIconIndirect.Call(uintptr(unsafe.Pointer(&ii)))
	if icon == 0 {
		return 0, fmt.Errorf("CreateIconIndirect: %v", err)
	}
	return icon, nil
}

type flashWInfo struct {
	Size    uint32
	Hwnd    windows.HWND
	Flags   uint32
	Count   uint32
	Timeout uint32
}

// FlashFrame is BrowserWindow.flashFrame: flash, FlashWindowEx with
// FLASHW_ALL four times at the cursor blink rate (Chromium's FlashFrame;
// FLASHW_TRAY only for a window with a custom region, which Clack's
// isn't); otherwise FLASHW_STOP (Electron's own stop, for "the Chromium's
// implementation has a bug stopping flash").
func FlashFrame(hwnd uintptr, flash bool) {
	fi := flashWInfo{Hwnd: windows.HWND(hwnd), Flags: flashwStop}
	fi.Size = uint32(unsafe.Sizeof(fi))
	if flash {
		fi.Flags, fi.Count = flashwAll, 4
	}
	procFlashWindowEx.Call(uintptr(unsafe.Pointer(&fi)))
}

// WindowVisible is the native window.isVisible Slack's flash asks
// (IsWindowVisible: false for a window hidden to the tray, true for a
// minimized one).
func WindowVisible(hwnd uintptr) bool {
	r, _, _ := procIsWindowVisible.Call(hwnd)
	return r != 0
}

// ShowInactive is BrowserWindow.showInactive: HWNDMessageHandler::Show
// with kInactive, SW_SHOWNA for a maximized or arranged (snapped) window
// so it keeps its place, SW_SHOWNOACTIVATE otherwise. Neither activates
// it.
func ShowInactive(hwnd uintptr) {
	cmd := uintptr(swShowNoActivate)
	if z, _, _ := procIsZoomed.Call(hwnd); z != 0 || windowArranged(hwnd) {
		cmd = swShowNA
	}
	procShowWindow.Call(hwnd, cmd)
}

func windowArranged(hwnd uintptr) bool {
	if procIsWindowArranged.Find() != nil {
		return false
	}
	r, _, _ := procIsWindowArranged.Call(hwnd)
	return r != 0
}

// Minimize is BrowserWindow.minimize on a visible window:
// HWNDMessageHandler::Minimize's ExecuteSystemMenuCommand(SC_MINIMIZE).
func Minimize(hwnd uintptr) {
	procSendMessageW.Call(hwnd, wmSysCommand, scMinimize, 0)
}

type lastInputInfo struct {
	Size uint32
	Time uint32
}

// SystemIdleTime is powerMonitor.getSystemIdleTime: whole seconds since
// the last keyboard or mouse input (GetLastInputInfo against
// GetTickCount, unsigned so a wrap counts right), 0 if Windows won't say.
func SystemIdleTime() int {
	li := lastInputInfo{}
	li.Size = uint32(unsafe.Sizeof(li))
	if r, _, _ := procGetLastInputInfo.Call(uintptr(unsafe.Pointer(&li))); r == 0 {
		return 0
	}
	now, _, _ := procGetTickCount.Call()
	return int((uint32(now) - li.Time) / 1000)
}

// ToastsEnabled is Slack's isWindowsToastNotificationEnabled (de, @1003700
// in the win32 bundle) for Clack's app id: Windows shows its toasts
// (ToastNotifier.Setting Enabled), or it couldn't say (Slack then assumes
// it does).
func ToastsEnabled() bool {
	s, err := ToastSetting(AppUserModelID)
	return err != nil || s == ToastEnabled
}
