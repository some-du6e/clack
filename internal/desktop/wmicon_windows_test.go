//go:build windows

package desktop

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"runtime"
	"testing"
	"time"
	"unsafe"

	"gioui.org/app"
	"golang.org/x/sys/windows"
)

var procPostThreadMessageW = user32.NewProc("PostThreadMessageW")

const wmGetIcon = 0x007F // wmApp is tray_windows.go's

// iconAlive reports whether h is an icon Windows still holds (one freed
// isn't), freeing the bitmaps GetIconInfo copies out (taskbar_windows.go's
// iconInfo, tray_shell_windows_test.go's ttGetIconInfo).
func iconAlive(h uintptr) bool {
	var ii iconInfo
	r, _, _ := ttGetIconInfo.Call(h, uintptr(unsafe.Pointer(&ii)))
	if ii.Mask != 0 {
		procDeleteObject.Call(uintptr(ii.Mask))
	}
	if ii.Color != 0 {
		procDeleteObject.Call(uintptr(ii.Color))
	}
	return r != 0
}

// testIconWindow is a top-level window of the test's own, its class with
// no icon (DefWindowProcW its window procedure), on a thread of its own
// pumping messages; done destroys it.
func testIconWindow(t *testing.T) (hwnd uintptr, done func()) {
	t.Helper()
	type made struct {
		hwnd, tid uintptr
		err       error
	}
	ch := make(chan made, 1)
	go func() {
		runtime.LockOSThread()
		cls, _ := windows.UTF16PtrFromString("ClackIconTest")
		inst, _, _ := procGetModuleHandleW.Call(0)
		wc := wndClassEx{WndProc: procDefWindowProcW.Addr(), Instance: windows.Handle(inst), ClassName: cls}
		wc.Size = uint32(unsafe.Sizeof(wc))
		procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))) // a second test run finds it registered
		h, _, err := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(cls)), uintptr(unsafe.Pointer(cls)),
			wsOverlapped, 0, 0, 64, 64, 0, 0, inst, 0)
		ch <- made{h, uintptr(windows.GetCurrentThreadId()), err}
		if h == 0 {
			return
		}
		var m msg
		for {
			r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
			if int32(r) <= 0 {
				return
			}
			if m.Message == wmApp && m.Wnd == 0 { // done: the window goes on its own thread
				procDestroyWindow.Call(h)
				return
			}
			procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
			procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
		}
	}()
	w := <-ch
	if w.hwnd == 0 {
		t.Fatalf("no test window: %v", w.err)
	}
	return w.hwnd, func() { procPostThreadMessageW.Call(w.tid, wmApp, 0, 0) }
}

func testWinIconPNG(t *testing.T, size int, c color.NRGBA) []byte {
	t.Helper()
	m := image.NewNRGBA(image.Rect(0, 0, size, size))
	for i := 0; i < len(m.Pix); i += 4 {
		m.Pix[i], m.Pix[i+1], m.Pix[i+2], m.Pix[i+3] = c.R, c.G, c.B, c.A
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, m); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestSetAppIconWindows: a plugin's app icon on Windows (Slick's window
// setIcon: CustomAppIcon) is the window's big and small icons
// (WM_SETICON), set from a goroutine of its own. Changes in a row free
// each icon only once WM_SETICON has handed it back as the one it
// replaced: the icons the window shows are alive, and they're the only
// ones left. nil puts the class's (the exe's) back and frees the last.
func TestSetAppIconWindows(t *testing.T) {
	hwnd, done := testIconWindow(t)
	defer done()
	get := func(kind uintptr) uintptr {
		r, _, _ := procSendMessageW.Call(hwnd, wmGetIcon, kind, 0)
		return r
	}
	settled := func(what string, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for {
			winIcon.mu.Lock()
			idle := !winIcon.sending && !winIcon.dirty
			winIcon.mu.Unlock()
			if idle && cond() {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s", what)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	SetWindowIcon(app.Win32ViewEvent{HWND: hwnd})
	if get(iconBig) != 0 || get(iconSmall) != 0 {
		t.Fatal("the test window has icons before any was set")
	}
	SetAppIcon(testWinIconPNG(t, 512, color.NRGBA{R: 0xff, A: 0xff}))
	settled("the plugin's icons", func() bool { return get(iconBig) != 0 && get(iconSmall) != 0 })

	for i, c := range []color.NRGBA{{G: 0xff, A: 0xff}, {B: 0xff, A: 0xff}, {R: 0xff, G: 0xff, A: 0xff}, {A: 0xff}} {
		SetAppIcon(testWinIconPNG(t, 64+16*i, c))
	}
	settled("the last of four changes in a row", func() bool { return get(iconBig) != 0 && get(iconSmall) != 0 })
	big, small := get(iconBig), get(iconSmall)
	if !iconAlive(big) || !iconAlive(small) {
		t.Errorf("an icon the window shows was freed (big %v, small %v)", iconAlive(big), iconAlive(small))
	}
	winIcon.mu.Lock()
	left := len(winIcon.ours)
	shown := winIcon.ours[big] && winIcon.ours[small]
	winIcon.mu.Unlock()
	if left != 2 || !shown {
		t.Errorf("%d icons left unfreed (the shown pair among them %v), want just the pair the window shows", left, shown)
	}

	SetAppIcon(nil)
	settled("the class's icon again", func() bool { return get(iconBig) == 0 && get(iconSmall) == 0 })
	winIcon.mu.Lock()
	left = len(winIcon.ours)
	winIcon.mu.Unlock()
	if left != 0 || iconAlive(big) {
		t.Errorf("after nil: %d icons unfreed, the last shown still alive %v", left, iconAlive(big))
	}
	SetWindowIcon(app.Win32ViewEvent{}) // the window going
}
