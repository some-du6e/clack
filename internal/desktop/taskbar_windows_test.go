//go:build windows

package desktop

import (
	"os"
	"runtime"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/some-du6e/clack/internal/desktop/uiatest"
)

// The taskbar calls on a real Windows desktop (CLACK_WINDOWS_TASKBAR_TESTS=1,
// ci's windows-applog job), on a plain overlapped window of the test's own
// on a locked thread, as Clack makes them on Gio's window thread.
//
// What it proves: ITaskbarList3::SetOverlayIcon takes Slack's badge for a
// count and the clear (S_OK once the window's taskbar button exists), the
// icon Windows gets holds BadgeOverlay's pixels, FlashWindowEx reaches the
// shell (its HSHELL_FLASH for the window), ShowInactive shows a hidden
// window without taking the foreground and Minimize iconifies it, and the
// idle time drops on input. What it doesn't: how Explorer draws the
// overlay (the screenshots in toast-shots show it), or the flash's
// colours.

var (
	ttGetIconInfo        = user32.NewProc("GetIconInfo")
	ttGetDIBits          = gdi32.NewProc("GetDIBits")
	ttGetDC              = user32.NewProc("GetDC")
	ttReleaseDC          = user32.NewProc("ReleaseDC")
	ttIsIconic           = user32.NewProc("IsIconic")
	ttGetForeground      = user32.NewProc("GetForegroundWindow")
	ttSetWindowPos       = user32.NewProc("SetWindowPos")
	ttPeekMessage        = user32.NewProc("PeekMessageW")
	ttSendInput          = user32.NewProc("SendInput")
	ttTaskbarButtonMsgID uintptr
	ttButtonCreated      bool
)

func taskbarTests(t *testing.T) {
	t.Helper()
	if os.Getenv("CLACK_WINDOWS_TASKBAR_TESTS") == "" {
		t.Skip("CLACK_WINDOWS_TASKBAR_TESTS not set")
	}
}

func ttWndProc(hwnd, m, wp, lp uintptr) uintptr {
	if ttTaskbarButtonMsgID != 0 && m == ttTaskbarButtonMsgID {
		ttButtonCreated = true
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, m, wp, lp)
	return r
}

// testWindow is a visible overlapped window with a taskbar button.
func testWindow(t *testing.T) uintptr {
	t.Helper()
	ttTaskbarButtonMsgID, _, _ = procRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(windows.StringToUTF16Ptr("TaskbarButtonCreated"))))
	name := windows.StringToUTF16Ptr("ClackTaskbarTest")
	wc := wndClassEx{WndProc: syscall.NewCallback(ttWndProc), ClassName: name}
	wc.Size = uint32(unsafe.Sizeof(wc))
	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	title := windows.StringToUTF16Ptr("Clack taskbar test")
	const wsOverlappedWindow, wsVisible = 0x00CF0000, 0x10000000
	hwnd, _, err := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(title)),
		wsOverlappedWindow|wsVisible, 100, 100, 480, 320, 0, 0, 0, 0)
	if hwnd == 0 {
		t.Fatalf("CreateWindowEx: %v", err)
	}
	t.Cleanup(func() { procDestroyWindow.Call(hwnd) })
	return hwnd
}

// flashHeard pumps the test window's messages (the flash's on and off
// toggles run on its thread, as its timer) until the shell hears hwnd's
// HSHELL_FLASH, or d passes.
func flashHeard(hook *uiatest.ShellHook, hwnd uintptr, d time.Duration) bool {
	end := time.Now().Add(d)
	for time.Now().Before(end) {
		pump(50 * time.Millisecond)
		if hook.WaitFlash(hwnd, 10*time.Millisecond) {
			return true
		}
	}
	return false
}

// pump runs the thread's messages for d.
func pump(d time.Duration) {
	var m [48]byte
	end := time.Now().Add(d)
	for time.Now().Before(end) {
		for {
			r, _, _ := ttPeekMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0, 1 /* PM_REMOVE */)
			if r == 0 {
				break
			}
			procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
			procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestTaskbarOverlayIcon(t *testing.T) {
	taskbarTests(t)
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	hwnd := testWindow(t)
	for i := 0; i < 200 && !ttButtonCreated; i++ {
		pump(50 * time.Millisecond)
	}
	t.Logf("TaskbarButtonCreated: %v", ttButtonCreated)
	for _, n := range []int{1, 42, 150} {
		if err := SetTaskbarOverlay(hwnd, n, "You have unread messages"); err != nil {
			t.Fatalf("SetTaskbarOverlay(%d): %v", n, err)
		}
		pump(300 * time.Millisecond)
		if n == 42 {
			screenshot(t, "taskbar-overlay-42")
		}
	}
	if err := SetTaskbarOverlay(hwnd, 0, "ignored"); err != nil {
		t.Fatalf("SetTaskbarOverlay(0): %v", err)
	}
	pump(300 * time.Millisecond)
	screenshot(t, "taskbar-overlay-cleared")
}

// The HICON is the overlay's pixels: GetIconInfo's colour bitmap read back
// with GetDIBits as top-down 32-bit BGRA.
func TestTaskbarOverlayHICON(t *testing.T) {
	taskbarTests(t)
	img, err := BadgeOverlay(7)
	if err != nil {
		t.Fatal(err)
	}
	icon, err := hiconFromBitmap(img)
	if err != nil {
		t.Fatal(err)
	}
	defer procDestroyIcon.Call(icon)
	var ii iconInfo
	if r, _, err := ttGetIconInfo.Call(icon, uintptr(unsafe.Pointer(&ii))); r == 0 {
		t.Fatalf("GetIconInfo: %v", err)
	}
	defer procDeleteObject.Call(uintptr(ii.Color))
	defer procDeleteObject.Call(uintptr(ii.Mask))
	hdr := bitmapV5Header{Width: 16, Height: -16, Planes: 1, BitCount: 32}
	hdr.Size = 40 // a BITMAPINFOHEADER
	px := make([]byte, 16*16*4)
	dc, _, _ := ttGetDC.Call(0)
	defer ttReleaseDC.Call(0, dc)
	if r, _, err := ttGetDIBits.Call(dc, uintptr(ii.Color), 0, 16, uintptr(unsafe.Pointer(&px[0])), uintptr(unsafe.Pointer(&hdr)), 0); r != 16 {
		t.Fatalf("GetDIBits: %d %v", r, err)
	}
	for i := 0; i < 16*16; i++ {
		s := img.Pix[i*4:]
		d := px[i*4:]
		if d[0] != s[2] || d[1] != s[1] || d[2] != s[0] || d[3] != s[3] {
			t.Fatalf("pixel %d: icon BGRA %v, overlay RGBA %v", i, d[:4], s[:4])
		}
	}
}

// FlashFrame reaches the shell; ShowInactive and Minimize are the
// "idle" flash's revive of a window hidden to the tray.
func TestTaskbarFlashAndRevive(t *testing.T) {
	taskbarTests(t)
	hook, err := uiatest.StartShellHook()
	if err != nil {
		t.Fatal(err)
	}
	defer hook.Close()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	hwnd := testWindow(t)
	pump(time.Second)
	t.Logf("the test window %#x; shell hook messages so far: %v", hwnd, hook.Seen())

	Minimize(hwnd) // not the active window any more: the button flashes
	pump(300 * time.Millisecond)
	if r, _, _ := ttIsIconic.Call(hwnd); r == 0 {
		t.Fatal("Minimize didn't iconify the window")
	}
	hook.Drain()
	FlashFrame(hwnd, true)
	if !flashHeard(hook, hwnd, 5*time.Second) {
		t.Fatalf("no HSHELL_FLASH for the window after FlashFrame(true); the shell hook heard %v", hook.Seen())
	}
	FlashFrame(hwnd, false)
	pump(200 * time.Millisecond)
	screenshot(t, "taskbar-flash-stopped")

	// hidden the way Gio patch 13 hides (SWP_HIDEWINDOW | SWP_NOACTIVATE …)
	ttSetWindowPos.Call(hwnd, 0, 0, 0, 0, 0, 0x0080|0x0010|0x0002|0x0200|0x0001|0x0004)
	pump(200 * time.Millisecond)
	if WindowVisible(hwnd) {
		t.Fatal("WindowVisible after SWP_HIDEWINDOW")
	}
	fg, _, _ := ttGetForeground.Call()
	ShowInactive(hwnd)
	Minimize(hwnd)
	pump(300 * time.Millisecond)
	if !WindowVisible(hwnd) {
		t.Fatal("ShowInactive left the window hidden")
	}
	if r, _, _ := ttIsIconic.Call(hwnd); r == 0 {
		t.Fatal("the revived window isn't minimized")
	}
	if now, _, _ := ttGetForeground.Call(); now == hwnd && fg != hwnd {
		t.Fatal("ShowInactive took the foreground")
	}
	hook.Drain()
	FlashFrame(hwnd, true)
	if !flashHeard(hook, hwnd, 5*time.Second) {
		t.Fatalf("no HSHELL_FLASH for the revived window; the shell hook heard %v", hook.Seen())
	}
	pump(500 * time.Millisecond)
	screenshot(t, "taskbar-flash-revived")
	FlashFrame(hwnd, false)
}

type mouseInput struct {
	Type      uint32
	_         uint32
	Dx, Dy    int32
	MouseData uint32
	Flags     uint32
	Time      uint32
	Extra     uintptr
}

// SystemIdleTime counts from the last input: a zero mouse move resets it.
func TestSystemIdleTime(t *testing.T) {
	taskbarTests(t)
	before := SystemIdleTime()
	in := mouseInput{Type: 0 /* INPUT_MOUSE */, Flags: 0x0001 /* MOUSEEVENTF_MOVE */}
	if r, _, err := ttSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in)); r != 1 {
		t.Skipf("SendInput: %v (idle %d s)", err, before)
	}
	time.Sleep(100 * time.Millisecond)
	if n := SystemIdleTime(); n > 1 {
		t.Fatalf("idle %d s right after input (before: %d)", n, before)
	}
	time.Sleep(2100 * time.Millisecond)
	if n := SystemIdleTime(); n < 2 {
		t.Fatalf("idle %d s after 2.1 s without input", n)
	}
	t.Logf("idle before the input: %d s", before)
}
