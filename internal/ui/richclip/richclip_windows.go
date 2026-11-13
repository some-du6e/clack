//go:build windows

package richclip

import (
	"context"
	"errors"
	"runtime"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	rcUser32                     = windows.NewLazySystemDLL("user32.dll")
	rcKernel32                   = windows.NewLazySystemDLL("kernel32.dll")
	rcOpenClipboard              = rcUser32.NewProc("OpenClipboard")
	rcCloseClipboard             = rcUser32.NewProc("CloseClipboard")
	rcEmptyClipboard             = rcUser32.NewProc("EmptyClipboard")
	rcSetClipboardData           = rcUser32.NewProc("SetClipboardData")
	rcGetClipboardData           = rcUser32.NewProc("GetClipboardData")
	rcIsClipboardFormatAvailable = rcUser32.NewProc("IsClipboardFormatAvailable")
	rcRegisterClipboardFormatW   = rcUser32.NewProc("RegisterClipboardFormatW")
	rcGlobalAlloc                = rcKernel32.NewProc("GlobalAlloc")
	rcGlobalFree                 = rcKernel32.NewProc("GlobalFree")
	rcGlobalLock                 = rcKernel32.NewProc("GlobalLock")
	rcGlobalUnlock               = rcKernel32.NewProc("GlobalUnlock")
	rcGlobalSize                 = rcKernel32.NewProc("GlobalSize")
	rcRtlMoveMemory              = rcKernel32.NewProc("RtlMoveMemory")
)

const (
	cfUnicodeText = 13
	gmemMoveable  = 0x0002
	// Chromium's names (clipboard_format_type_win.cc)
	fmtHTML   = "HTML Format"
	fmtCustom = "Chromium Web Custom MIME Data Format"
)

func supported() bool { return rcUser32.Load() == nil && rcKernel32.Load() == nil }

func register(name string) uintptr {
	p, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return 0
	}
	f, _, _ := rcRegisterClipboardFormatW.Call(uintptr(unsafe.Pointer(p)))
	return f
}

// open opens the clipboard on a locked thread; the caller defers close
// and UnlockOSThread.
func open(ctx context.Context) error {
	for i := 0; ; i++ {
		if r, _, _ := rcOpenClipboard.Call(0); r != 0 {
			return nil
		}
		if i == 20 || ctx.Err() != nil {
			return errors.New("the clipboard is busy")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func write(ctx context.Context, c Copy) error {
	if !supported() {
		return ErrUnsupported
	}
	runtime.LockOSThread() // the clipboard belongs to the thread that opened it
	defer runtime.UnlockOSThread()
	if err := open(ctx); err != nil {
		return err
	}
	defer rcCloseClipboard.Call()
	if r, _, _ := rcEmptyClipboard.Call(); r == 0 {
		return errors.New("couldn't empty the clipboard")
	}
	u := utf16.Encode([]rune(c.Text))
	tb := make([]byte, 2*len(u)+2)
	for i, v := range u {
		tb[2*i], tb[2*i+1] = byte(v), byte(v>>8)
	}
	if err := setData(cfUnicodeText, tb); err != nil {
		return err
	}
	if c.HTML != "" {
		if f := register(fmtHTML); f != 0 {
			_ = setData(f, append(CFHTML(c.HTML), 0))
		}
	}
	if len(c.Custom) > 0 {
		if f := register(fmtCustom); f != 0 {
			_ = setData(f, EncodeCustom(c.Custom))
		}
	}
	return nil
}

func setData(f uintptr, data []byte) error {
	h, _, _ := rcGlobalAlloc.Call(gmemMoveable, uintptr(len(data)))
	if h == 0 {
		return errors.New("out of memory")
	}
	p, _, _ := rcGlobalLock.Call(h)
	if p == 0 {
		rcGlobalFree.Call(h)
		return errors.New("couldn't lock the clipboard memory")
	}
	if len(data) > 0 {
		rcRtlMoveMemory.Call(p, uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)))
	}
	rcGlobalUnlock.Call(h)
	if r, _, _ := rcSetClipboardData.Call(f, h); r == 0 {
		rcGlobalFree.Call(h) // still ours when SetClipboardData fails
		return errors.New("the clipboard refused the data")
	}
	return nil
}

func readCustom(ctx context.Context) (map[string]string, error) {
	if !supported() {
		return nil, ErrUnsupported
	}
	f := register(fmtCustom)
	if f == 0 {
		return nil, nil
	}
	if ok, _, _ := rcIsClipboardFormatAvailable.Call(f); ok == 0 {
		return nil, nil
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := open(ctx); err != nil {
		return nil, err
	}
	defer rcCloseClipboard.Call()
	h, _, _ := rcGetClipboardData.Call(f)
	if h == 0 {
		return nil, nil
	}
	n, _, _ := rcGlobalSize.Call(h)
	if n == 0 || n > maxCustom {
		return nil, nil
	}
	p, _, _ := rcGlobalLock.Call(h)
	if p == 0 {
		return nil, nil
	}
	b := make([]byte, n)
	rcRtlMoveMemory.Call(uintptr(unsafe.Pointer(&b[0])), p, n)
	rcGlobalUnlock.Call(h)
	return DecodeCustom(b)
}
