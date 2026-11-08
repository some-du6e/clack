// Package uiguard knows which goroutine is the UI goroutine, so code that
// may block can check it isn't on it.
//
// On macOS Gio delivers window events and waits for each frame on the
// AppKit main thread: while the UI goroutine handles an event (a frame,
// posted work) the main thread waits for it. Anything slow there (exec,
// file I/O, network, a D-Bus or cgo call that waits on the main thread)
// beachballs the whole app. Functions that may block call OffUI first;
// on the UI goroutine that is logged once per call site and recorded for
// tests (TestStartupStatesNeverBlock).
package uiguard

import (
	"log"
	"runtime"
	"sync"
	"sync/atomic"
)

var uiGID atomic.Int64

// MarkUI records the calling goroutine as the UI goroutine (ui.App.Run;
// tests that drive frames themselves).
func MarkUI() { uiGID.Store(goid()) }

// Unmark forgets the UI goroutine (tests).
func Unmark() { uiGID.Store(0) }

// OnUI reports whether the caller is the UI goroutine.
func OnUI() bool {
	g := uiGID.Load()
	return g != 0 && goid() == g
}

var seen struct {
	sync.Mutex
	what map[string]bool
	list []string
}

// OffUI is called first by functions that may block. On the UI goroutine
// it logs (once per what) and records a violation; it never panics, so a
// missed case is a slow frame, not a crash.
func OffUI(what string) {
	if uiGID.Load() == 0 || !OnUI() {
		return
	}
	seen.Lock()
	defer seen.Unlock()
	seen.list = append(seen.list, what)
	if seen.what == nil {
		seen.what = map[string]bool{}
	}
	if !seen.what[what] {
		seen.what[what] = true
		log.Printf("ui: %s on the UI goroutine (this can freeze the window)", what)
	}
}

// Violations returns the OffUI calls made on the UI goroutine since the
// last Reset.
func Violations() []string {
	seen.Lock()
	defer seen.Unlock()
	return append([]string(nil), seen.list...)
}

// Reset clears the recorded violations.
func Reset() {
	seen.Lock()
	seen.list = nil
	seen.what = nil
	seen.Unlock()
}

// goid is the current goroutine's id, from its stack header
// ("goroutine 18 [running]:"). About a microsecond; only blocking paths
// call it.
func goid() int64 {
	var b [64]byte
	n := runtime.Stack(b[:], false)
	s := b[:n]
	const p = "goroutine "
	if len(s) < len(p) {
		return -1
	}
	var id int64
	for _, c := range s[len(p):] {
		if c < '0' || c > '9' {
			break
		}
		id = id*10 + int64(c-'0')
	}
	return id
}
