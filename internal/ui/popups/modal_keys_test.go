package popups

import (
	"image"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/refocus"
)

// Slack's modal keyboard model through Gio's router (modal_focus.go):
// what has the focus on open, Enter and Space on the focused button,
// Escape, Tab / Shift+Tab cycling inside the dialog with the ring, and the
// focus going back to the field that had it.

// mdField stands in for the composer: a focusable widget that takes the
// focus back through refocus, as composer.Editor does.
type mdField struct{ tag int }

func (f *mdField) layout(gtx layout.Context) {
	if refocus.Take(gtx, &f.tag) {
		gtx.Execute(key.FocusCmd{Tag: &f.tag})
	}
	for {
		if _, ok := gtx.Event(key.FocusFilter{Target: &f.tag}); !ok {
			break
		}
	}
	st := clip.Rect{Max: image.Pt(400, 40)}.Push(gtx.Ops)
	event.Op(gtx.Ops, &f.tag)
	st.Pop()
	refocus.Note(gtx, &f.tag)
}

type mdEnv struct {
	t     *testing.T
	h     *tzHost
	r     input.Router
	ops   op.Ops
	now   time.Time
	field mdField
}

// newMdEnv starts with the field focused, like a composer being typed in.
func newMdEnv(t *testing.T, h *tzHost) *mdEnv {
	refocus.Reset()
	t.Cleanup(refocus.Reset)
	e := &mdEnv{t: t, h: h, now: time.Now()}
	e.frame()
	e.r.Source().Execute(key.FocusCmd{Tag: &e.field.tag})
	e.frame()
	e.frame()
	if !e.fieldFocused() {
		t.Fatal("the stand-in composer didn't take the focus")
	}
	return e
}

func (e *mdEnv) frame() {
	e.ops.Reset()
	gtx := layout.Context{Ops: &e.ops, Source: e.r.Source(), Now: e.now,
		Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(1200, 800))}
	e.field.layout(gtx)
	e.h.mu.Lock()
	shown := append([]Overlay(nil), e.h.shown...)
	e.h.mu.Unlock()
	for _, o := range shown {
		if o.Dismissed() {
			continue
		}
		g := gtx
		g.Constraints = layout.Constraints{Max: gtx.Constraints.Max}
		o.Layout(g)
	}
	e.r.Frame(&e.ops)
	e.now = e.now.Add(16 * time.Millisecond)
}

func (e *mdEnv) settle() {
	for range 3 {
		e.frame()
	}
}

func (e *mdEnv) press(n key.Name, mods key.Modifiers) {
	e.r.Queue(key.Event{Name: n, Modifiers: mods, State: key.Press})
	e.frame()
	e.r.Queue(key.Event{Name: n, Modifiers: mods, State: key.Release})
	e.frame()
	e.frame()
}

func (e *mdEnv) gtx() layout.Context { return layout.Context{Source: e.r.Source()} }

func (e *mdEnv) fieldFocused() bool { return e.r.Source().Focused(&e.field.tag) }

// top is the open modal on top.
func (e *mdEnv) top() *Modal {
	e.h.mu.Lock()
	defer e.h.mu.Unlock()
	for i := len(e.h.shown) - 1; i >= 0; i-- {
		if m, ok := e.h.shown[i].(*Modal); ok && !m.Dismissed() {
			return m
		}
	}
	return nil
}

// focus checks the top modal's focused button and its ring.
func (e *mdEnv) focus(want string, ring bool) {
	e.t.Helper()
	m := e.top()
	if m == nil {
		e.t.Fatalf("focus %q: no modal open", want)
	}
	got, r := m.FocusedButton(e.gtx())
	if got != want || (want != "" && r != ring) {
		e.t.Fatalf("focused %q (ring %v), want %q (ring %v)", got, r, want, ring)
	}
}

// open runs show (which opens a modal) and the frames that focus it.
func (e *mdEnv) open(show func()) *Modal {
	e.t.Helper()
	show()
	e.settle()
	m := e.top()
	if m == nil {
		e.t.Fatal("no modal opened")
	}
	if e.fieldFocused() {
		e.t.Fatal("the composer kept the focus under the modal")
	}
	return m
}

// cycle presses Tab (back: Shift+Tab) and checks each focus in turn.
func (e *mdEnv) cycle(back bool, want ...string) {
	e.t.Helper()
	mods := key.Modifiers(0)
	if back {
		mods = key.ModShift
	}
	for _, w := range want {
		e.press(key.NameTab, mods)
		e.focus(w, true)
	}
}

// closedRefocused checks that the modal closed and the composer has the
// focus again.
func (e *mdEnv) closedRefocused(m *Modal) {
	e.t.Helper()
	e.settle()
	if !m.Dismissed() {
		e.t.Fatal("the modal is still open")
	}
	if !e.fieldFocused() {
		e.t.Fatal("the focus didn't go back to the composer")
	}
}

func mdConfirm(e *mdEnv, n *int) *Modal {
	return e.open(func() {
		Confirm(e.h, "Delete this?", "Are you sure?", "Delete", true, nil, func() { *n++ })
	})
}

// ConfirmationModal: the action has the focus and the ring on open; Enter
// and Space press it; Escape closes; the composer gets the focus back.
func TestModalConfirmKeys(t *testing.T) {
	h := hcEnv(t)
	e := newMdEnv(t, h)
	ok := 0

	m := mdConfirm(e, &ok)
	e.focus("Delete", true)
	e.press(key.NameReturn, 0)
	if ok != 1 {
		t.Fatalf("Enter on Delete: confirmed %d times", ok)
	}
	e.closedRefocused(m)

	m = mdConfirm(e, &ok)
	e.press(key.NameSpace, 0)
	if ok != 2 {
		t.Fatalf("Space on Delete: confirmed %d times", ok-1)
	}
	e.closedRefocused(m)

	m = mdConfirm(e, &ok)
	e.press(key.NameEscape, 0)
	if ok != 2 {
		t.Fatal("Escape confirmed")
	}
	e.closedRefocused(m)

	// c-sk-modal tab order: Cancel, Delete, then the close button (it
	// comes after the footer); wrapping both ways
	m = mdConfirm(e, &ok)
	e.cycle(false, "Close", "Cancel", "Delete", "Close")
	e.cycle(true, "Delete", "Cancel")
	e.press(key.NameReturn, 0) // Enter presses the focused Cancel
	if ok != 2 {
		t.Fatal("Enter on Cancel confirmed")
	}
	e.closedRefocused(m)

	m = mdConfirm(e, &ok)
	e.cycle(false, "Close")
	e.press(key.NameSpace, 0) // Space on ×
	if ok != 2 {
		t.Fatal("Space on the close button confirmed")
	}
	e.closedRefocused(m)
}

// Tab stays inside the modal: it never reaches the composer behind it.
func TestModalFocusTrapped(t *testing.T) {
	h := hcEnv(t)
	e := newMdEnv(t, h)
	ok := 0
	m := mdConfirm(e, &ok)
	for i := range 7 {
		e.press(key.NameTab, 0)
		if e.fieldFocused() {
			t.Fatalf("Tab %d moved the focus out of the modal", i+1)
		}
		if got, _ := m.FocusedButton(e.gtx()); got == "" {
			t.Fatalf("Tab %d: nothing in the modal has the focus", i+1)
		}
	}
	e.press(key.NameEscape, 0)
	e.closedRefocused(m)
}

// A mouse press hides the ring (Slack's keyboard-focus tracking); Tab
// brings it back.
func TestModalRingHidesOnPointer(t *testing.T) {
	h := hcEnv(t)
	e := newMdEnv(t, h)
	ok := 0
	m := mdConfirm(e, &ok)
	e.focus("Delete", true)
	p := m.rect.Min.Add(image.Pt(40, 90)) // the body text
	e.r.Queue(pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: f32pt(p)})
	e.frame()
	e.r.Queue(pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: f32pt(p)})
	e.settle()
	e.focus("Delete", false)
	e.cycle(false, "Close")
	if ok != 0 || m.Dismissed() {
		t.Fatal("clicking the body pressed something")
	}
}

// "Delete message" (BaseDeleteMessageDialog, a DeprecatedBaseDialog with
// focusOnPrimaryButton): Delete is focused with the ring; the header's
// close button comes first in the tab order.
func TestDeleteMessageKeys(t *testing.T) {
	h := mkHost(t)
	e := newMdEnv(t, h)
	m := e.open(func() { ConfirmDelete(h, mkMsg(true)) })
	if m.o.Title != "Delete message" {
		t.Fatalf("title %q", m.o.Title)
	}
	e.focus("Delete", true)
	e.cycle(false, "Close", "Cancel", "Delete", "Close")
	e.cycle(true, "Delete", "Cancel", "Close")
	e.press(key.NameEscape, 0)
	e.closedRefocused(m)

	m = e.open(func() { ConfirmDelete(h, mkMsg(true)) })
	e.press(key.NameReturn, 0)
	e.closedRefocused(m)
}

// "Delete file" (DeleteFileDialog: focusOnPrimaryButton, header close).
func TestDeleteFileKeys(t *testing.T) {
	h := mkHost(t)
	e := newMdEnv(t, h)
	m := e.open(func() {
		ConfirmDeleteFile(h, &richtext.File{ID: "F0FAKE00001", Name: "synthetic.txt", Title: "synthetic.txt"})
	})
	e.focus("Yes, Delete This File", true)
	e.cycle(false, "Close", "Cancel", "Yes, Delete This File")
	e.cycle(true, "Cancel")
	e.press(key.NameSpace, 0)
	e.closedRefocused(m)
}

// "Delete this section?" (ConfirmationModal, danger submit): Delete has
// the focus; Enter deletes.
func TestDeleteSectionKeys(t *testing.T) {
	h := hcEnv(t)
	e := newMdEnv(t, h)
	deleted := 0
	m := e.open(func() { h.ShowOverlay(NewDeleteSectionModal(h, "Synthetic", func() { deleted++ })) })
	e.focus("Delete", true)
	e.cycle(false, "Close", "Cancel", "Delete")
	e.press(key.NameReturn, 0)
	if deleted != 1 {
		t.Fatalf("Enter: deleted %d times", deleted)
	}
	e.closedRefocused(m)

	m = e.open(func() { h.ShowOverlay(NewDeleteSectionModal(h, "Synthetic", func() { deleted++ })) })
	e.press(key.NameEscape, 0)
	if deleted != 1 {
		t.Fatal("Escape deleted the section")
	}
	e.closedRefocused(m)
}

// Leaving a private channel (LeaveChannel: autoFocus on "Leave Channel").
func TestLeavePrivateChannelKeys(t *testing.T) {
	h := hcEnv(t)
	e := newMdEnv(t, h)
	m := e.open(func() { leaveChannel(h, "C0FAKE00009", "private", "plans", false) })
	if m.o.Title != "Leave plans" {
		t.Fatalf("title %q", m.o.Title)
	}
	e.focus("Leave Channel", true)
	e.cycle(true, "Cancel", "Close", "Leave Channel")
	e.press(key.NameEscape, 0)
	e.closedRefocused(m)
}

// "Rename this section": the name field has the focus (no ring on a
// button); Tab moves between the field, Save (once there is a change)
// and the close button; Enter in the field saves.
func TestRenameSectionTab(t *testing.T) {
	h := hcEnv(t)
	e := newMdEnv(t, h)
	var rs *RenameSectionModal
	e.open(func() {
		rs = NewRenameSectionModal(h, "S0FAKE", "Synthetic", "")
		h.ShowOverlay(rs.modal)
	})
	if !e.r.Source().Focused(&rs.ed) {
		t.Fatal("the name field doesn't have the focus")
	}
	e.focus("", false)
	e.cycle(false, "Close") // Save is disabled until the name changes
	e.press(key.NameTab, 0)
	if !e.r.Source().Focused(&rs.ed) {
		t.Fatal("Tab from the close button didn't wrap to the name field")
	}
	rs.SetName("Synthetic two")
	e.settle()
	e.cycle(false, "Save", "Close")
	e.press(key.NameEscape, 0)
	e.closedRefocused(rs.modal)
}

// A modal over a modal: closing the top one gives the focus back to the
// one under it, not to the composer.
func TestNestedModalFocus(t *testing.T) {
	h := hcEnv(t)
	e := newMdEnv(t, h)
	ok := 0
	a := mdConfirm(e, &ok)
	e.cycle(false, "Close", "Cancel")
	b := e.open(func() { Confirm(h, "Second", "", "OK", false, nil, nil) })
	e.focus("OK", true)
	e.press(key.NameEscape, 0)
	e.settle()
	if !b.Dismissed() || a.Dismissed() {
		t.Fatalf("Escape closed the wrong modal (top %v, under %v)", b.Dismissed(), a.Dismissed())
	}
	if e.fieldFocused() {
		t.Fatal("the composer took the focus while a modal is still open")
	}
	e.focus("Cancel", true)
	e.press(key.NameEscape, 0)
	e.closedRefocused(a)
}

// A Clickable's key click needs press and release while focused: the
// Enter that opened a dialog (released after it took the focus) presses
// nothing in it.
func TestModalOpeningEnterDoesNotPress(t *testing.T) {
	h := hcEnv(t)
	e := newMdEnv(t, h)
	ok := 0
	e.r.Queue(key.Event{Name: key.NameReturn, State: key.Press})
	e.frame()
	Confirm(h, "Delete this?", "", "Delete", true, nil, func() { ok++ })
	e.settle()
	e.r.Queue(key.Event{Name: key.NameReturn, State: key.Release})
	e.settle()
	if ok != 0 || e.top() == nil {
		t.Fatal("the opening Enter's release confirmed the dialog")
	}
}

func f32pt(p image.Point) f32.Point { return f32.Pt(float32(p.X), float32(p.Y)) }

var _ = core.Message{}
