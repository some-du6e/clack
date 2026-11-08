package ui

import (
	"sync"
	"sync/atomic"
	"time"
)

// The OS colour scheme (Appearance > "System").
//
// Reading it can block: macOS' old `defaults read -g AppleInterfaceStyle`
// was an exec with a 1 s timeout, Linux asks the XDG portal over D-Bus.
// On macOS Gio runs window events and layout synchronously on the AppKit
// main thread, so any of that in a frame beachballs the whole app; the
// Appearance page used to exec `defaults` on every frame it drew
// (2026-11-08). So the scheme lives in an atomic, written only by the
// platform watcher (osdark_*.go: AppKit's appearance change on macOS, the
// portal's SettingChanged signal on Linux, plus a slow poll off the UI
// goroutine everywhere), and the UI reads osSchemeDark, which never
// blocks. osPrefersDark, the platform's blocking read, is called by the
// watchers only (TestOSPrefersDarkOffUI).

// osScheme: 0 not known (yet), 1 light, 2 dark.
var osScheme atomic.Int32

// osSchemeCh wakes the fan-out goroutine; buffered so a writer on any
// thread (AppKit's main thread included) never waits.
var osSchemeCh = make(chan struct{}, 1)

var osSchemeSubs struct {
	sync.Mutex
	fns   []func()
	start sync.Once
}

// osSchemeDark is the last OS scheme a watcher saw: an atomic load, safe in
// layout. ok is false until one reported (or where the OS has none).
func osSchemeDark() (dark, ok bool) {
	switch osScheme.Load() {
	case 1:
		return false, true
	case 2:
		return true, true
	}
	return false, false
}

// setOSScheme records a successful read. Any goroutine or OS thread: an
// atomic swap and a non-blocking send, nothing else (the macOS observer
// calls it on the main thread).
func setOSScheme(dark bool) {
	n := int32(1)
	if dark {
		n = 2
	}
	if osScheme.Swap(n) != n {
		select {
		case osSchemeCh <- struct{}{}:
		default: // a wake-up is pending; it reads the latest value
		}
	}
}

// onOSScheme calls f (off the UI goroutine) whenever the OS scheme changes,
// and starts the platform watcher on first use.
func onOSScheme(f func()) {
	osSchemeSubs.Lock()
	osSchemeSubs.fns = append(osSchemeSubs.fns, f)
	osSchemeSubs.Unlock()
	osSchemeSubs.start.Do(func() {
		go func() {
			for range osSchemeCh {
				osSchemeSubs.Lock()
				fns := append([]func(){}, osSchemeSubs.fns...)
				osSchemeSubs.Unlock()
				for _, f := range fns {
					f()
				}
			}
		}()
		startOSSchemeWatcher()
	})
	if _, ok := osSchemeDark(); ok {
		f() // a value from before f subscribed
	}
}

// osSchemePoll is the watchers' fallback poll (off the UI goroutine): the
// change signals are the fast path.
const osSchemePoll = 10 * time.Second
