package uiatest

import (
	"errors"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ShellHook hears what Explorer's taskbar hears about top-level windows:
// a hidden window registered with RegisterShellHookWindow gets the
// "SHELLHOOK" message for each, and HSHELL_FLASH (HSHELL_REDRAW with
// HSHELL_HIGHBIT) names a window whose taskbar button FlashWindowEx is
// flashing. That's the shell's side of a flash: it proves Windows told
// the taskbar, not what the button looked like.
type ShellHook struct {
	hwnd    uintptr
	flashes chan uintptr
	done    chan struct{}
}

var (
	shUser32                   = windows.NewLazySystemDLL("user32.dll")
	procRegisterClassExW       = shUser32.NewProc("RegisterClassExW")
	procCreateWindowExW        = shUser32.NewProc("CreateWindowExW")
	procDestroyWindow          = shUser32.NewProc("DestroyWindow")
	procDefWindowProcW         = shUser32.NewProc("DefWindowProcW")
	procGetMessageW            = shUser32.NewProc("GetMessageW")
	procDispatchMessageW       = shUser32.NewProc("DispatchMessageW")
	procPostMessageW           = shUser32.NewProc("PostMessageW")
	procPostQuitMessage        = shUser32.NewProc("PostQuitMessage")
	procRegisterWindowMessageW = shUser32.NewProc("RegisterWindowMessageW")
	procRegisterShellHookWnd   = shUser32.NewProc("RegisterShellHookWindow")
	procDeregisterShellHookWnd = shUser32.NewProc("DeregisterShellHookWindow")
)

const (
	hshellFlash = 0x8006 // HSHELL_REDRAW | HSHELL_HIGHBIT
	shWMClose   = 0x0010
	shWMDestroy = 0x0002
)

type shWndClass struct {
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

type shMsg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      [2]int32
	Private uint32
}

var (
	shMu      sync.Mutex
	shCurrent *ShellHook
	shMsgID   uintptr
	shClass   sync.Once
	shProc    uintptr
	shName    = windows.StringToUTF16Ptr("ClackTestShellHook")
)

func shWndProc(hwnd, m, wp, lp uintptr) uintptr {
	shMu.Lock()
	h := shCurrent
	shMu.Unlock()
	switch {
	case h != nil && m == shMsgID:
		if wp == hshellFlash {
			select {
			case h.flashes <- lp:
			default:
			}
		}
		return 0
	case m == shWMClose:
		procDestroyWindow.Call(hwnd)
		return 0
	case m == shWMDestroy:
		procPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, m, wp, lp)
	return r
}

// StartShellHook starts listening (one at a time).
func StartShellHook() (*ShellHook, error) {
	h := &ShellHook{flashes: make(chan uintptr, 64), done: make(chan struct{})}
	errc := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer close(h.done)
		shClass.Do(func() {
			shProc = syscall.NewCallback(shWndProc)
			wc := shWndClass{WndProc: shProc, ClassName: shName}
			wc.Size = uint32(unsafe.Sizeof(wc))
			procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
			id, _, _ := procRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(windows.StringToUTF16Ptr("SHELLHOOK"))))
			shMsgID = id
		})
		shMu.Lock()
		shCurrent = h
		shMu.Unlock()
		hwnd, _, err := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(shName)), 0, 0x80000000 /* WS_POPUP */, 0, 0, 0, 0, 0, 0, 0, 0)
		if hwnd == 0 {
			errc <- err
			return
		}
		h.hwnd = hwnd
		if r, _, err := procRegisterShellHookWnd.Call(hwnd); r == 0 {
			procDestroyWindow.Call(hwnd)
			errc <- err
			return
		}
		errc <- nil
		var m shMsg
		for {
			r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
			if int32(r) <= 0 {
				break
			}
			procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
		}
	}()
	if err := <-errc; err != nil {
		return nil, errors.Join(errors.New("uiatest: shell hook window"), err)
	}
	return h, nil
}

// Drain forgets the flashes heard so far.
func (h *ShellHook) Drain() {
	for {
		select {
		case <-h.flashes:
		default:
			return
		}
	}
}

// WaitFlash reports whether the shell heard hwnd's taskbar button flash
// within d.
func (h *ShellHook) WaitFlash(hwnd uintptr, d time.Duration) bool {
	t := time.After(d)
	for {
		select {
		case got := <-h.flashes:
			if got == hwnd {
				return true
			}
		case <-t:
			return false
		}
	}
}

// Close stops listening.
func (h *ShellHook) Close() {
	procDeregisterShellHookWnd.Call(h.hwnd)
	procPostMessageW.Call(h.hwnd, shWMClose, 0, 0)
	<-h.done
	shMu.Lock()
	if shCurrent == h {
		shCurrent = nil
	}
	shMu.Unlock()
}
