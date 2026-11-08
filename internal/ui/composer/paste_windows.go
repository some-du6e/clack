//go:build windows

package composer

import (
	"context"
	"errors"
	"runtime"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows: Gio reads only CF_UNICODETEXT; the HTML is the registered
// "HTML Format" (CF_HTML), read here with user32 directly.

var (
	pwUser32                     = windows.NewLazySystemDLL("user32.dll")
	pwKernel32                   = windows.NewLazySystemDLL("kernel32.dll")
	pwOpenClipboard              = pwUser32.NewProc("OpenClipboard")
	pwCloseClipboard             = pwUser32.NewProc("CloseClipboard")
	pwGetClipboardData           = pwUser32.NewProc("GetClipboardData")
	pwIsClipboardFormatAvailable = pwUser32.NewProc("IsClipboardFormatAvailable")
	pwRegisterClipboardFormatW   = pwUser32.NewProc("RegisterClipboardFormatW")
	pwGlobalLock                 = pwKernel32.NewProc("GlobalLock")
	pwGlobalUnlock               = pwKernel32.NewProc("GlobalUnlock")
	pwGlobalSize                 = pwKernel32.NewProc("GlobalSize")
	pwRtlMoveMemory              = pwKernel32.NewProc("RtlMoveMemory")
)

var errClipNoHTML = errors.New("no html on the clipboard")

func clipHTMLSupported() bool { return pwUser32.Load() == nil && pwKernel32.Load() == nil }

func clipReadHTML(ctx context.Context) ([]byte, error) {
	name, err := windows.UTF16PtrFromString("HTML Format")
	if err != nil {
		return nil, err
	}
	f, _, _ := pwRegisterClipboardFormatW.Call(uintptr(unsafe.Pointer(name)))
	if f == 0 {
		return nil, errClipNoHTML
	}
	if ok, _, _ := pwIsClipboardFormatAvailable.Call(f); ok == 0 {
		return nil, errClipNoHTML
	}
	// The clipboard belongs to the thread that opened it.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	// Another window (Gio reading the text) may hold it for a moment.
	for i := 0; ; i++ {
		if r, _, _ := pwOpenClipboard.Call(0); r != 0 {
			break
		}
		if i == 20 || ctx.Err() != nil {
			return nil, errClipNoHTML
		}
		time.Sleep(5 * time.Millisecond)
	}
	defer pwCloseClipboard.Call()
	h, _, _ := pwGetClipboardData.Call(f)
	if h == 0 {
		return nil, errClipNoHTML
	}
	n, _, _ := pwGlobalSize.Call(h)
	if n == 0 || n > phMaxHTML+4096 {
		return nil, errClipNoHTML
	}
	p, _, _ := pwGlobalLock.Call(h)
	if p == 0 {
		return nil, errClipNoHTML
	}
	b := make([]byte, n)
	pwRtlMoveMemory.Call(uintptr(unsafe.Pointer(&b[0])), p, n)
	pwGlobalUnlock.Call(h)
	return cfHTML(b), nil
}
