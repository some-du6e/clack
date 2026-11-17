// SPDX-License-Identifier: Unlicense OR MIT

//go:build linux && !android && !nox11

package app

import (
	stdcontext "context"
	"image"
	"image/color"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/io/system"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
)

// Clack patch 35 (CLACK_PATCHES.md §35) on a real X server: ci's
// linux-gio job runs it under Xvfb with CLACK_X11_ASKED_TESTS set. A Gio
// window's client draws its frames as Clack's does, and asks for a later
// wake-up again in a frame that comes before it (ui's quiet frames do).
// What it proves on the X11 driver, where Window.Invalidate comes back to
// the window from its event loop: a frame only Invalidate brought has
// Asked false (Clack's animations repaint that way, and such a frame may
// show the last frame's ops again); a frame that executes
// op.InvalidateCmd{} has the next asked for at once; one that executes
// op.InvalidateCmd{At: t} has every frame before t say so, and the frame
// at t asked for; a real pointer press a handler takes (XTEST, from
// python-xlib) brings a frame asked for, with the press in it.
func TestFrameEventAskedX11(t *testing.T) {
	if os.Getenv("CLACK_X11_ASKED_TESTS") == "" || os.Getenv("DISPLAY") == "" {
		t.Skip("CLACK_X11_ASKED_TESTS or DISPLAY not set")
	}
	os.Unsetenv("WAYLAND_DISPLAY") // X11, not Wayland
	w, win, frames, actions := askedX11Window(t)
	// next is the next frame the client drew (within d)
	next := func(what string, d time.Duration) askedSeen {
		t.Helper()
		select {
		case f := <-frames:
			return f
		case <-time.After(d):
			t.Fatalf("%s: no frame in %v", what, d)
			return askedSeen{}
		}
	}
	// quiet waits until no frame came for 500 ms (the window's start:
	// Expose, configure), failing if frames never stop
	quiet := func() {
		t.Helper()
		for dl := time.Now().Add(20 * time.Second); ; {
			select {
			case <-frames:
				if time.Now().After(dl) {
					t.Fatal("the window keeps drawing frames with nothing asking")
				}
			case <-time.After(500 * time.Millisecond):
				return
			}
		}
	}
	quiet()

	t.Run("invalidate", func(t *testing.T) {
		for i := 0; i < 5; i++ {
			w.Invalidate() // from another goroutine, as Clack's invalidator calls it
			if f := next("Invalidate", 10*time.Second); f.asked {
				t.Errorf("Invalidate %d: the frame it brought says the window asked (at %v)", i, f.askedAt.Sub(f.now))
			}
			quiet()
		}
	})

	t.Run("invalidate cmd", func(t *testing.T) {
		actions <- func(gtx layout.Context) { gtx.Execute(op.InvalidateCmd{}) }
		w.Invalidate()
		next("the frame that asks", 10*time.Second)
		if f := next("the frame asked for", 10*time.Second); !f.asked || f.askedAt.After(f.now) {
			t.Errorf("after op.InvalidateCmd{}: Asked %v at %v from the frame's time, want asked at once", f.asked, f.askedAt.Sub(f.now))
		}
		quiet()
	})

	t.Run("invalidate cmd later", func(t *testing.T) {
		at := time.Now().Add(time.Second)
		actions <- func(gtx layout.Context) { gtx.Execute(op.InvalidateCmd{At: at}) }
		w.Invalidate()
		next("the frame that asks", 10*time.Second)
		time.Sleep(200 * time.Millisecond)
		w.Invalidate() // a frame before at: an animation's
		for {
			f := next("the frame at the wake-up", 10*time.Second)
			if !f.asked || !f.askedAt.Equal(at) {
				t.Fatalf("a frame %v before or after the wake-up: Asked %v at %v, want asked at the wake-up", f.now.Sub(at), f.asked, f.askedAt.Sub(at))
			}
			if !f.now.Before(at) {
				break // the wake-up's own frame
			}
		}
		quiet()
	})

	t.Run("press", func(t *testing.T) {
		out := runXTestClick(t, win, 40, 30)
		t.Logf("xtest: %s", out)
		for dl := time.Now().Add(10 * time.Second); ; {
			f := next("the press's frame", time.Until(dl)+time.Millisecond)
			if f.pressed {
				if !f.asked || f.askedAt.After(f.now) {
					t.Errorf("the frame with the press in it: Asked %v at %v, want asked at once", f.asked, f.askedAt.Sub(f.now))
				}
				break
			}
		}
	})
}

// askedSeen is what the client saw in one frame.
type askedSeen struct {
	asked        bool
	askedAt, now time.Time
	pressed      bool // a press reached the window's handler in it
}

// askedX11Window opens a Gio window whose client lays out a frame as
// Clack's does (a handler over the window taking presses, a paint), runs
// the next action sent on actions inside a frame, and asks again for a
// later wake-up a frame comes before (FrameEvent.AskedAt after its Now).
func askedX11Window(t *testing.T) (*Window, uintptr, <-chan askedSeen, chan<- func(layout.Context)) {
	t.Helper()
	w := new(Window)
	w.Option(Title("Clack asked test"), Size(320, 240))
	views := make(chan uintptr, 1)
	frames := make(chan askedSeen, 64)
	actions := make(chan func(layout.Context), 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		var ops op.Ops
		tag := new(int)
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
				f := askedSeen{asked: e.Asked, askedAt: e.AskedAt, now: e.Now}
				gtx := NewContext(&ops, e)
				for {
					ev, ok := gtx.Event(pointer.Filter{Target: tag, Kinds: pointer.Press})
					if !ok {
						break
					}
					if p, ok := ev.(pointer.Event); ok && p.Kind == pointer.Press {
						f.pressed = true
					}
				}
				cl := clip.Rect(image.Rectangle{Max: e.Size}).Push(gtx.Ops)
				event.Op(gtx.Ops, tag)
				paint.ColorOp{Color: color.NRGBA{R: 0x40, G: 0x80, B: 0xc0, A: 0xff}}.Add(gtx.Ops)
				paint.PaintOp{}.Add(gtx.Ops)
				cl.Pop()
				select {
				case act := <-actions:
					act(gtx)
				default:
				}
				if e.Asked && e.AskedAt.After(e.Now) {
					gtx.Execute(op.InvalidateCmd{At: e.AskedAt})
				}
				e.Frame(gtx.Ops)
				select {
				case frames <- f:
				default:
				}
			case DestroyEvent:
				if e.Err != nil {
					t.Logf("window: %v", e.Err)
				}
				return
			}
		}
	}()
	t.Cleanup(func() {
		w.Perform(system.ActionClose)
		// the close is an XSendEvent from this goroutine: wake the window's
		// loop (asleep in poll) so its XPending flushes it, or every run
		// waits out the timeout and leaks the window
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
	select {
	case <-frames:
	case <-time.After(30 * time.Second):
		t.Fatal("no first frame")
	}
	return w, win, frames, actions
}

// xtestClickPy moves the pointer to (x, y) in window win and clicks
// button 1 there, through the XTEST extension (real input to the X
// server, as a mouse would send).
const xtestClickPy = `import sys, time
from Xlib import X, display
from Xlib.ext import xtest
d = display.Display()
root = d.screen().root
win = d.create_resource_object("window", int(sys.argv[1]))
r = root.translate_coords(win, int(sys.argv[2]), int(sys.argv[3]))
xtest.fake_input(d, X.MotionNotify, x=r.x, y=r.y)
d.sync()
time.sleep(0.3)
xtest.fake_input(d, X.ButtonPress, 1)
d.sync()
time.sleep(0.05)
xtest.fake_input(d, X.ButtonRelease, 1)
d.sync()
print("clicked at root", r.x, r.y)
`

func runXTestClick(t *testing.T, win uintptr, x, y int) string {
	t.Helper()
	py := filepath.Join(t.TempDir(), "xtest_click.py")
	if err := os.WriteFile(py, []byte(xtestClickPy), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, stop := stdcontext.WithTimeout(stdcontext.Background(), time.Minute)
	defer stop()
	out, err := exec.CommandContext(ctx, "python3", "-I", py, strconv.FormatUint(uint64(win), 10), strconv.Itoa(x), strconv.Itoa(y)).CombinedOutput()
	if err != nil {
		t.Fatalf("python3 xtest click: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}
