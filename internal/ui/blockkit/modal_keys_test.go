package blockkit

import (
	"testing"
	"time"

	"gioui.org/io/key"
	"gioui.org/layout"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/popups"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/shortcut"
)

func (e *bkEnv) keyMods(n key.Name, m key.Modifiers) {
	e.r.Queue(key.Event{Name: n, Modifiers: m, State: key.Press})
	e.frame()
	e.r.Queue(key.Event{Name: n, Modifiers: m, State: key.Release})
	e.frame()
	e.frame()
}

func (e *bkEnv) gtx() layout.Context { return layout.Context{Source: e.r.Source()} }

// A confirm object (AttachmentActionConfirmDialog, a DeprecatedBaseDialog
// without focusOnPrimaryButton): nothing is focused on open, so Enter and
// Space do nothing; Tab goes close button, deny, confirm, wrapping; Enter
// on the confirm button sends the action, Space on deny doesn't.
func TestConfirmObjectKeys(t *testing.T) {
	e := newBKEnv(t, false, false)
	e.show("buttons")
	n := len(e.fs.BlockActionCalls())
	open := func() *popups.Modal {
		t.Helper()
		e.modals = nil
		e.idle("bk_buttons")
		e.clickEl("bk_buttons", "btn_confirm")
		e.frame()
		e.frame()
		m, ok := topOverlay[*popups.Modal](e)
		if !ok || m.Dismissed() {
			t.Fatal("the confirm dialog didn't open")
		}
		return m
	}
	focus := func(m *popups.Modal, want string, ring bool) {
		t.Helper()
		got, r := m.FocusedButton(e.gtx())
		if got != want || (want != "" && r != ring) {
			t.Fatalf("focused %q (ring %v), want %q (ring %v)", got, r, want, ring)
		}
	}

	m := open()
	focus(m, "", false)
	e.key(key.NameReturn)
	e.key(key.NameSpace)
	if m.Dismissed() {
		t.Fatal("Enter or Space closed a dialog with nothing focused")
	}
	e.noNewCall(n, 100*time.Millisecond, "Enter with nothing focused")
	for _, want := range []string{"Close", "Keep it", "Delete", "Close"} {
		e.key(key.NameTab)
		focus(m, want, true)
	}
	e.keyMods(key.NameTab, key.ModShift)
	focus(m, "Delete", true)
	e.key(key.NameReturn)
	if !m.Dismissed() {
		t.Fatal("Enter on Delete didn't close the dialog")
	}
	e.calls(n + 1)
	n++

	m = open()
	e.key(key.NameTab)
	e.key(key.NameTab)
	focus(m, "Keep it", true)
	e.key(key.NameSpace)
	if !m.Dismissed() {
		t.Fatal("Space on Keep it didn't close the dialog")
	}
	e.noNewCall(n, 150*time.Millisecond, "Keep it")
}

// A Block Kit modal: the footer's submit button has the focus (no ring)
// with no form element in the body; Tab cycles the header's close button,
// the footer's Close and Submit; Enter on Close backs out; ⌘Enter (the
// meta key elsewhere) submits.
func TestBlockKitModalKeys(t *testing.T) {
	e := newBKEnv(t, false, false)
	pt := func(s string) []byte { return mustJSON(Text{Type: "plain_text", Text: s}) }
	view := func() *core.View {
		return &core.View{ID: "V0FAKEKEYS", RootViewID: "V0FAKEKEYS", Type: "modal", Title: pt("Synthetic"),
			Submit: pt("Save"), Close: pt("Cancel"), Blocks: []byte(`[{"type":"section","text":{"type":"plain_text","text":"Nothing to fill in."}}]`)}
	}
	open := func() *Stack {
		t.Helper()
		s := newStack(e.h, view())
		e.h.ShowOverlay(s)
		e.frame()
		e.frame()
		e.frame()
		return s
	}
	focus := func(s *Stack, want string, ring bool) {
		t.Helper()
		got, r := s.FocusedButton(e.gtx())
		if got != want || (want != "" && r != ring) {
			t.Fatalf("focused %q (ring %v), want %q (ring %v)", got, r, want, ring)
		}
	}

	s := open()
	focus(s, "submit", false)
	for _, want := range []string{"close", "cancel", "submit", "close"} {
		e.key(key.NameTab)
		focus(s, want, true)
	}
	e.keyMods(key.NameTab, key.ModShift)
	focus(s, "submit", true)
	e.keyMods(key.NameTab, key.ModShift)
	focus(s, "cancel", true)
	e.key(key.NameReturn)
	if !s.done {
		t.Fatal("Enter on Close didn't close the modal")
	}

	s = open()
	cmd := key.ModSuper
	if shortcut.GOOS == "darwin" {
		cmd = key.ModCommand
	}
	e.keyMods(key.NameReturn, cmd)
	if !s.busy && !s.done {
		t.Fatal("⌘Enter didn't submit")
	}
}
