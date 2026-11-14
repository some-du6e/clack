// SPDX-License-Identifier: Unlicense OR MIT

//go:build windows

package app

import (
	"image"
	"os"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	syscall "golang.org/x/sys/windows"

	"gioui.org/app/internal/windows"
	"gioui.org/io/system"
	"gioui.org/op"
)

// Clack patch (CLACK_PATCHES.md §25) on a real Windows desktop
// (CLACK_WINDOWS_BOUNDS_TESTS, ci's windows-applog job).

func boundsTestsEnabled() bool { return os.Getenv("CLACK_WINDOWS_BOUNDS_TESTS") != "" }

var testSystemParametersInfo = testUser32.NewProc("SystemParametersInfoW")

type boundsTestWindow struct {
	w      *Window
	hwnd   syscall.Handle
	frames atomic.Int64
	cfg    atomic.Pointer[Config]
	done   chan struct{}
}

// openBoundsTestWindow opens a Gio window with opts after a title, a size
// and a minimum, recording its ConfigEvents; it closes when the test ends.
func openBoundsTestWindow(t *testing.T, opts ...Option) *boundsTestWindow {
	t.Helper()
	h := &boundsTestWindow{w: new(Window), done: make(chan struct{})}
	h.w.Option(append([]Option{Title("Clack bounds test"), Size(480, 360), MinSize(300, 200)}, opts...)...)
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
			case ConfigEvent:
				c := e.Config
				h.cfg.Store(&c)
			case DestroyEvent:
				return
			}
		}
	}()
	select {
	case h.hwnd = <-hwnds:
	case <-time.After(30 * time.Second):
		t.Fatal("no Win32ViewEvent")
	}
	t.Cleanup(func() { h.close(t) })
	return h
}

func (h *boundsTestWindow) config() Config {
	if c := h.cfg.Load(); c != nil {
		return *c
	}
	return Config{}
}

func (h *boundsTestWindow) rect() image.Rectangle {
	return winRectImage(windows.GetWindowRect(h.hwnd))
}

func (h *boundsTestWindow) zoomed() bool {
	r, _, _ := testIsZoomed.Call(uintptr(h.hwnd))
	return r != 0
}

func (h *boundsTestWindow) close(t *testing.T) {
	select {
	case <-h.done:
		return
	default:
	}
	windows.PostMessage(h.hwnd, windows.WM_CLOSE, 0, 0)
	select {
	case <-h.done:
	case <-time.After(10 * time.Second):
		t.Error("the window didn't close")
	}
}

// Displays is the runner's monitor as Windows reports it elsewhere: one
// display, primary, its bounds the screen's (SM_CXSCREEN, SM_CYSCREEN at
// the origin), its work area SPI_GETWORKAREA's, its scale the system DPI
// over 96.
func TestDisplaysWindows(t *testing.T) {
	if !boundsTestsEnabled() {
		t.Skip("CLACK_WINDOWS_BOUNDS_TESTS not set")
	}
	ds := Displays()
	t.Logf("displays: %+v", ds)
	var prim *Display
	for i := range ds {
		if ds[i].Primary {
			if prim != nil {
				t.Fatal("two primary displays")
			}
			prim = &ds[i]
		}
	}
	if prim == nil {
		t.Fatal("no primary display")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	restore := windows.PerMonitorDPIAware()
	defer restore()
	if want := image.Rect(0, 0, windows.GetSystemMetrics(0), windows.GetSystemMetrics(1)); prim.Bounds != want {
		t.Errorf("the primary's bounds %v, want the screen's %v", prim.Bounds, want)
	}
	var wa windows.Rect
	const spiGetWorkArea = 0x0030
	if r, _, err := testSystemParametersInfo.Call(spiGetWorkArea, 0, uintptr(unsafe.Pointer(&wa)), 0); r == 0 {
		t.Fatalf("SPI_GETWORKAREA: %v", err)
	}
	if want := winRectImage(wa); prim.WorkArea != want {
		t.Errorf("the primary's work area %v, want %v", prim.WorkArea, want)
	}
	if want := float32(windows.GetSystemDPI()) / 96; prim.Scale != want {
		t.Errorf("the primary's scale %v, want %v", prim.Scale, want)
	}
}

// Bounds on Windows: the window is created at the frame asked for, which
// its ConfigEvents report with its monitor; a move, a minimize (no bounds
// then) and a restore bring ConfigEvents; a later Bounds moves a windowed
// window and restores a maximized one onto the frame. Opened maximized it
// restores to the frame; opened full screen it covers the monitor; opened
// hidden and maximized (StartHidden) it has no bounds until ActionRaise
// shows it, maximized.
func TestBoundsWindows(t *testing.T) {
	if !boundsTestsEnabled() {
		t.Skip("CLACK_WINDOWS_BOUNDS_TESTS not set")
	}
	var prim Display
	for _, d := range Displays() {
		if d.Primary {
			prim = d
		}
	}
	wa := prim.WorkArea
	r := image.Rect(wa.Min.X+60, wa.Min.Y+40, wa.Min.X+60+520, wa.Min.Y+40+380)

	// 1. created at r
	h := openBoundsTestWindow(t, Bounds(r))
	waitUntil(t, "a frame", func() bool { return h.frames.Load() > 0 })
	if got := h.rect(); got != r {
		t.Errorf("created at %v, want %v", got, r)
	}
	waitUntil(t, "the ConfigEvent's bounds", func() bool { return h.config().Bounds() == r })
	if d := h.config().Display(); d != prim {
		t.Errorf("the window's display %+v, want the primary %+v", d, prim)
	}

	// 2. a move by someone else (SetWindowPos from this thread)
	moved := r.Add(image.Pt(37, 23))
	windows.SetWindowPos(h.hwnd, 0, int32(moved.Min.X), int32(moved.Min.Y), int32(moved.Dx()), int32(moved.Dy()), windows.SWP_NOZORDER|windows.SWP_NOACTIVATE)
	waitUntil(t, "the bounds after a move", func() bool { return h.config().Bounds() == moved })

	// 3. minimized: no bounds; restored: back
	h.w.Option(Minimized.Option())
	waitUntil(t, "no bounds while minimized", func() bool { return windows.IsIconic(h.hwnd) && h.config().Bounds().Empty() })
	h.w.Perform(system.ActionRaise)
	waitUntil(t, "the bounds after the restore", func() bool { return !windows.IsIconic(h.hwnd) && h.config().Bounds() == moved })

	// 4. a later Bounds on the windowed window
	r4 := image.Rect(wa.Min.X+90, wa.Min.Y+70, wa.Min.X+90+600, wa.Min.Y+70+420)
	h.w.Option(Bounds(r4))
	waitUntil(t, "the frame Bounds gave", func() bool { return h.rect() == r4 && h.config().Bounds() == r4 })

	// 5. maximized, then Bounds: restored onto the frame
	h.w.Option(Maximized.Option())
	waitUntil(t, "maximized", func() bool { return h.zoomed() && h.config().Mode == Maximized })
	if b := h.config().Bounds(); !wa.In(b) {
		t.Errorf("maximized: bounds %v, want the work area %v (and the frame around it)", b, wa)
	}
	h.w.Option(Bounds(r))
	waitUntil(t, "restored onto r by Bounds", func() bool { return !h.zoomed() && h.rect() == r && h.config().Mode == Windowed })
	h.close(t)

	// 6. created maximized: restores to r
	h = openBoundsTestWindow(t, Bounds(r), Maximized.Option())
	waitUntil(t, "created maximized", func() bool { return h.zoomed() && h.config().Mode == Maximized })
	windows.ShowWindow(h.hwnd, windows.SW_RESTORE)
	waitUntil(t, "restored to r", func() bool { return !h.zoomed() && h.rect() == r })
	h.close(t)

	// 7. created full screen: on the monitor r is on
	h = openBoundsTestWindow(t, Bounds(r), Fullscreen.Option())
	waitUntil(t, "created full screen", func() bool { return h.config().Mode == Fullscreen })
	t.Logf("full screen: bounds %v on %+v", h.config().Bounds(), h.config().Display())
	if b := h.config().Bounds(); !wa.In(b) || h.config().Display() != prim {
		t.Errorf("full screen: bounds %v on %+v, want over the primary's work area %v", b, h.config().Display(), wa)
	}
	h.close(t)

	// 8. created hidden and maximized: no bounds until shown, maximized
	h = openBoundsTestWindow(t, Bounds(r), Maximized.Option(), StartHidden(true))
	time.Sleep(time.Second)
	if windows.IsWindowVisible(h.hwnd) || !h.config().Bounds().Empty() {
		t.Fatalf("started hidden: visible %v, bounds %v", windows.IsWindowVisible(h.hwnd), h.config().Bounds())
	}
	h.w.Perform(system.ActionRaise)
	waitUntil(t, "shown maximized by ActionRaise", func() bool {
		return windows.IsWindowVisible(h.hwnd) && h.zoomed() && h.config().Mode == Maximized && !h.config().Bounds().Empty()
	})
	windows.ShowWindow(h.hwnd, windows.SW_RESTORE)
	waitUntil(t, "restored to r", func() bool { return !h.zoomed() && h.rect() == r })
}
