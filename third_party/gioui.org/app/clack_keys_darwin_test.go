// SPDX-License-Identifier: Unlicense OR MIT

//go:build darwin && !ios

package app

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gioui.org/app/cocoatest"
	"gioui.org/io/key"
	"gioui.org/op"
)

// Clack patch 29 (CLACK_PATCHES.md) on a real macOS desktop
// (CLACK_MACOS_WINDOW_TESTS, ci's macos job): key presses posted as a
// keyboard makes them (cocoatest.PostKey: the modifiers' flagsChanged,
// the key with the current layout's characters, through NSApp
// sendEvent:) reach the window's key filter once each. That includes
// the ⌘ presses with ⌥ held, which AppKit's text system sends no command
// for (⌥⌘B, ⌥⇧⌘D, ⌃⌥⇧⌘D) and Gio lost before. The log has what the view
// got of each key (cocoatest.TraceKeys): which ones came through
// doCommandBySelector: and which through the patch.
func TestMacCommandKeys(t *testing.T) {
	if !macWindowTestsEnabled() {
		t.Skip("CLACK_MACOS_WINDOW_TESTS not set")
	}
	w := new(Window)
	w.Option(Title("Clack key test"), Size(480, 320))
	views := make(chan uintptr, 1)
	var frames atomic.Int64
	var mu sync.Mutex
	var presses []key.Event
	go func() {
		var ops op.Ops
		every := key.Filter{Optional: key.ModCtrl | key.ModShift | key.ModAlt | key.ModCommand | key.ModSuper}
		for {
			switch e := w.Event().(type) {
			case AppKitViewEvent:
				if e.View != 0 {
					select {
					case views <- e.View:
					default:
					}
				}
			case FrameEvent:
				ops.Reset()
				gtx := NewContext(&ops, e)
				for {
					ev, ok := gtx.Event(every)
					if !ok {
						break
					}
					if ke, ok := ev.(key.Event); ok && ke.State == key.Press {
						switch ke.Name {
						case key.NameShift, key.NameCtrl, key.NameAlt, key.NameCommand, key.NameSuper:
							continue // the modifiers' own (flagsChanged)
						}
						mu.Lock()
						presses = append(presses, ke)
						mu.Unlock()
					}
				}
				e.Frame(gtx.Ops)
				frames.Add(1)
			case DestroyEvent:
				return
			}
		}
	}()
	var view uintptr
	select {
	case view = <-views:
	case <-time.After(30 * time.Second):
		t.Fatal("no AppKitViewEvent")
	}
	macWaitUntil(t, "the first frame", func() bool { return frames.Load() > 0 })
	cocoatest.OnMain(func() {
		cocoatest.OrderFront(view)
		cocoatest.TraceKeys(view)
	})
	macWaitUntil(t, "the key window", func() bool {
		var s cocoatest.Window
		cocoatest.OnMain(func() { s = cocoatest.State(view) })
		return s.Key
	})
	var layout string
	cocoatest.OnMain(func() { layout = cocoatest.KeyboardLayout() })
	t.Logf("keyboard layout %s", layout)

	const cmd, opt, shift, ctrl = cocoatest.Cmd, cocoatest.Opt, cocoatest.Shift, cocoatest.Ctrl
	for _, c := range []struct {
		name string
		code int
		mods int
		want key.Event
	}{
		{"⌘B", cocoatest.KeyB, cmd, key.Event{Name: "B", Modifiers: key.ModCommand}},
		{"⇧⌘D", cocoatest.KeyD, cmd | shift, key.Event{Name: "D", Modifiers: key.ModCommand | key.ModShift}},
		// the composer's inline code and strike (⌘C and ⌘X are the Edit
		// menu's Copy and Cut key equivalents; shifted, they're not)
		{"⇧⌘C", cocoatest.KeyC, cmd | shift, key.Event{Name: "C", Modifiers: key.ModCommand | key.ModShift}},
		{"⇧⌘X", cocoatest.KeyX, cmd | shift, key.Event{Name: "X", Modifiers: key.ModCommand | key.ModShift}},
		{"⌥⇧⌘D", cocoatest.KeyD, cmd | opt | shift, key.Event{Name: "D", Modifiers: key.ModCommand | key.ModAlt | key.ModShift}},
		{"⌃⌥⇧⌘D (Hyper+D)", cocoatest.KeyD, cmd | opt | shift | ctrl, key.Event{Name: "D", Modifiers: key.ModCommand | key.ModAlt | key.ModShift | key.ModCtrl}},
		{"⌥⌘B", cocoatest.KeyB, cmd | opt, key.Event{Name: "B", Modifiers: key.ModCommand | key.ModAlt}},
		{"⌘↑", cocoatest.KeyUp, cmd, key.Event{Name: key.NameUpArrow, Modifiers: key.ModCommand}},
		{"↑", cocoatest.KeyUp, 0, key.Event{Name: key.NameUpArrow}},
		{"⌥⇧D", cocoatest.KeyD, opt | shift, key.Event{Name: "D", Modifiers: key.ModAlt | key.ModShift}},
		{"A", cocoatest.KeyA, 0, key.Event{Name: "A"}},
	} {
		mu.Lock()
		presses = nil
		mu.Unlock()
		var chars, ign string
		cocoatest.OnMain(func() { chars, ign = cocoatest.PostKey(view, c.code, c.mods) })
		want := c.want
		want.State = key.Press
		deadline := time.Now().Add(5 * time.Second)
		count := func() (n int, got []key.Event) {
			mu.Lock()
			defer mu.Unlock()
			for _, p := range presses {
				if p == want {
					n++
				}
			}
			return n, append([]key.Event(nil), presses...)
		}
		for n, _ := count(); n == 0 && time.Now().Before(deadline); n, _ = count() {
			time.Sleep(10 * time.Millisecond)
		}
		time.Sleep(200 * time.Millisecond) // a second delivery would come by now
		n, got := count()
		var trace string
		cocoatest.OnMain(func() { trace = cocoatest.KeyTrace() })
		t.Logf("%s (characters %+q, ignoring modifiers %+q): Gio got %v; the view got:\n%s", c.name, chars, ign, got, trace)
		if n != 1 {
			t.Errorf("%s: %d presses of %v, want 1 (got %v)", c.name, n, want, got)
		}
	}
}
