// SPDX-License-Identifier: Unlicense OR MIT

//go:build windows

package app

import (
	"encoding/binary"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	syscall "golang.org/x/sys/windows"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/system"
	"gioui.org/op"
)

// Not a patch: what Clack's Threads list (internal/ui/views threads.go,
// gap-w26) relies on, on a real Windows desktop. A key typed while a tag
// that isn't an editor holds the key focus arrives there as the key.Event
// (WM_KEYDOWN) and then the text it typed as a key.EditEvent (WM_CHAR
// after TranslateMessage), so the list can hand "!" for Shift+1 to the
// reply box it opens. The keys go through SendInput, Windows' own input
// path, which moves the real focus and keyboard state, so it runs only
// where CLACK_WINDOWS_KEY_TESTS is set (ci's windows-applog job).

var (
	testSendInput           = testUser32.NewProc("SendInput")
	testMapVirtualKey       = testUser32.NewProc("MapVirtualKeyW")
	testGetForegroundWindow = testUser32.NewProc("GetForegroundWindow")
	testSetForegroundWindow = testUser32.NewProc("SetForegroundWindow")
	testGetSystemMetrics    = testUser32.NewProc("GetSystemMetrics")
	testGetKeyboardLayout   = testUser32.NewProc("GetKeyboardLayout")
)

// testInput is user32's INPUT on 64-bit Windows: the type, then the
// 32-byte union (MOUSEINPUT is the largest).
type testInput struct {
	typ uint32
	_   uint32
	u   [32]byte
}

func keyInput(vk uint16, up bool) testInput {
	in := testInput{typ: 1} // INPUT_KEYBOARD
	scan, _, _ := testMapVirtualKey.Call(uintptr(vk), 0)
	var flags uint32
	if up {
		flags |= 0x2 // KEYEVENTF_KEYUP
	}
	binary.LittleEndian.PutUint16(in.u[0:], vk)
	binary.LittleEndian.PutUint16(in.u[2:], uint16(scan))
	binary.LittleEndian.PutUint32(in.u[4:], flags)
	return in
}

// clickInput is a left click at screen point (x, y), absolute.
func clickInput(x, y int32) []testInput {
	cx, _, _ := testGetSystemMetrics.Call(0) // SM_CXSCREEN
	cy, _, _ := testGetSystemMetrics.Call(1)
	mouse := func(flags uint32) testInput {
		in := testInput{typ: 0} // INPUT_MOUSE
		binary.LittleEndian.PutUint32(in.u[0:], uint32(int64(x)*65535/int64(max(cx, 1))))
		binary.LittleEndian.PutUint32(in.u[4:], uint32(int64(y)*65535/int64(max(cy, 1))))
		binary.LittleEndian.PutUint32(in.u[12:], flags)
		return in
	}
	const move, abs, down, up = 0x1, 0x8000, 0x2, 0x4
	return []testInput{mouse(move | abs), mouse(down | abs), mouse(up | abs)}
}

func sendInput(t *testing.T, ins []testInput) {
	t.Helper()
	n, _, err := testSendInput.Call(uintptr(len(ins)), uintptr(unsafe.Pointer(&ins[0])), unsafe.Sizeof(ins[0]))
	if int(n) != len(ins) {
		t.Fatalf("SendInput sent %d of %d: %v", n, len(ins), err)
	}
}

func TestWinKeyTextToFocusedTag(t *testing.T) {
	if os.Getenv("CLACK_WINDOWS_KEY_TESTS") == "" {
		t.Skip("CLACK_WINDOWS_KEY_TESTS not set")
	}
	w := new(Window)
	w.Option(Title("Clack key text test"), Size(480, 360))
	hwnds := make(chan syscall.Handle, 1)
	evs := make(chan string, 256)
	done := make(chan struct{})
	var focusReq atomic.Bool
	tag := new(int)
	go func() {
		defer close(done)
		var ops op.Ops
		frame := 0
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
				frame++
				if focusReq.CompareAndSwap(true, false) {
					gtx.Execute(key.FocusCmd{Tag: tag})
				}
				// the keys a list like Threads' reads (typeFilters)
				for {
					ev, ok := gtx.Event(key.FocusFilter{Target: tag},
						key.Filter{Focus: tag, Name: "1", Optional: key.ModShift},
						key.Filter{Focus: tag, Name: "A", Optional: key.ModShift})
					if !ok {
						break
					}
					switch ev := ev.(type) {
					case key.FocusEvent:
						evs <- fmt.Sprintf("focus %v", ev.Focus)
					case key.Event:
						if ev.State == key.Press {
							evs <- fmt.Sprintf("key %s shift=%v", ev.Name, ev.Modifiers.Contain(key.ModShift))
						}
					case key.EditEvent:
						evs <- fmt.Sprintf("edit %q", ev.Text)
					}
				}
				event.Op(gtx.Ops, tag)
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
	// the window takes the foreground the way a user gives it: a click
	_, screen := clientCentre(hwnd)
	sendInput(t, clickInput(screen.X, screen.Y))
	time.Sleep(300 * time.Millisecond)
	if fg, _, _ := testGetForegroundWindow.Call(); syscall.Handle(fg) != hwnd {
		testSetForegroundWindow.Call(uintptr(hwnd))
		time.Sleep(300 * time.Millisecond)
	}
	if fg, _, _ := testGetForegroundWindow.Call(); syscall.Handle(fg) != hwnd {
		t.Fatalf("the test window isn't in the foreground (%#x, want %#x): SendInput would type elsewhere", fg, hwnd)
	}
	hkl, _, _ := testGetKeyboardLayout.Call(0)
	t.Logf("keyboard layout %#x", hkl)
	focusReq.Store(true)
	wait := func(want string) []string {
		t.Helper()
		var got []string
		dl := time.After(15 * time.Second)
		for {
			select {
			case s := <-evs:
				got = append(got, s)
				if s == want {
					return got
				}
			case <-dl:
				t.Fatalf("no %q; events %q", want, got)
			}
		}
	}
	wait("focus true")
	// Shift+1, as a user types it
	const vkShift, vk1, vkA = 0x10, 0x31, 0x41
	sendInput(t, []testInput{keyInput(vkShift, false), keyInput(vk1, false), keyInput(vk1, true), keyInput(vkShift, true)})
	got := wait(`edit "!"`)
	if strings.Join(got, "|") != `key 1 shift=true|edit "!"` {
		t.Errorf("Shift+1 reached the focused tag as %q, want the key, then its text", got)
	}
	sendInput(t, []testInput{keyInput(vkA, false), keyInput(vkA, true)})
	got = wait(`edit "a"`)
	if strings.Join(got, "|") != `key A shift=false|edit "a"` {
		t.Errorf("A reached the focused tag as %q, want the key, then its text", got)
	}
}
