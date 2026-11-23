//go:build windows

package richclip

import (
	"context"
	"errors"
	"unicode/utf16"
	"unsafe"

	"gioui.org/app/winclip"
	"golang.org/x/sys/windows"
)

var (
	rcUser32                   = windows.NewLazySystemDLL("user32.dll")
	rcKernel32                 = windows.NewLazySystemDLL("kernel32.dll")
	rcEmptyClipboard           = rcUser32.NewProc("EmptyClipboard")
	rcSetClipboardData         = rcUser32.NewProc("SetClipboardData")
	rcRegisterClipboardFormatW = rcUser32.NewProc("RegisterClipboardFormatW")
	rcGlobalAlloc              = rcKernel32.NewProc("GlobalAlloc")
	rcGlobalFree               = rcKernel32.NewProc("GlobalFree")
	rcGlobalLock               = rcKernel32.NewProc("GlobalLock")
	rcGlobalUnlock             = rcKernel32.NewProc("GlobalUnlock")
	rcRtlMoveMemory            = rcKernel32.NewProc("RtlMoveMemory")
)

const (
	cfUnicodeText = 13
	gmemMoveable  = 0x0002
	// Chromium's names (clipboard_format_type_win.cc)
	fmtHTML   = HTMLFormat
	fmtCustom = CustomFormat
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

// Every open of the clipboard is a winclip.Session: the process's one at
// a time. Two of a paste's reads at once, each opening it with no window,
// let one's close free the copy the other was still reading (Karim's
// crash, 2026-11-21: 0xc0000374 in GlobalSize).

func write(ctx context.Context, c Copy) error {
	if !supported() {
		return ErrUnsupported
	}
	u := utf16.Encode([]rune(c.Text))
	tb := make([]byte, 2*len(u)+2)
	for i, v := range u {
		tb[2*i], tb[2*i+1] = byte(v), byte(v>>8)
	}
	var html, custom []byte
	var fHTML, fCustom uintptr
	if c.HTML != "" {
		html, fHTML = append(CFHTML(c.HTML), 0), register(fmtHTML)
	}
	if len(c.Custom) > 0 {
		custom, fCustom = EncodeCustom(c.Custom), register(fmtCustom)
	}
	return winclip.Session(ctx, 0, func() error {
		if r, _, _ := rcEmptyClipboard.Call(); r == 0 {
			return errors.New("couldn't empty the clipboard")
		}
		if err := setData(cfUnicodeText, tb); err != nil {
			return err
		}
		if fHTML != 0 {
			_ = setData(fHTML, html)
		}
		if fCustom != 0 {
			_ = setData(fCustom, custom)
		}
		return nil
	})
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
	snap, err := winclip.Read(ctx, func(f winclip.Format) int64 {
		if f.Name == fmtCustom {
			return maxCustom
		}
		return 0
	})
	if err != nil {
		return nil, err
	}
	return CustomFrom(snap), nil
}

// CustomFrom is the web custom data in a snapshot, nil when it has none or
// it doesn't decode (a paste reads it with its other formats in one
// session: composer).
func CustomFrom(snap *winclip.Snapshot) map[string]string {
	f, ok := snap.Named(fmtCustom)
	if !ok {
		return nil
	}
	b, ok := snap.Bytes(f.ID)
	if !ok {
		return nil
	}
	m, err := DecodeCustom(b)
	if err != nil {
		return nil
	}
	return m
}
