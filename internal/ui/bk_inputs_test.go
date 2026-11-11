package ui

import (
	"encoding/json"
	"image"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"gioui.org/io/key"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/blockkit"
)

// The modal field Karim couldn't delete from (2026-11-10), through the
// whole window: the fake app's settings modal (shortcut SURFA3: an
// italic context line, a divider, a context line, one multiline
// plain_text_input with an initial_value) opened over a channel, its
// field clicked and edited with every frame's root key handlers, the
// message list, the composer and the plugins' global keys in play. The
// every-field modal (SURFA4) takes Backspace and typing in each input.
func TestBlockKitModalFieldsEdit(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Gallery)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	e := startEnv(t, fs, t.TempDir())
	fs.Emit = func(ev map[string]any) {
		b, _ := json.Marshal(ev)
		typ, _ := ev["type"].(string)
		e.a.data.core.Inject(slack.Event{Type: typ, Raw: b})
	}
	e.waitReady()
	a := e.a
	e.openChannel(fakeslack.BlockKitGalleryID)
	e.until("the gallery", 20*time.Second, func() bool { return e.loadedMessages(fakeslack.BlockKitGalleryID) >= 7 })
	e.settle(time.Second)
	onMac := key.ModShortcut != key.ModShortcutAlt
	mod := key.ModShortcut

	field := func(bid string) blockkit.FieldInfo {
		t.Helper()
		for _, f := range blockkit.ModalFields(a) {
			if f.BlockID == bid {
				return f
			}
		}
		t.Fatalf("no field %s in the modal", bid)
		return blockkit.FieldInfo{}
	}
	open := func(action, view string) {
		t.Helper()
		blockkit.RunShortcut(a, core.AppShortcut{ActionID: action, AppID: fakeslack.AppAppID, AppName: "Deploy Bot", Type: "global_action"},
			fakeslack.BlockKitGalleryID, "")
		e.until("the modal "+view, 15*time.Second, func() bool {
			m, ok := blockkit.OpenModal(a)
			return ok && m.ViewID == view && len(blockkit.ModalFields(a)) > 0 && !blockkit.ModalFields(a)[0].R.Empty()
		})
		e.settle(300 * time.Millisecond)
	}
	focus := func(bid string) {
		t.Helper()
		// the every-field modal scrolls: Slack's caps at 640px
		for range 12 {
			f := field(bid)
			dy := 0
			switch {
			case f.R.Max.Y > f.Body.Max.Y:
				dy = f.R.Max.Y - f.Body.Max.Y + 8
			case f.R.Min.Y < f.Body.Min.Y:
				dy = f.R.Min.Y - f.Body.Min.Y - 8
			}
			if dy == 0 || f.Body.Empty() {
				break
			}
			e.scroll(center2(f.Body), float32(dy))
			for range 3 {
				e.frame()
			}
		}
		f := field(bid)
		e.click(center2(f.R))
		for range 4 {
			e.frame()
		}
		if !field(bid).Focused {
			t.Fatalf("%s: a click at %v (box %v) didn't focus it", bid, center2(f.R), f.R)
		}
	}
	check := func(bid, step, want string) {
		t.Helper()
		f := field(bid)
		if !f.Focused {
			t.Errorf("%s, %s: the field lost the focus", bid, step)
		}
		if got := strings.TrimRight(f.Text, "\n"); got != want {
			t.Errorf("%s, %s: the field holds %q, want %q", bid, step, got, want)
		}
	}
	typ := func(s string) { // as Gio's Window.EditorInsert: the edit, then the caret after it
		sel := e.r.EditorState().Selection.Range
		pos := min(sel.Start, sel.End) + utf8.RuneCountInString(s)
		e.r.Queue(key.EditEvent{Range: sel, Text: s}, key.SelectionEvent{Start: pos, End: pos})
		for range 3 {
			e.frame()
		}
	}

	open("SURFA3", fakeslack.SurfNoteView)
	focus("note")
	e.press(key.NameEnd, mod)
	e.press(key.NameDeleteBackward, 0)
	check("note", "Backspace", "pinned tes")
	e.press(key.NameDeleteBackward, mod)
	if onMac {
		check("note", "Cmd+Backspace", "")
		typ("pinned ")
	} else {
		check("note", "Ctrl+Backspace", "pinned ")
	}
	typ("abc")
	check("note", "typing", "pinned abc")
	e.press(key.NameHome, 0)
	e.press(key.NameDeleteForward, 0)
	check("note", "Home, Delete", "inned abc")
	e.press("A", mod)
	e.press(key.NameDeleteBackward, 0)
	check("note", "select all, Backspace", "")
	typ("new note")
	check("note", "typing", "new note")
	blockkit.SubmitModal(a)
	e.until("views.submit", 10*time.Second, func() bool {
		_, _, sub, _ := fs.SurfaceApp().Snapshot()
		return len(sub) > 0
	})
	_, _, sub, _ := fs.SurfaceApp().Snapshot()
	if st := sub[len(sub)-1].Get("state"); !strings.Contains(st, `"value":"new note"`) {
		t.Errorf("views.submit state %s, want the note's value \"new note\"", st)
	}
	e.until("the modal to close", 10*time.Second, func() bool { _, ok := blockkit.OpenModal(a); return !ok })

	open("SURFA4", fakeslack.SurfInputsView)
	for _, c := range []struct{ bid, initial, typed string }{
		{"single", "one line", "x"}, {"multi", "two\nlines", "x"}, {"number", "4242", "7"},
		{"email", "ada@example.com", "x"}, {"url", "https://example.com/path", "x"}, {"rich", "rich start", "x"},
	} {
		focus(c.bid)
		e.press(key.NameEnd, mod)
		e.press(key.NameDeleteBackward, 0)
		check(c.bid, "Backspace at the end", c.initial[:len(c.initial)-1])
		e.press("A", mod)
		e.press(key.NameDeleteBackward, 0)
		check(c.bid, "select all, Backspace", "")
		typ(c.typed)
		check(c.bid, "typing", c.typed)
	}
	blockkit.BackModal(a)
	e.settle(300 * time.Millisecond)
}

func center2(r image.Rectangle) image.Point { return r.Min.Add(r.Size().Div(2)) }
