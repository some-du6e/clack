package blockkit

import (
	"strings"
	"testing"
	"time"

	"gioui.org/io/key"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
)

// Karim, 2026-11-10: "u cant delete the modal editable field?" (an app's
// settings modal: one multiline plain_text_input with an initial_value).
// Every text element of a modal, clicked and edited with the keys a person
// uses, as Slack's browser fields take them: Backspace, Delete, Mod+End,
// Home, Mod+Backspace (a word on Linux and Windows; the line on macOS,
// with Option for a word), select all and delete, typing. Each step
// shows in the field and in the state views.submit sends. The view_updated
// Slack sends after view_opened, and a later views.update of the same
// view, keep the focused field and what was typed (Slack's input stays
// mounted). fakeslack's SurfNote and SurfInputs fixtures, synthetic.
func TestModalTextInputsEdit(t *testing.T) {
	e := newBKEnv(t, false, false)
	onMac := key.ModShortcut != key.ModShortcutAlt
	open := func(raw map[string]any) (*Stack, *core.View) {
		t.Helper()
		v, err := core.ParseView(mustJSON(raw))
		if err != nil {
			t.Fatal(err)
		}
		s := newStack(e.h, v)
		e.h.ShowOverlay(s)
		e.frame()
		e.frame()
		s.update(v) // Slack's view_updated right after view_opened
		e.frame()
		e.frame()
		return s, v
	}
	field := func(s *Stack, bid string) FieldInfo {
		t.Helper()
		for _, f := range s.fields() {
			if f.BlockID == bid {
				return f
			}
		}
		t.Fatalf("no field %s (fields %+v)", bid, s.fields())
		return FieldInfo{}
	}
	check := func(s *Stack, bid, step, want string) {
		t.Helper()
		f := field(s, bid)
		if !f.Focused {
			t.Errorf("%s, %s: the field lost the focus", bid, step)
		}
		if strings.TrimRight(f.Text, "\n") != want {
			t.Errorf("%s, %s: the field holds %q, want %q", bid, step, f.Text, want)
		}
		if strings.TrimSpace(f.Value) != strings.TrimSpace(want) {
			t.Errorf("%s, %s: state value %q, want %q", bid, step, f.Value, strings.TrimSpace(want))
		}
	}
	focus := func(s *Stack, bid string) {
		t.Helper()
		f := field(s, bid)
		if f.R.Empty() {
			t.Fatalf("%s wasn't drawn: %+v", bid, f)
		}
		e.click(center(f.R))
		e.frame()
		e.frame()
		if !field(s, bid).Focused {
			t.Fatalf("%s: a click at %v (box %v) didn't focus it", bid, center(f.R), f.R)
		}
	}
	mod := key.ModShortcut

	// the settings modal Karim couldn't edit
	s, v := open(fakeslack.SurfNote(e.fs))
	f := field(s, "note")
	if f.Text != fakeslack.SurfNoteInitial || f.Value != fakeslack.SurfNoteInitial {
		t.Fatalf("note field %+v, want the initial value %q", f, fakeslack.SurfNoteInitial)
	}
	focus(s, "note")
	e.keyMods(key.NameEnd, mod)
	check(s, "note", "Mod+End", "pinned test")
	e.keyMods(key.NameDeleteBackward, 0)
	check(s, "note", "Backspace", "pinned tes")
	e.keyMods(key.NameDeleteBackward, mod)
	if onMac {
		check(s, "note", "Cmd+Backspace", "")
		e.typeText("pinned tes")
		e.keyMods(key.NameDeleteBackward, key.ModShortcutAlt)
		check(s, "note", "Option+Backspace", "pinned ")
	} else {
		check(s, "note", "Ctrl+Backspace", "pinned ")
	}
	e.typeText("abc")
	check(s, "note", "typing", "pinned abc")
	e.keyMods(key.NameHome, 0)
	e.keyMods(key.NameDeleteForward, 0)
	check(s, "note", "Home, Delete", "inned abc")
	e.keyMods(key.NameDeleteForward, mod)
	if onMac {
		check(s, "note", "Cmd+Delete", "")
	} else {
		check(s, "note", "Ctrl+Delete", " abc")
	}
	e.typeText("x")
	e.keyMods("A", mod)
	e.keyMods(key.NameDeleteBackward, 0)
	check(s, "note", "select all, Backspace", "")
	e.typeText("new note")
	check(s, "note", "typing", "new note")
	// the app's views.update of the same view: same block, same field
	s.update(v)
	e.frame()
	e.frame()
	check(s, "note", "after views.update", "new note")
	e.keyMods(key.NameDeleteBackward, 0)
	check(s, "note", "Backspace after views.update", "new not")
	n := len(submittedState(e))
	s.doSubmit()
	e.until("views.submit", 10*time.Second, func() bool { return len(submittedState(e)) > n })
	if st := submittedState(e)[n]; !strings.Contains(st, `"note":{"v":{"type":"plain_text_input","value":"new not"}}`) {
		t.Errorf("views.submit state %s, want note.v = \"new not\"", st)
	}
	e.closeOverlays()

	// every text element, each with an initial value
	s, _ = open(fakeslack.SurfInputs())
	for _, c := range []struct {
		bid, initial, typed string
	}{
		{"single", "one line", "x"},
		{"multi", "two\nlines", "x"},
		{"number", "4242", "7"},
		{"email", "ada@example.com", "x"},
		{"url", "https://example.com/path", "x"},
		{"rich", "rich start", "x"},
	} {
		f := field(s, c.bid)
		if strings.TrimRight(f.Text, "\n") != c.initial {
			t.Errorf("%s: the field opened with %q, want %q", c.bid, f.Text, c.initial)
		}
		focus(s, c.bid)
		e.keyMods(key.NameEnd, mod)
		e.keyMods(key.NameDeleteBackward, 0)
		want := c.initial[:len(c.initial)-1]
		check(s, c.bid, "Backspace at the end", want)
		e.keyMods("A", mod)
		e.keyMods(key.NameDeleteBackward, 0)
		check(s, c.bid, "select all, Backspace", "")
		e.typeText(c.typed)
		check(s, c.bid, "typing", c.typed)
	}
	e.closeOverlays()
}

// submittedState is the state param of every views.submit the fake app got.
func submittedState(e *bkEnv) []string {
	_, _, sub, _ := e.fs.SurfaceApp().Snapshot()
	out := make([]string, len(sub))
	for i, f := range sub {
		out[i] = f.Get("state")
	}
	return out
}
