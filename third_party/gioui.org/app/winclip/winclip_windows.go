// SPDX-License-Identifier: Unlicense OR MIT

//go:build windows

package winclip

import (
	"context"
	"errors"
	"runtime"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32                  = windows.NewLazySystemDLL("user32.dll")
	kernel32                = windows.NewLazySystemDLL("kernel32.dll")
	openClipboard           = user32.NewProc("OpenClipboard")
	closeClipboard          = user32.NewProc("CloseClipboard")
	enumClipboardFormats    = user32.NewProc("EnumClipboardFormats")
	getClipboardFormatNameW = user32.NewProc("GetClipboardFormatNameW")
	getClipboardData        = user32.NewProc("GetClipboardData")
	globalSize              = kernel32.NewProc("GlobalSize")
	globalLock              = kernel32.NewProc("GlobalLock")
	globalUnlock            = kernel32.NewProc("GlobalUnlock")
	rtlMoveMemory           = kernel32.NewProc("RtlMoveMemory")
)

// ErrBusy is a session that couldn't start before its context ended:
// another session of this process went on too long, or another app kept
// the clipboard open.
var ErrBusy = errors.New("the clipboard is busy")

// sem is the process's one session.
var sem = make(chan struct{}, 1)

// openBudget and openPause: another app may hold the clipboard for a
// moment (a clipboard manager reading a new copy), so the open is tried
// every 5 ms for up to 80 ms, then the session fails (ErrBusy).
// Chromium's ScopedClipboard::Acquire tries 5 times with Sleep(5) between,
// and each Sleep lasts a timer tick (15.6 ms by default): about 60-80 ms
// in all. The wait for this process's own sessions comes before and isn't
// counted.
const (
	openBudget = 80 * time.Millisecond
	openPause  = 5 * time.Millisecond
)

// Session runs f with the clipboard open, after every other session of
// the process has closed it, and closes it when f returns. It runs on a
// locked OS thread (the clipboard belongs to the thread that opened it),
// and opens it for window owner (0 for none: an EmptyClipboard in f then
// leaves no owner). f must copy out what it reads before it returns, and
// must not start another session. ctx bounds the wait for the session and
// the open.
func Session(ctx context.Context, owner uintptr, f func() error) error {
	// red2: no semaphore (proof run: TestWinClipboardReadersOneAtATime must fail)
	_ = sem
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	for start := time.Now(); ; {
		if r, _, _ := openClipboard.Call(owner); r != 0 {
			break
		}
		if time.Since(start) >= openBudget || ctx.Err() != nil {
			return ErrBusy
		}
		time.Sleep(openPause)
	}
	defer closeClipboard.Call()
	return f()
}

// Read takes a snapshot in one session: the clipboard's formats, and a
// copy of the data of each format want gives a limit (in bytes; 0 skips
// it). A format past its limit is in TooBig, unread. The copies are made
// before the session closes; parse them after.
func Read(ctx context.Context, want func(Format) int64) (*Snapshot, error) {
	return ReadPlan(ctx, func(fs []Format) map[uint32]int64 {
		m := map[uint32]int64{}
		for _, f := range fs {
			if limit := want(f); limit > 0 {
				m[f.ID] = limit
			}
		}
		return m
	})
}

// ReadPlan is Read with the limits decided from the whole format list:
// plan gets the clipboard's formats and returns each one's limit (a
// format it leaves out isn't read). A paste skips an office app's
// rendered picture that way, before copying any of it.
func ReadPlan(ctx context.Context, plan func([]Format) map[uint32]int64) (*Snapshot, error) {
	s := &Snapshot{Data: map[uint32][]byte{}, TooBig: map[uint32]bool{}}
	err := Session(ctx, 0, func() error {
		s.Formats = formats()
		limits := plan(s.Formats)
		for _, f := range s.Formats {
			limit := limits[f.ID]
			if limit <= 0 {
				continue
			}
			b, err := CopyData(f.ID, limit)
			switch {
			case err == nil:
				s.Data[f.ID] = b
			case errors.Is(err, ErrTooBig):
				s.TooBig[f.ID] = true
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s, nil
}

// formats lists the open clipboard's formats (at most maxFormats).
func formats() []Format {
	var fs []Format
	var f uintptr
	for len(fs) < maxFormats {
		f, _, _ = enumClipboardFormats.Call(f)
		if f == 0 {
			break
		}
		x := Format{ID: uint32(f)}
		if f >= 0xc000 {
			var buf [256]uint16
			if n, _, _ := getClipboardFormatNameW.Call(f, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf))); n > 0 && n <= uintptr(len(buf)) {
				x.Name = windows.UTF16ToString(buf[:n])
			}
		}
		fs = append(fs, x)
	}
	return fs
}

// The standard formats whose handle isn't an HGLOBAL (a bitmap, a
// palette, a metafile, an owner-drawn one): GlobalSize and GlobalLock are
// wrong on them.
var gdiFormats = map[uint32]bool{
	2:    true, // CF_BITMAP
	3:    true, // CF_METAFILEPICT (memory, but holding a metafile handle)
	9:    true, // CF_PALETTE
	14:   true, // CF_ENHMETAFILE
	0x80: true, // CF_OWNERDISPLAY
	0x82: true, // CF_DSPBITMAP
	0x83: true, // CF_DSPMETAFILEPICT
	0x8e: true, // CF_DSPENHMETAFILE
}

// CopyData copies format id's data out of the open clipboard (call it in a
// Session): its global memory block, GlobalSize bytes (the block's size,
// at least the data's), copied while it is locked and never past it.
// ErrNoData for a null handle, an empty block or one that won't lock;
// ErrTooBig past limit; ErrNotMemory for a format whose handle isn't
// global memory.
func CopyData(id uint32, limit int64) ([]byte, error) {
	if gdiFormats[id] {
		return nil, ErrNotMemory
	}
	h, _, _ := getClipboardData.Call(uintptr(id))
	if h == 0 {
		return nil, ErrNoData
	}
	n, _, _ := globalSize.Call(h)
	if n == 0 {
		return nil, ErrNoData
	}
	if int64(n) > limit || int64(n) < 0 {
		return nil, ErrTooBig
	}
	p, _, _ := globalLock.Call(h)
	if p == 0 {
		return nil, ErrNoData
	}
	defer globalUnlock.Call(h)
	b := make([]byte, n)
	rtlMoveMemory.Call(uintptr(unsafe.Pointer(&b[0])), p, n) // no uintptr becomes a Go pointer
	return b, nil
}
