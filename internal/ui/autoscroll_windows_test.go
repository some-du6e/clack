//go:build windows

package ui

import (
	"encoding/binary"
	"image"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"gioui.org/app"
	"gioui.org/io/event"
	"golang.org/x/sys/windows"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

var (
	asTestUser32           = windows.NewLazySystemDLL("user32.dll")
	asTestSendInput        = asTestUser32.NewProc("SendInput")
	asTestGetSystemMetrics = asTestUser32.NewProc("GetSystemMetrics")
	asTestClientToScreen   = asTestUser32.NewProc("ClientToScreen")
	asTestGetCursorInfo    = asTestUser32.NewProc("GetCursorInfo")
	asTestLoadCursor       = asTestUser32.NewProc("LoadCursorW")
	asTestSetForeground    = asTestUser32.NewProc("SetForegroundWindow")
)

// asTestInput is user32's INPUT on 64-bit Windows: the type, then the
// 32-byte union (MOUSEINPUT is the largest).
type asTestInput struct {
	typ uint32
	_   uint32
	u   [32]byte
}

const (
	asMove, asAbs            = 0x0001, 0x8000
	asLeftDown, asLeftUp     = 0x0002, 0x0004
	asMiddleDown, asMiddleUp = 0x0020, 0x0040
)

// asMouse is a MOUSEINPUT at screen point p (absolute) with flags.
func asMouse(p image.Point, flags uint32) asTestInput {
	cx, _, _ := asTestGetSystemMetrics.Call(0) // SM_CXSCREEN
	cy, _, _ := asTestGetSystemMetrics.Call(1)
	in := asTestInput{typ: 0} // INPUT_MOUSE
	binary.LittleEndian.PutUint32(in.u[0:], uint32(int64(p.X)*65535/int64(max(cx, 1))))
	binary.LittleEndian.PutUint32(in.u[4:], uint32(int64(p.Y)*65535/int64(max(cy, 1))))
	binary.LittleEndian.PutUint32(in.u[12:], flags|asMove|asAbs)
	return in
}

// asWheel is a wheel turn of delta (WHEEL_DELTA 120 a notch, negative
// toward the user) at screen point p.
func asWheel(p image.Point, delta int32) asTestInput {
	in := asMouse(p, 0x0800) // MOUSEEVENTF_WHEEL
	binary.LittleEndian.PutUint32(in.u[8:], uint32(delta))
	return in
}

func asSend(t *testing.T, ins ...asTestInput) {
	t.Helper()
	n, _, err := asTestSendInput.Call(uintptr(len(ins)), uintptr(unsafe.Pointer(&ins[0])), unsafe.Sizeof(ins[0]))
	if int(n) != len(ins) {
		t.Fatalf("SendInput sent %d of %d: %v", n, len(ins), err)
	}
}

// TestAutoscrollWindow is middle-click autoscroll (Gio patch 14) on a
// real Windows desktop (CLACK_WINDOWS_AUTOSCROLL_TESTS=1, ci's
// windows-applog job, where it is on by default as in Chromium): App.Run's
// own window signed in to internal/fakeslack with a long channel open, and
// the mouse driven by SendInput, Windows' own input path.
//
//  1. Middle button down over the messages, the pointer moved 120 px up,
//     held: the messages scroll toward older ones while the button is
//     down, and Windows shows Chromium's north panning cursor (pan_north.cur
//     through CreateIconFromResourceEx) rather than any system cursor.
//  2. The button released after the move: it stops.
//  3. A middle click without moving: still on (toggled); the pointer moved
//     below scrolls toward newer messages with no button held; a left
//     click stops it.
//
// What it proves: WM_MBUTTONDOWN/UP reach Gio as ButtonTertiary presses
// in the right place, the drags keep coming while Gio holds the mouse
// capture, Clack's frame loop paces the scrolling with no other input
// (only the autoscroll's own InvalidateCmd asks for frames), a plain
// WM_MOUSEMOVE steers the toggled mode, and the cursor Windows shows is
// the one the patch made. What it doesn't: the speed against Chromium's
// numbers (the router tests in internal/ui/autoscroll do that on exact
// frame times), or how it looks.
func TestAutoscrollWindow(t *testing.T) {
	if os.Getenv("CLACK_WINDOWS_AUTOSCROLL_TESTS") == "" {
		t.Skip("CLACK_WINDOWS_AUTOSCROLL_TESTS not set")
	}
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	home := t.TempDir()
	cache := filepath.Join(home, "cache")
	hwnds := make(chan uintptr, 1)
	var frames atomic.Int64
	images := imgcache.New(fs.Fetch, "", nil)
	a := New(Options{ConfigDir: home, NoRealtime: true, Started: time.Now(), OnWindowEvent: func(e event.Event) {
		switch e := e.(type) {
		case app.Win32ViewEvent:
			if e.HWND != 0 {
				select {
				case hwnds <- e.HWND:
				default:
				}
			}
		case app.FrameEvent:
			frames.Add(1)
		}
	}}, images)
	a.Attach(core.New(slack.New(fs.Creds()), store.New(cache)), cache)
	done := make(chan error, 1)
	go func() { done <- a.Run() }()
	t.Cleanup(func() {
		a.Quit()
		select {
		case <-done:
		case <-time.After(30 * time.Second):
			t.Log("Run didn't return after Quit")
		}
	})
	var hwnd uintptr
	select {
	case hwnd = <-hwnds:
	case <-time.After(60 * time.Second):
		t.Fatal("no Win32ViewEvent")
	}
	// on runs f on the UI goroutine and waits for it
	on := func(f func()) {
		t.Helper()
		ran := make(chan struct{})
		a.Post(func() { f(); close(ran) })
		select {
		case <-ran:
		case err := <-done:
			t.Fatalf("Run returned (%v)", err)
		case <-time.After(20 * time.Second):
			t.Fatal("posted work didn't run")
		}
	}
	until := func(what string, ok func() bool) {
		t.Helper()
		for dl := time.Now().Add(60 * time.Second); !ok(); time.Sleep(50 * time.Millisecond) {
			if time.Now().After(dl) {
				t.Fatalf("timed out waiting for %s", what)
			}
		}
	}
	until("the first frames", func() bool { return frames.Load() > 2 })
	booted := func() (ok bool) { on(func() { ok = !a.data.booted.IsZero() }); return }
	until("core ready", booted)
	ch := fs.ChannelIDs()[2]
	on(func() { a.Navigate("c/" + ch) })
	loaded := func() (n int) {
		on(func() {
			if tl := a.data.core.S.Timeline(ch); tl != nil {
				n = len(tl.Msgs)
			}
		})
		return
	}
	until("the channel's history", func() bool { return loaded() > 30 })
	time.Sleep(2 * time.Second) // boot settles (the reveal, images)
	asTestSetForeground.Call(hwnd)
	time.Sleep(300 * time.Millisecond)

	rows := func() (m map[string]int) {
		on(func() {
			m = map[string]int{}
			if c := a.convos[ch+"/"]; c != nil {
				if l, ok := c.list.(channelList); ok {
					for _, r := range l.VisibleRows() {
						m[r.Key] = r.Y
					}
				}
			}
		})
		return
	}
	state := func() (active, toggled bool) {
		on(func() { active, toggled = a.autoscroll.Active(), a.autoscroll.Toggled() })
		return
	}
	// the messages' middle, window coordinates (perfEnv.listPoint), on
	// the screen
	var win image.Point
	on(func() {
		o := a.primaryOrigin
		win = image.Pt(o.X+(a.winSize.X-o.X)/2, o.Y+(a.winSize.Y-o.Y)/2)
	})
	pt := struct{ X, Y int32 }{int32(win.X), int32(win.Y)}
	asTestClientToScreen.Call(hwnd, uintptr(unsafe.Pointer(&pt)))
	p := image.Pt(int(pt.X), int(pt.Y))
	t.Logf("window point %v, screen %v", win, p)

	// mid-channel, so there is room both ways (it may open at the top of
	// what's loaded): the wheel to the bottom, then six notches up
	for i := 0; i < 30; i++ {
		asSend(t, asWheel(p, -120))
		time.Sleep(30 * time.Millisecond)
	}
	time.Sleep(800 * time.Millisecond)
	for i := 0; i < 6; i++ {
		asSend(t, asWheel(p, 120))
		time.Sleep(30 * time.Millisecond)
	}
	time.Sleep(time.Second)

	// start: middle down at the first point where it starts (a link
	// under the pointer rightly doesn't)
	start := func() image.Point {
		t.Helper()
		for _, q := range []image.Point{p, p.Add(image.Pt(0, 37)), p.Add(image.Pt(0, -41)), p.Add(image.Pt(200, 0))} {
			asSend(t, asMouse(q, 0))
			time.Sleep(100 * time.Millisecond)
			asSend(t, asMouse(q, asMiddleDown))
			time.Sleep(300 * time.Millisecond)
			if active, _ := state(); active {
				return q
			}
			asSend(t, asMouse(q, asMiddleUp))
			time.Sleep(200 * time.Millisecond)
		}
		t.Fatal("a real middle press over the messages started no autoscroll")
		return image.Point{}
	}

	// watch samples the rows every 40 ms for d and returns how far they
	// moved, summed sample by sample (a fast scroll leaves no row on
	// screen in both ends' snapshots)
	watch := func(d time.Duration) (moved int) {
		prev := rows()
		for dl := time.Now().Add(d); time.Now().Before(dl); {
			time.Sleep(40 * time.Millisecond)
			cur := rows()
			moved += movedBetween(prev, cur)
			prev = cur
		}
		return moved
	}

	// 1. held, 120 px up
	q := start()
	asSend(t, asMouse(q.Sub(image.Pt(0, 120)), 0))
	moved := watch(200 * time.Millisecond)
	var ci struct {
		size   uint32
		flags  uint32
		cursor uintptr
		x, y   int32
	}
	ci.size = uint32(unsafe.Sizeof(ci))
	if r, _, err := asTestGetCursorInfo.Call(uintptr(unsafe.Pointer(&ci))); r == 0 {
		t.Logf("GetCursorInfo: %v", err)
	} else if ci.flags&1 == 0 { // CURSOR_SHOWING
		t.Logf("the runner shows no cursor (flags %#x); not checking which", ci.flags)
	} else {
		var system []uintptr
		for _, id := range []uintptr{32512, 32513, 32645, 32646, 32649} { // IDC_ARROW, IBEAM, SIZENS, SIZEALL, HAND
			h, _, _ := asTestLoadCursor.Call(0, id)
			system = append(system, h)
		}
		for _, h := range system {
			if ci.cursor == h {
				t.Errorf("while scrolling up Windows shows a system cursor (%#x), not Chromium's pan_north", h)
			}
		}
		t.Logf("cursor while scrolling up: %#x (system arrow/ibeam/sizens/sizeall/hand: %#x)", ci.cursor, system)
	}
	moved += watch(400 * time.Millisecond)
	t.Logf("held 120px above for ~600ms: the messages moved %dpx", moved)
	if moved > -100 {
		t.Errorf("held 120px above: the messages moved %dpx, want well over 100px toward older ones", moved)
	}
	// 2. released after the move: stopped
	asSend(t, asMouse(q.Sub(image.Pt(0, 120)), asMiddleUp))
	time.Sleep(300 * time.Millisecond)
	if active, _ := state(); active {
		t.Fatal("still running after the middle button came up")
	}
	if m := watch(300 * time.Millisecond); m != 0 {
		t.Errorf("kept moving (%dpx) after the release", m)
	}

	// 3. a click without moving: toggled
	q = start()
	asSend(t, asMouse(q, asMiddleUp))
	time.Sleep(300 * time.Millisecond)
	if _, toggled := state(); !toggled {
		t.Fatal("a real middle click didn't leave it toggled")
	}
	asSend(t, asMouse(q.Add(image.Pt(0, 90)), 0))
	moved = watch(600 * time.Millisecond)
	t.Logf("toggled, 90px below for 600ms: the messages moved %dpx", moved)
	if moved <= 20 {
		t.Errorf("toggled, 90px below: the messages moved %dpx, want toward newer ones", moved)
	}
	asSend(t, asMouse(q.Add(image.Pt(0, 90)), asLeftDown), asMouse(q.Add(image.Pt(0, 90)), asLeftUp))
	time.Sleep(300 * time.Millisecond)
	if active, _ := state(); active {
		t.Error("a real left click didn't stop it")
	}
}
