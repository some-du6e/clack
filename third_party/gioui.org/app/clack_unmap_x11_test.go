// SPDX-License-Identifier: Unlicense OR MIT

//go:build linux && !android && !nox11

package app

import (
	stdcontext "context"
	"os"
	"os/exec"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"gioui.org/gpu"
	"gioui.org/io/system"
	"gioui.org/op"
)

// Clack patch (CLACK_PATCHES.md §46) on a real X server (ci's linux-gio job
// runs it under Xvfb with CLACK_X11_DROP_TESTS set, beside the XDND test):
// a window that animates (each frame asks for the next, as a spinner or an
// animated emoji does) is unmapped by another client (python-xlib's
// XUnmapWindow, as a window manager iconifies it). Its frames stop and its
// frame texture goes; mapped again, it draws again.
func TestUnmappedStopsFramesX11(t *testing.T) {
	if os.Getenv("CLACK_X11_DROP_TESTS") == "" || os.Getenv("DISPLAY") == "" {
		t.Skip("CLACK_X11_DROP_TESTS or DISPLAY not set")
	}
	os.Unsetenv("WAYLAND_DISPLAY") // X11, not Wayland
	held0 := gpu.ClackFrameTexturesHeld()
	w := new(Window)
	w.Option(Title("Clack unmap test"), Size(320, 240))
	views := make(chan uintptr, 1)
	var frames atomic.Int64
	done := make(chan struct{})
	go func() {
		defer close(done)
		var ops op.Ops
		for {
			switch e := w.Event().(type) {
			case X11ViewEvent:
				if e.Window != 0 {
					select {
					case views <- e.Window:
					default:
					}
				}
			case FrameEvent:
				gtx := NewContext(&ops, e)
				gtx.Execute(op.InvalidateCmd{})
				e.Frame(gtx.Ops)
				frames.Add(1)
			case DestroyEvent:
				return
			}
		}
	}()
	t.Cleanup(func() {
		w.Perform(system.ActionClose)
		w.Invalidate()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Log("the window didn't close within 10 s")
		}
	})
	var win uintptr
	select {
	case win = <-views:
	case <-time.After(30 * time.Second):
		t.Fatal("no X11ViewEvent (is this X11?)")
	}
	until := func(what string, ok func() bool) {
		t.Helper()
		for dl := time.Now().Add(15 * time.Second); !ok(); time.Sleep(20 * time.Millisecond) {
			if time.Now().After(dl) {
				t.Fatalf("timed out waiting for %s", what)
			}
		}
	}
	xmap := func(how string) {
		t.Helper()
		ctx, stop := stdcontext.WithTimeout(stdcontext.Background(), 30*time.Second)
		defer stop()
		out, err := exec.CommandContext(ctx, "python3", "-I", "-c", `
import sys
from Xlib import display
d = display.Display()
w = d.create_resource_object("window", int(sys.argv[1]))
w.unmap() if sys.argv[2] == "unmap" else w.map()
d.sync()
`, strconv.FormatUint(uint64(win), 10), how).CombinedOutput()
		if err != nil {
			t.Fatalf("python3 %s: %v\n%s", how, err, out)
		}
	}
	until("frames", func() bool { return frames.Load() > 10 })
	if n := gpu.ClackFrameTexturesHeld(); n != held0+1 {
		t.Logf("animating: %d frame textures held, %d before the window (its GPU may not draw partial frames)", n, held0)
	}
	held := gpu.ClackFrameTexturesHeld()

	xmap("unmap")
	time.Sleep(500 * time.Millisecond) // the UnmapNotify, any frame in flight
	f0 := frames.Load()
	time.Sleep(time.Second)
	if n := frames.Load() - f0; n > 0 {
		t.Errorf("unmapped: %d frames in a second, want none", n)
	}
	if n := gpu.ClackFrameTexturesHeld(); n != held0 {
		t.Errorf("unmapped: %d frame textures held (%d while it drew, %d before the window), want the window's let go", n, held, held0)
	}

	xmap("map")
	until("a frame after the map", func() bool { return frames.Load() > f0+3 })
	t.Logf("animating, unmapped and mapped again: %d frames, %d frame textures held (%d before)", frames.Load(), gpu.ClackFrameTexturesHeld(), held0)
}
