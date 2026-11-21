//go:build windows

package composer

import (
	"context"
	"errors"
	"unsafe"

	"gioui.org/app/winclip"
	"golang.org/x/sys/windows"
)

// Windows: Gio reads only CF_UNICODETEXT; the HTML is the registered
// "HTML Format" (CF_HTML), read here with user32 directly, in a
// winclip.Session like every open of the clipboard in the process (the
// paste's file probe reads it at the same time: attach_windows.go).

var (
	pwUser32                     = windows.NewLazySystemDLL("user32.dll")
	pwKernel32                   = windows.NewLazySystemDLL("kernel32.dll")
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
	var b []byte
	err = winclip.Session(ctx, 0, func() error {
		h, _, _ := pwGetClipboardData.Call(f)
		if h == 0 {
			return errClipNoHTML
		}
		n, _, _ := pwGlobalSize.Call(h)
		if n == 0 || n > phMaxHTML+4096 {
			return errClipNoHTML
		}
		p, _, _ := pwGlobalLock.Call(h)
		if p == 0 {
			return errClipNoHTML
		}
		defer pwGlobalUnlock.Call(h)
		b = make([]byte, n)
		pwRtlMoveMemory.Call(uintptr(unsafe.Pointer(&b[0])), p, n)
		return nil
	})
	if err != nil {
		return nil, errClipNoHTML
	}
	return cfHTML(b), nil
}
