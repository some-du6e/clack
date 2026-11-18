//go:build darwin && cgo

package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gioui.org/app/cocoatest"
)

// TestMacInspectKeys is the inspect tool's keys on App.Run's own window
// on a real macOS desktop (CLACK_MACOS_WINDOW_TESTS=1, ci's macos job;
// TestMain runs app.Main), signed in to internal/fakeslack, with its menu
// bar. The keys are posted as a keyboard makes them (cocoatest.PostKey:
// the modifiers' flagsChanged, the key's down and up with the current
// layout's characters) and AppKit delivers them: NSApp sendEvent:, the
// menu bar's key equivalents, Gio's view and its interpretKeyEvents:.
//
//  1. With no CLACK_INSPECT and no pref, Hyper+D (⌃⌥⇧⌘D) does nothing.
//  2. The Konami code, ↑ ↑ ↓ ↓ ← → ← → B A, turns the tool on at once and
//     writes inspect_tool to ui.json.
//  3. Hyper+D starts a session and again ends it; Esc ends one too.
//  4. In a session F freezes the window on its frame (inspect/freeze.go)
//     and F again unfreezes it; Esc unfreezes too, the session on, and
//     Esc again ends it. Frozen, App.Run draws the frame on screen again
//     through Metal with nothing laid out.
//  5. ⌘⌥⇧D, the old shortcut, starts none and leaves the sidebar (the
//     menu's Hide Sidebar is ⌘⇧D).
//  6. The code again turns the tool off, in ui.json too.
//
// Before Gio patch 29 no ⌘ press with ⌥ held reached the window: the
// view got ⌥⇧⌘D's and ⌃⌥⇧⌘D's keyDown:, its interpretKeyEvents: called
// neither doCommandBySelector: nor insertText:, and Gio dropped the press
// (ci run 43661396830). The log has what the view got of each key
// (cocoatest.TraceKeys).
func TestMacInspectKeys(t *testing.T) {
	if !macWindowTests {
		t.Skip("CLACK_MACOS_WINDOW_TESTS not set")
	}
	t.Setenv("CLACK_INSPECT", "")
	t.Setenv("YOLOSLACK_INSPECT", "")
	ma := runMacBoundsApp(t, nil)
	defer ma.quit(t, true)
	var layout string
	onMain(func() {
		cocoatest.OrderFront(ma.view)
		cocoatest.TraceKeys(ma.view)
		layout = cocoatest.KeyboardLayout()
	})
	t.Logf("keyboard layout %s", layout)
	ma.until(t, "the key window", func() bool { return ma.state().Key })

	tool := func() (on, active bool) {
		ma.onUI(t, func() { on, active = ma.a.insp != nil, ma.a.insp != nil && ma.a.insp.Active() })
		return on, active
	}
	frozen := func() (f bool) {
		ma.onUI(t, func() { f = ma.a.insp != nil && ma.a.insp.Frozen() })
		return f
	}
	laidOut := func() (n uint64) {
		ma.onUI(t, func() { n = ma.a.frameSeq.Load() })
		return n
	}
	// draft is the open conversation's composer text, ok false without one
	draft := func() (s string, ok bool) {
		ma.onUI(t, func() {
			if c := ma.a.convoFor(ma.a.router.Current().Channel(), ""); c != nil {
				if cb, isBox := c.comp.(composerBox); isBox {
					s, ok = cb.DocText(), true
				}
			}
		})
		return s, ok
	}
	sidebarHidden := func() (h bool) {
		ma.onUI(t, func() { h = ma.a.sidebar.hidden })
		return h
	}
	saved := func() bool {
		b, err := os.ReadFile(filepath.Join(ma.dir, "ui.json"))
		if err != nil {
			return false
		}
		var p prefs
		return json.Unmarshal(b, &p) == nil && p.InspectTool
	}
	trace := func() (s string) {
		onMain(func() { s = cocoatest.KeyTrace() })
		return s
	}
	press := func(name string, code, mods int) {
		t.Helper()
		var c, ign string
		onMain(func() { c, ign = cocoatest.PostKey(ma.view, code, mods) })
		time.Sleep(150 * time.Millisecond) // the run loop delivers them in order
		t.Logf("%s (characters %+q, ignoring modifiers %+q); the view got:\n%s", name, c, ign, trace())
	}
	// eventually waits up to 10 s for ok, which reads the tool.
	eventually := func(what string, ok func() bool) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for !ok() {
			if time.Now().After(deadline) {
				on, active := tool()
				t.Fatalf("%s: tool %v, session %v, ui.json inspect_tool %v", what, on, active, saved())
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	const cmd, opt, shift, ctrl = cocoatest.Cmd, cocoatest.Opt, cocoatest.Shift, cocoatest.Ctrl
	hyper := func() { press("⌃⌥⇧⌘D", cocoatest.KeyD, cmd|opt|shift|ctrl) }
	konami := func() {
		for _, k := range []struct {
			name string
			code int
		}{
			{"↑", cocoatest.KeyUp}, {"↑", cocoatest.KeyUp}, {"↓", cocoatest.KeyDown}, {"↓", cocoatest.KeyDown},
			{"←", cocoatest.KeyLeft}, {"→", cocoatest.KeyRight}, {"←", cocoatest.KeyLeft}, {"→", cocoatest.KeyRight},
			{"B", cocoatest.KeyB}, {"A", cocoatest.KeyA},
		} {
			press(k.name, k.code, 0)
		}
	}

	// 1. off: Hyper+D does nothing
	hyper()
	time.Sleep(time.Second)
	if on, _ := tool(); on {
		t.Fatal("the tool exists with no CLACK_INSPECT, no pref and no code")
	}

	// 2. the Konami code turns it on, now and in ui.json
	konami()
	eventually("the tool on after the Konami code", func() bool { on, _ := tool(); return on })
	eventually("inspect_tool in ui.json", saved)

	// 3. Hyper+D starts a session, again ends it; Esc ends one
	hyper()
	eventually("a session after Hyper+D", func() bool { _, a := tool(); return a })
	hyper()
	eventually("the session ended by Hyper+D again", func() bool { _, a := tool(); return !a })
	hyper()
	eventually("a session after Hyper+D", func() bool { _, a := tool(); return a })
	press("Esc", cocoatest.KeyEscape, 0)
	eventually("the session ended by Esc", func() bool { _, a := tool(); return !a })

	// 4. F freezes and unfreezes; Esc unfreezes, then ends the session
	active := func() bool { _, a := tool(); return a }
	hyper()
	eventually("a session after Hyper+D", active)
	press("F", cocoatest.KeyF, 0)
	eventually("frozen after F", frozen)
	seq := laidOut()
	before, hasDraft := draft()
	onMain(func() { cocoatest.PostKey(ma.view, cocoatest.KeyX, 0) }) // a key the tool doesn't take
	time.Sleep(300 * time.Millisecond)
	if n := laidOut(); n != seq {
		t.Errorf("%d frames laid out while frozen", n-seq)
	}
	press("F", cocoatest.KeyF, 0)
	eventually("unfrozen by F again, the session on", func() bool { return !frozen() && active() })
	time.Sleep(300 * time.Millisecond) // frames laid out again
	if after, ok := draft(); hasDraft && ok && after != before {
		t.Errorf("the X typed while frozen reached the composer: %q, was %q", after, before)
	}
	press("F", cocoatest.KeyF, 0)
	eventually("frozen after F", frozen)
	press("Esc", cocoatest.KeyEscape, 0)
	eventually("unfrozen by Esc, the session on", func() bool { return !frozen() && active() })
	press("Esc", cocoatest.KeyEscape, 0)
	eventually("the session ended by Esc again", func() bool { return !active() })

	// 5. the old shortcut starts none, and isn't the menu's ⌘⇧D
	sb := sidebarHidden()
	press("⌥⇧⌘D", cocoatest.KeyD, cmd|opt|shift)
	time.Sleep(time.Second)
	if _, a := tool(); a {
		t.Error("⌘⌥⇧D, the old shortcut, started a session")
	}
	if now := sidebarHidden(); now != sb {
		t.Errorf("⌘⌥⇧D toggled the sidebar (hidden %v → %v)", sb, now)
	}

	// 6. the code again turns it off
	konami()
	eventually("the tool off after the code again", func() bool { on, _ := tool(); return !on })
	eventually("inspect_tool gone from ui.json", func() bool { return !saved() })
}
