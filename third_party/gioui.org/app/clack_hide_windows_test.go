// SPDX-License-Identifier: Unlicense OR MIT

//go:build windows

package app

import (
	"os"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	syscall "golang.org/x/sys/windows"

	"gioui.org/app/internal/windows"
	"gioui.org/io/system"
	"gioui.org/op"
	"gioui.org/widget"
)

// Clack patch (CLACK_PATCHES.md §13) on a real Windows desktop
// (CLACK_WINDOWS_TRAY_TESTS, ci's windows-applog job).

var (
	testIsZoomed          = testUser32.NewProc("IsZoomed")
	testIsWindow          = testUser32.NewProc("IsWindow")
	testSetCaretBlinkTime = testUser32.NewProc("SetCaretBlinkTime")
	testGetWindowText     = testUser32.NewProc("GetWindowTextW")
	testGetForeground     = testUser32.NewProc("GetForegroundWindow")
)

func testWindowText(hwnd syscall.Handle) string {
	buf := make([]uint16, 256)
	n, _, _ := testGetWindowText.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf[:n])
}

func windowTestsEnabled() bool { return os.Getenv("CLACK_WINDOWS_TRAY_TESTS") != "" }

type hideTestWindow struct {
	w       *Window
	hwnd    syscall.Handle
	frames  atomic.Int64
	wakeups atomic.Int64 // events that are neither frames nor config
	// onWakeup, when set, runs on the event goroutine for each of them,
	// before the next Event call
	onWakeup atomic.Pointer[func()]
	closing  chan *ClosingEvent
	done     chan struct{}
}

// hideTestWindow opens a Gio window whose ClosingEvents go to the test.
// abort decides each close (the test's App: runFromTray).
func newHideTestWindow(t *testing.T, abort func() bool, opts ...Option) *hideTestWindow {
	t.Helper()
	h := openHideTestWindow(t, abort, opts...)
	waitUntil(t, "the first frame", func() bool { return h.frames.Load() > 0 })
	time.Sleep(300 * time.Millisecond)
	return h
}

// openHideTestWindow is newHideTestWindow without waiting for a frame.
func openHideTestWindow(t *testing.T, abort func() bool, opts ...Option) *hideTestWindow {
	t.Helper()
	h := &hideTestWindow{w: new(Window), closing: make(chan *ClosingEvent, 4), done: make(chan struct{})}
	h.w.Option(append([]Option{Title("Clack hide test"), Size(480, 360)}, opts...)...)
	hwnds := make(chan syscall.Handle, 1)
	go func() {
		defer close(h.done)
		var ops op.Ops
		for {
			switch e := h.w.Event().(type) {
			case Win32ViewEvent:
				if e.HWND != 0 {
					select {
					case hwnds <- syscall.Handle(e.HWND):
					default:
					}
				}
			case FrameEvent:
				ops.Reset()
				e.Frame(&ops)
				h.frames.Add(1)
			case *ClosingEvent:
				if abort() {
					e.Abort()
					h.w.Hide() // as Clack does: on the UI goroutine, inside the event
				}
				h.closing <- e
			case ConfigEvent:
			case DestroyEvent:
				return
			default:
				h.wakeups.Add(1)
				if f := h.onWakeup.Load(); f != nil {
					(*f)()
				}
			}
		}
	}()
	select {
	case h.hwnd = <-hwnds:
	case <-time.After(30 * time.Second):
		t.Fatal("no Win32ViewEvent")
	}
	return h
}

func waitUntil(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (h *hideTestWindow) visible() bool { return windows.IsWindowVisible(h.hwnd) }

func (h *hideTestWindow) closeWindow(t *testing.T) {
	t.Helper()
	windows.PostMessage(h.hwnd, windows.WM_CLOSE, 0, 0)
	select {
	case <-h.closing:
	case <-time.After(10 * time.Second):
		t.Fatal("no ClosingEvent")
	}
}

// Window.Hide inside a ClosingEvent (aborted) hides the window without
// destroying it; a hidden window draws no frames but still wakes the app
// (Invalidate's event); ActionRaise shows it again, maximized if it was;
// a close that isn't aborted destroys it.
func TestHideAndRaise(t *testing.T) {
	if testing.Short() {
		t.Skip("opens windows")
	}
	if !windowTestsEnabled() {
		t.Skip("CLACK_WINDOWS_TRAY_TESTS not set")
	}
	for _, maximized := range []bool{false, true} {
		name := map[bool]string{false: "windowed", true: "maximized"}[maximized]
		t.Run(name, func(t *testing.T) {
			var abort atomic.Bool
			abort.Store(true)
			var opts []Option
			if maximized {
				opts = append(opts, Maximized.Option())
			}
			h := newHideTestWindow(t, abort.Load, opts...)
			if maximized {
				waitUntil(t, "maximized", func() bool { r, _, _ := testIsZoomed.Call(uintptr(h.hwnd)); return r != 0 })
			}
			h.closeWindow(t)
			if h.visible() {
				t.Fatal("still visible after Hide")
			}
			if r, _, _ := testIsWindow.Call(uintptr(h.hwnd)); r == 0 {
				t.Fatal("Hide destroyed the window")
			}
			time.Sleep(500 * time.Millisecond) // the hide's own WM_WINDOWPOSCHANGED frame
			f0, w0 := h.frames.Load(), h.wakeups.Load()
			for range 5 {
				h.w.Invalidate()
				time.Sleep(100 * time.Millisecond)
			}
			if n := h.frames.Load() - f0; n != 0 {
				t.Errorf("hidden: %d frames after Invalidate, want none", n)
			}
			if h.wakeups.Load() == w0 {
				t.Error("hidden: Invalidate delivered no event to the app")
			}

			h.w.Perform(system.ActionRaise)
			waitUntil(t, "visible after ActionRaise", h.visible)
			waitUntil(t, "a frame after ActionRaise", func() bool { return h.frames.Load() > f0 })
			if r, _, _ := testIsZoomed.Call(uintptr(h.hwnd)); (r != 0) != maximized {
				t.Errorf("after ActionRaise: maximized %v, want %v", r != 0, maximized)
			}

			abort.Store(false)
			h.closeWindow(t)
			select {
			case <-h.done:
			case <-time.After(10 * time.Second):
				t.Fatal("the window didn't close")
			}
		})
	}
}

// Options that leave the mode alone (a title, a minimum size) keep a
// hidden window hidden and a minimized one minimized (Clack patch,
// CLACK_PATCHES.md §20): Clack's title carries the unread count, and each
// change used to bring back a window closed to the tray or restore a
// minimized one. A mode change still applies, and ActionRaise still shows.
func TestOptionKeepsHiddenAndMinimized(t *testing.T) {
	if testing.Short() {
		t.Skip("opens windows")
	}
	if !windowTestsEnabled() {
		t.Skip("CLACK_WINDOWS_TRAY_TESTS not set")
	}
	var abort atomic.Bool
	abort.Store(true)
	h := newHideTestWindow(t, abort.Load)
	iconic := func() bool { return windows.IsIconic(h.hwnd) }

	h.closeWindow(t)
	if h.visible() {
		t.Fatal("still visible after Hide")
	}
	h.w.Option(Title("Clack hide test - 3 new items"))
	h.w.Option(MinSize(400, 300))
	time.Sleep(300 * time.Millisecond)
	if h.visible() {
		t.Fatal("hidden: a title or minimum size showed the window")
	}
	if got := testWindowText(h.hwnd); got != "Clack hide test - 3 new items" {
		t.Errorf("hidden: title %q", got)
	}
	h.w.Perform(system.ActionRaise)
	waitUntil(t, "visible after ActionRaise", h.visible)

	h.w.Option(Minimized.Option())
	waitUntil(t, "minimized", iconic)
	h.w.Option(Title("Clack hide test - 4 new items"))
	h.w.Option(MinSize(420, 300))
	time.Sleep(300 * time.Millisecond)
	if !iconic() || !h.visible() {
		t.Fatalf("minimized: a title or minimum size changed it (iconic %v, visible %v)", iconic(), h.visible())
	}
	h.w.Option(Maximized.Option()) // a mode change still applies
	waitUntil(t, "maximized", func() bool { r, _, _ := testIsZoomed.Call(uintptr(h.hwnd)); return r != 0 && !iconic() })

	abort.Store(false)
	h.closeWindow(t)
	select {
	case <-h.done:
	case <-time.After(10 * time.Second):
		t.Fatal("the window didn't close")
	}
}

// An Invalidate made while the app handles a wake-up of a window that
// draws no frames (hidden, minimized) gets a wake-up of its own (Clack
// patch, CLACK_PATCHES.md §26). Clack runs posted work on those
// wake-ups; a notification's taskbar flash, posted from a goroutine
// while the notification's own wake-up was still being handled, waited
// for the next event of any kind (internal/ui TestTaskbarWindow). The
// Invalidate here comes from another goroutine, as Clack's invalidator's
// does, and the wake-up waits for it, so it always lands while the
// driver is still delivering the first.
func TestInvalidateWhileHandlingWakesHidden(t *testing.T) {
	if testing.Short() {
		t.Skip("opens windows")
	}
	if !windowTestsEnabled() {
		t.Skip("CLACK_WINDOWS_TRAY_TESTS not set")
	}
	for _, mode := range []string{"hidden", "minimized"} {
		t.Run(mode, func(t *testing.T) {
			var abort atomic.Bool
			abort.Store(true)
			h := newHideTestWindow(t, abort.Load)
			if mode == "hidden" {
				h.closeWindow(t) // aborted: Window.Hide
				if h.visible() {
					t.Fatal("still visible after Hide")
				}
			} else {
				h.w.Option(Minimized.Option())
				waitUntil(t, "minimized", func() bool { return windows.IsIconic(h.hwnd) })
			}
			time.Sleep(500 * time.Millisecond) // the hide's or minimize's own events

			// armed: the next wake-up invalidates from another goroutine
			// while the driver still delivers it (mayInvalidate false; a
			// stray wake-up from an Invalidate that skipped the gate
			// re-arms)
			var armed, inner atomic.Bool
			hook := func() {
				if !armed.CompareAndSwap(true, false) {
					return
				}
				h.w.invMu.Lock()
				gated := !h.w.mayInvalidate
				h.w.invMu.Unlock()
				if !gated {
					armed.Store(true)
					return
				}
				done := make(chan struct{})
				go func() { h.w.Invalidate(); close(done) }()
				<-done
				inner.Store(true)
			}
			h.onWakeup.Store(&hook)
			defer h.onWakeup.Store(nil)
			armed.Store(true)
			var w1 int64
			deadline := time.Now().Add(10 * time.Second)
			for !inner.Load() {
				if time.Now().After(deadline) {
					t.Fatal("no wake-up while the driver was still delivering one")
				}
				w1 = h.wakeups.Load()
				h.w.Invalidate()
				time.Sleep(100 * time.Millisecond)
			}
			waitUntil(t, "a wake-up for the Invalidate made while the app handled one", func() bool { return h.wakeups.Load() > w1+1 })

			h.w.Perform(system.ActionRaise)
			waitUntil(t, "shown again", func() bool { return h.visible() && !windows.IsIconic(h.hwnd) })
			abort.Store(false)
			h.closeWindow(t)
			select {
			case <-h.done:
			case <-time.After(10 * time.Second):
				t.Fatal("the window didn't close")
			}
		})
	}
}

// Window.Hide before the window exists, and on a destroyed one, is a
// no-op that reports false.
func TestHideWithoutDriver(t *testing.T) {
	if new(Window).Hide() {
		t.Error("Hide on a window that was never shown reported true")
	}
}

// The caret blinks at the OS's rate: GetCaretBlinkTime, read as a window
// opens (Chromium's OsSettingsProviderWin::CaretBlinkInterval), none for
// INFINITE (the Control Panel's "None"). The test sets the runner's blink
// time with SetCaretBlinkTime and puts it back.
func TestCaretBlinkFromOS(t *testing.T) {
	if !windowTestsEnabled() {
		t.Skip("CLACK_WINDOWS_TRAY_TESTS not set")
	}
	orig := windows.GetCaretBlinkTime()
	t.Logf("the runner's caret blink time: %d ms", orig)
	t.Cleanup(func() {
		testSetCaretBlinkTime.Call(uintptr(orig))
		widget.SetCaretBlinkInterval(-1)
	})
	for _, c := range []struct {
		set  uint32
		want time.Duration
	}{
		{800, 800 * time.Millisecond},
		{530, 530 * time.Millisecond}, // Windows' default
		{windows.INFINITE, 0},
	} {
		if r, _, err := testSetCaretBlinkTime.Call(uintptr(c.set)); r == 0 {
			t.Logf("SetCaretBlinkTime(%#x): %v; skipped", c.set, err)
			continue
		}
		if got := windows.GetCaretBlinkTime(); got != c.set {
			t.Logf("SetCaretBlinkTime(%#x) reads back %#x; skipped", c.set, got)
			continue
		}
		widget.SetCaretBlinkInterval(-1)
		h := newHideTestWindow(t, func() bool { return false })
		if got := widget.CaretBlinkInterval(); got != c.want {
			t.Errorf("GetCaretBlinkTime %#x: CaretBlinkInterval %v, want %v", c.set, got, c.want)
		}
		h.closeWindow(t)
		<-h.done
	}
}

// A window made with StartHidden (Clack patch, CLACK_PATCHES.md §23) is
// never shown by its creation: not visible, not the foreground window;
// past the frames its creation's configure draws (Gio's update on
// WM_WINDOWPOSCHANGED and WM_STYLECHANGED), Invalidate draws nothing but
// still wakes the app; a title change keeps it hidden (§20); ActionRaise
// shows it and it draws; it closes as usual.
func TestStartHidden(t *testing.T) {
	if testing.Short() {
		t.Skip("opens windows")
	}
	if !windowTestsEnabled() {
		t.Skip("CLACK_WINDOWS_TRAY_TESTS not set")
	}
	var abort atomic.Bool
	h := openHideTestWindow(t, abort.Load, StartHidden(true))
	time.Sleep(time.Second)
	if h.visible() {
		t.Fatal("a StartHidden window is visible after its creation")
	}
	if fg, _, _ := testGetForeground.Call(); syscall.Handle(fg) == h.hwnd {
		t.Error("a StartHidden window took the foreground")
	}
	f0, w0 := h.frames.Load(), h.wakeups.Load()
	t.Logf("the creation drew %d frames", f0)
	for range 5 {
		h.w.Invalidate()
		time.Sleep(100 * time.Millisecond)
	}
	if n := h.frames.Load() - f0; n != 0 {
		t.Errorf("hidden from the start: %d frames after Invalidate, want none", n)
	}
	if h.wakeups.Load() == w0 {
		t.Error("hidden from the start: Invalidate delivered no event to the app")
	}
	h.w.Option(Title("Clack hide test - 3 new items"))
	time.Sleep(300 * time.Millisecond)
	if h.visible() {
		t.Fatal("a title change showed the window started hidden")
	}

	f1 := h.frames.Load()
	h.w.Perform(system.ActionRaise)
	waitUntil(t, "visible after ActionRaise", h.visible)
	waitUntil(t, "a frame after ActionRaise", func() bool { return h.frames.Load() > f1 })
	if got := testWindowText(h.hwnd); got != "Clack hide test - 3 new items" {
		t.Errorf("title %q after the show", got)
	}
	h.closeWindow(t)
	select {
	case <-h.done:
	case <-time.After(10 * time.Second):
		t.Fatal("the window didn't close")
	}
}
