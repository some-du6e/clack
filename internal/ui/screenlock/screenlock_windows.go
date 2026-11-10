//go:build windows

package screenlock

import (
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows: a message-only window on its own locked thread, registered with
// WTSRegisterSessionNotification(NOTIFY_FOR_THIS_SESSION), gets
// WM_WTSSESSION_CHANGE with WTS_SESSION_LOCK / WTS_SESSION_UNLOCK.

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	wtsapi32 = windows.NewLazySystemDLL("wtsapi32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procRegisterClassExW  = user32.NewProc("RegisterClassExW")
	procCreateWindowExW   = user32.NewProc("CreateWindowExW")
	procDestroyWindow     = user32.NewProc("DestroyWindow")
	procDefWindowProcW    = user32.NewProc("DefWindowProcW")
	procGetMessageW       = user32.NewProc("GetMessageW")
	procDispatchMessageW  = user32.NewProc("DispatchMessageW")
	procPostMessageW      = user32.NewProc("PostMessageW")
	procPostQuitMessage   = user32.NewProc("PostQuitMessage")
	procGetModuleHandleW  = kernel32.NewProc("GetModuleHandleW")
	procWTSRegister       = wtsapi32.NewProc("WTSRegisterSessionNotification")
	procWTSUnRegister     = wtsapi32.NewProc("WTSUnRegisterSessionNotification")
	errNoWindow           = syscall.Errno(0)
	registerClassOnce     sync.Once
	registerClassErr      error
	windowsMu             sync.Mutex
	windowsByHWND         = map[uintptr]func(bool){}
	wndProcCallback       = syscall.NewCallback(wndProc)
	testHookWindowCreated func(hwnd uintptr) // tests: send it session changes
)

const (
	hwndMessage          = ^uintptr(2) // HWND_MESSAGE (-3)
	wmClose              = 0x0010
	wmDestroy            = 0x0002
	wmWTSSessionChange   = 0x02B1
	wtsSessionLock       = 0x7
	wtsSessionUnlock     = 0x8
	notifyForThisSession = 0
	className            = "ClackScreenLock"
)

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   windows.Handle
	Icon       windows.Handle
	Cursor     windows.Handle
	Background windows.Handle
	MenuName   *uint16
	ClassName  *uint16
	IconSm     windows.Handle
}

type msg struct {
	Wnd     uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      struct{ X, Y int32 }
	Private uint32
}

func wndProc(hwnd, m, wp, lp uintptr) uintptr {
	switch m {
	case wmWTSSessionChange:
		windowsMu.Lock()
		f := windowsByHWND[hwnd]
		windowsMu.Unlock()
		if f != nil {
			switch wp {
			case wtsSessionLock:
				f(true)
			case wtsSessionUnlock:
				f(false)
			}
		}
		return 0
	case wmClose:
		procDestroyWindow.Call(hwnd)
		return 0
	case wmDestroy:
		procPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, m, wp, lp)
	return r
}

func watch(send func(bool)) (func(), error) {
	type result struct {
		hwnd uintptr
		err  error
	}
	ready := make(chan result, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		inst, _, _ := procGetModuleHandleW.Call(0)
		name, _ := windows.UTF16PtrFromString(className)
		registerClassOnce.Do(func() {
			wc := wndClassEx{WndProc: wndProcCallback, Instance: windows.Handle(inst), ClassName: name}
			wc.Size = uint32(unsafe.Sizeof(wc))
			if r, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
				registerClassErr = err
			}
		})
		if registerClassErr != nil {
			ready <- result{err: registerClassErr}
			return
		}
		hwnd, _, err := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(name)), 0, 0, 0, 0, 0, 0, hwndMessage, 0, inst, 0)
		if hwnd == 0 {
			ready <- result{err: err}
			return
		}
		windowsMu.Lock()
		windowsByHWND[hwnd] = send
		windowsMu.Unlock()
		if r, _, err := procWTSRegister.Call(hwnd, notifyForThisSession); r == 0 {
			procDestroyWindow.Call(hwnd)
			windowsMu.Lock()
			delete(windowsByHWND, hwnd)
			windowsMu.Unlock()
			ready <- result{err: err}
			return
		}
		if h := testHookWindowCreated; h != nil {
			h(hwnd)
		}
		ready <- result{hwnd: hwnd}
		var m msg
		for {
			r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
			if int32(r) <= 0 {
				break
			}
			procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
		}
		procWTSUnRegister.Call(hwnd)
		windowsMu.Lock()
		delete(windowsByHWND, hwnd)
		windowsMu.Unlock()
	}()
	r := <-ready
	if r.err != nil {
		if r.err == errNoWindow {
			return nil, ErrUnsupported
		}
		return nil, r.err
	}
	return func() { procPostMessageW.Call(r.hwnd, wmClose, 0, 0) }, nil
}
