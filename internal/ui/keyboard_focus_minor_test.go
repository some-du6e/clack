package ui

// The keyboard focus round's smaller fixes (keyboard-focus-fixes'
// MINORs), through the real panes on fakeslack, synthetic content only.

import (
	"image"
	"strings"
	"testing"
	"time"

	"gioui.org/io/key"

	"github.com/some-du6e/clack/internal/fakeslack"
)

// TestKeyboardFocusAltGrTypesNoPaste: AltGr+V, which Windows sends as
// Ctrl+Alt+V, types its character ("@" on Czech, Hungarian and Croatian
// layouts) into the input with nothing focused and pastes nothing: Slack's
// zQkO wants isCmdKey with e.key "v", and e.key is "@".
func TestKeyboardFocusAltGrTypesNoPaste(t *testing.T) {
	x := openTopEnv(t)
	c := x.a.convos[x.ch+"/"]
	box := x.box(x.ch, "")
	t.Cleanup(func() { x.kfClearDraft(c) })
	x.kfNothingFocused(t)
	before := box.DocText()
	altGr := key.ModShortcut | key.ModAlt
	x.r.Queue(key.Event{Name: "V", Modifiers: altGr, State: key.Press},
		key.EditEvent{Range: x.r.EditorState().Selection.Range, Text: "@"},
		key.Event{Name: "V", Modifiers: altGr, State: key.Release})
	x.until("the @ in the input", 2*time.Second, func() bool { return x.kfBoxFocused(c) })
	x.settle(300 * time.Millisecond)
	if got, want := strings.TrimSpace(box.DocText()), strings.TrimSpace(before)+"@"; got != want {
		t.Errorf("AltGr+V with nothing focused: the input holds %q, want %q", got, want)
	}
	if x.r.ClipboardRequested() {
		t.Error("AltGr+V asked for the clipboard: it pasted as Mod+V")
	}
}

// TestKeyboardFocusComposedTextOnly: with nothing focused, an input
// method's marked text (a dead key's, a CJK composition's first key)
// stays out of the message input, and the committed text goes in with the
// focus: "か", not "kか".
func TestKeyboardFocusComposedTextOnly(t *testing.T) {
	x := openTopEnv(t)
	c := x.a.convos[x.ch+"/"]
	box := x.box(x.ch, "")
	t.Cleanup(func() { x.kfClearDraft(c) })
	x.kfNothingFocused(t)
	before := box.DocText()
	// setMarkedText("k"): the text, then the composition over it
	x.r.Queue(key.EditEvent{Range: key.Range{Start: 0, End: 0}, Text: "k"}, key.CompositionEvent{Start: 0, End: 1})
	x.settle(300 * time.Millisecond)
	if got := box.DocText(); got != before || !x.r.Source().Focused(nil) {
		t.Fatalf("an input method's marked text with nothing focused: the input went from %q to %q (nothing focused %v)", before, got, x.r.Source().Focused(nil))
	}
	// insertText("か"): the commit over the marked text, the composition closed
	x.r.Queue(key.EditEvent{Range: key.Range{Start: 0, End: 1}, Text: "か"}, key.CompositionEvent{Start: -1, End: -1})
	x.until("the committed text in the input", 2*time.Second, func() bool { return x.kfBoxFocused(c) })
	x.settle(200 * time.Millisecond)
	if got, want := strings.TrimSpace(box.DocText()), strings.TrimSpace(before)+"か"; got != want {
		t.Errorf("the composition committed: the input holds %q, want %q", got, want)
	}
}

// TestKeyboardFocusPressDropsPendingListFocus: a thread opened in keyboard
// mode gives its list the focus once its replies are drawn, unless a click
// came first: a click in the message input while the replies load keeps
// the focus there when they arrive.
func TestKeyboardFocusPressDropsPendingListFocus(t *testing.T) {
	x := openTopEnv(t)
	c := x.a.convos[x.ch+"/"]
	t.Cleanup(func() { x.kfClearDraft(c) })
	c.comp.Focus()
	x.settle(200 * time.Millisecond)
	x.kfUpToList(t, func() bool { return x.cv().KeyboardSelected() != "" })
	x.root = fakeslack.ThreadOpenRead
	x.wheelTo(x.root)
	x.fs.SetFault("conversations.replies", fakeslack.Fault{Delay: 2 * time.Second, Times: 3})
	t.Cleanup(func() { x.fs.SetFault("conversations.replies", fakeslack.Fault{}) })
	asked := x.fs.CallCount("conversations.replies")
	x.a.OpenThread(x.ch, x.root)
	x.until("the replies asked for", 10*time.Second, func() bool { return x.fs.CallCount("conversations.replies") > asked })
	r := c.compRect
	x.click(image.Pt(r.Min.X+r.Dx()/2, r.Min.Y+r.Dy()/2))
	x.until("the click's focus in the input", 3*time.Second, func() bool { return x.kfBoxFocused(c) })
	x.until("the thread drawn", 20*time.Second, func() bool {
		tv := x.threadView(x.ch, x.root)
		return tv != nil && tv.HasMessages()
	})
	x.settle(500 * time.Millisecond)
	if tv := x.threadView(x.ch, x.root); x.kfListFocused(tv) || !x.kfBoxFocused(c) {
		t.Errorf("the thread's replies arrived after a click in the message input: its list has the focus %v, the input %v; the click's focus stays", x.kfListFocused(tv), x.kfBoxFocused(c))
	}
}
