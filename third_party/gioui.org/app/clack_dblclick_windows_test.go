// SPDX-License-Identifier: Unlicense OR MIT

//go:build windows

package app

import (
	"image"
	"os"
	"slices"
	"testing"
	"time"

	syscall "golang.org/x/sys/windows"

	"gioui.org/app/internal/windows"
	"gioui.org/io/system"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/widget"
)

var (
	testGetDoubleClickTime = testUser32.NewProc("GetDoubleClickTime")
	testSetDoubleClickTime = testUser32.NewProc("SetDoubleClickTime")
)

// Clack patch 48 on a real Windows desktop (CLACK_WINDOWS_DBLCLICK_TESTS,
// ci's windows-applog job and flake-check's dblclick): clicks sent through
// SendInput, Windows' own input path, count by the Mouse settings'
// double-click time, read again on every press as Chromium reads it. Two
// clicks 400 ms apart are a double click at the default 500 ms (where
// gesture.Click's old fixed 200 ms made them two), and two clicks once the
// setting is 300 ms; a press further from the first than half of
// SM_CXDOUBLECLK starts over.
func TestClackDoubleClickWindows(t *testing.T) {
	if os.Getenv("CLACK_WINDOWS_DBLCLICK_TESTS") == "" {
		t.Skip("CLACK_WINDOWS_DBLCLICK_TESTS not set")
	}
	orig, _, _ := testGetDoubleClickTime.Call()
	t.Cleanup(func() { testSetDoubleClickTime.Call(orig) })
	setTime := func(ms int) {
		t.Helper()
		if r, _, err := testSetDoubleClickTime.Call(uintptr(ms)); r == 0 {
			t.Fatalf("SetDoubleClickTime(%d): %v", ms, err)
		}
	}
	cx, cy := windows.GetSystemMetrics(windows.SM_CXDOUBLECLK), windows.GetSystemMetrics(windows.SM_CYDOUBLECLK)
	if iv, slop := windowsDoubleClick(); iv != time.Duration(orig)*time.Millisecond || slop != image.Pt(cx/2, cy/2) {
		t.Errorf("the rule reads %v and %v, want GetDoubleClickTime's %d ms and half of (%d, %d)", iv, slop, orig, cx, cy)
	}
	setTime(700)
	if iv, _ := windowsDoubleClick(); iv != 700*time.Millisecond {
		t.Errorf("after SetDoubleClickTime(700) the rule reads %v", iv)
	}
	t.Logf("GetDoubleClickTime %d ms, SM_CXDOUBLECLK %d, SM_CYDOUBLECLK %d", orig, cx, cy)

	w := new(Window)
	w.Option(Title("Clack double-click test"), Size(480, 360))
	hwnds := make(chan syscall.Handle, 1)
	clicks := make(chan int, 32)
	done := make(chan struct{})
	go func() {
		defer close(done)
		var ops op.Ops
		var btn widget.Clickable
		for {
			switch e := w.Event().(type) {
			case Win32ViewEvent:
				if e.HWND != 0 {
					select {
					case hwnds <- syscall.Handle(e.HWND):
					default:
					}
				}
			case FrameEvent:
				gtx := NewContext(&ops, e)
				for {
					cl, ok := btn.Update(gtx)
					if !ok {
						break
					}
					select {
					case clicks <- cl.NumClicks:
					default:
					}
				}
				btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{Size: gtx.Constraints.Max} })
				gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(30 * time.Millisecond)})
				e.Frame(gtx.Ops)
			case DestroyEvent:
				return
			}
		}
	}()
	t.Cleanup(func() {
		w.Perform(system.ActionClose)
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Log("the window didn't close")
		}
	})
	var hwnd syscall.Handle
	select {
	case hwnd = <-hwnds:
	case <-time.After(30 * time.Second):
		t.Fatal("no Win32ViewEvent")
	}
	time.Sleep(700 * time.Millisecond) // shown
	windows.SetWindowPos(hwnd, windows.HWND_TOPMOST, 0, 0, 0, 0, windows.SWP_NOMOVE|windows.SWP_NOSIZE)
	testSetForegroundWindow.Call(uintptr(hwnd))
	_, screen := clientCentre(hwnd)
	got := func(n int) []int {
		t.Helper()
		var out []int
		for range n {
			select {
			case c := <-clicks:
				out = append(out, c)
			case <-time.After(5 * time.Second):
				t.Fatalf("%d clicks of %d", len(out), n)
			}
		}
		return out
	}
	pair := func(gap time.Duration, dx int32) []int {
		t.Helper()
		sendInput(t, clickInput(screen.X, screen.Y))
		time.Sleep(gap)
		sendInput(t, clickInput(screen.X+dx, screen.Y))
		return got(2)
	}
	// a first click takes the foreground (it may only activate the
	// window); then nothing for a second, past any run
	sendInput(t, clickInput(screen.X, screen.Y))
	select {
	case <-clicks:
	case <-time.After(2 * time.Second):
	}
	time.Sleep(time.Second)
	for len(clicks) > 0 {
		<-clicks
	}

	setTime(500)
	if c := pair(400*time.Millisecond, 0); !slices.Equal(c, []int{1, 2}) {
		t.Errorf("400 ms apart at 500 ms: %v, want [1 2]", c)
	}
	time.Sleep(time.Second)
	setTime(300) // read again on the next press
	if c := pair(400*time.Millisecond, 0); !slices.Equal(c, []int{1, 1}) {
		t.Errorf("400 ms apart at 300 ms: %v, want [1 1]", c)
	}
	time.Sleep(time.Second)
	setTime(500)
	if c := pair(100*time.Millisecond, int32(cx/2+2)); !slices.Equal(c, []int{1, 1}) {
		t.Errorf("100 ms apart, %d px across: %v, want [1 1] (half of SM_CXDOUBLECLK %d)", cx/2+2, c, cx)
	}
}
