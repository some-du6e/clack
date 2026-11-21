// SPDX-License-Identifier: Unlicense OR MIT

//go:build windows

package winclip

import (
	"context"
	"errors"
	"runtime"
	"time"

	"golang.org/x/sys/windows"
)

var (
	user32         = windows.NewLazySystemDLL("user32.dll")
	openClipboard  = user32.NewProc("OpenClipboard")
	closeClipboard = user32.NewProc("CloseClipboard")
)

// ErrBusy is a session that couldn't start before its context ended:
// another session of this process went on too long, or another app kept
// the clipboard open.
var ErrBusy = errors.New("the clipboard is busy")

// sem is the process's one session.
var sem = make(chan struct{}, 1)

// openTries bounds the opens: another app may hold the clipboard for a
// moment (a clipboard manager reading a new copy), half a second at most.
const (
	openTries = 50
	openPause = 10 * time.Millisecond
)

// Session runs f with the clipboard open, after every other session of
// the process has closed it, and closes it when f returns. It runs on a
// locked OS thread (the clipboard belongs to the thread that opened it),
// and opens it for window owner (0 for none: an EmptyClipboard in f then
// leaves no owner). f must copy out what it reads before it returns, and
// must not start another session. ctx bounds the wait for the session and
// the open.
func Session(ctx context.Context, owner uintptr, f func() error) error {
	select {
	case sem <- struct{}{}:
	case <-ctx.Done():
		return ErrBusy
	}
	defer func() { <-sem }()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	for i := 0; ; i++ {
		if r, _, _ := openClipboard.Call(owner); r != 0 {
			break
		}
		if i == openTries || ctx.Err() != nil {
			return ErrBusy
		}
		time.Sleep(openPause)
	}
	defer closeClipboard.Call()
	return f()
}
