//go:build windows

package globalkey

import (
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32                 = windows.NewLazySystemDLL("user32.dll")
	procRegisterHotKey     = user32.NewProc("RegisterHotKey")
	procUnregisterHotKey   = user32.NewProc("UnregisterHotKey")
	procGetMessageW        = user32.NewProc("GetMessageW")
	procPostThreadMessageW = user32.NewProc("PostThreadMessageW")
)

const (
	modControl                 = 0x0002
	modShift                   = 0x0004
	modNoRepeat                = 0x4000
	vkSpace                    = 0x20
	wmHotkey                   = 0x0312
	wmQuit                     = 0x0012
	hotkeyID                   = 0x434c // "CL"
	errHotkeyAlreadyRegistered = 1409
)

type msg struct {
	Wnd     uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      struct{ X, Y int32 }
	Private uint32
}

// testHookThread gets the hotkey thread's id (tests post WM_HOTKEY to it).
var testHookThread func(tid uint32)

func register(onPress func()) (func(), error) {
	type result struct {
		tid uint32
		err error
	}
	ready := make(chan result, 1)
	exited := make(chan struct{})
	go func() {
		defer close(exited)
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		tid := windows.GetCurrentThreadId()
		// a NULL window: WM_HOTKEY goes to this thread's queue
		r, _, err := procRegisterHotKey.Call(0, hotkeyID, modControl|modShift|modNoRepeat, vkSpace)
		if r == 0 {
			if en, ok := err.(syscall.Errno); ok && en == errHotkeyAlreadyRegistered {
				err = ErrTaken
			}
			ready <- result{err: err}
			return
		}
		defer procUnregisterHotKey.Call(0, hotkeyID)
		if h := testHookThread; h != nil {
			h(tid)
		}
		ready <- result{tid: tid}
		var m msg
		for {
			r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
			if int32(r) <= 0 { // WM_QUIT or an error
				return
			}
			if m.Message == wmHotkey && m.WParam == hotkeyID {
				go onPress()
			}
		}
	}()
	r := <-ready
	if r.err != nil {
		return nil, r.err
	}
	return func() {
		procPostThreadMessageW.Call(uintptr(r.tid), wmQuit, 0, 0)
		select { // released before stop returns
		case <-exited:
		case <-time.After(2 * time.Second):
		}
	}, nil
}
