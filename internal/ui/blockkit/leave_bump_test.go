package blockkit

import (
	"image"
	"testing"

	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// TestLeaveBumpIsWizardSpeedBump: closing a Block Kit modal that would
// lose what's entered shows BlockKitModal's WizardSpeedBump (boot-render
// D5xX), a sheet over the card, not a dialog of its own: Keep Editing has
// the focus, Escape cancels it and the modal stays, Tab moves to Leave,
// and Enter there leaves. Clack opened it as a separate confirm dialog.
func TestLeaveBumpIsWizardSpeedBump(t *testing.T) {
	h := &confirmHost{th: theme.New(false)}
	v := &core.View{ID: "V1", RootViewID: "V1", Type: "modal",
		Title:  mustJSON(Text{Type: "plain_text", Text: "Request time off"}),
		Submit: mustJSON(Text{Type: "plain_text", Text: "Submit"})}
	s := newStack(h, v)
	var r input.Router
	var ops op.Ops
	frame := func() {
		ops.Reset()
		gtx := layout.Context{Ops: &ops, Source: r.Source(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(1200, 800))}
		s.Layout(gtx)
		r.Frame(&ops)
	}
	frame()
	left := 0
	s.showLeave(false, func() { left++ })
	for i := 0; i < 3; i++ {
		frame()
	}
	if len(h.shown) != 0 {
		t.Fatalf("the leave prompt opened %d overlays, want none (a sheet in the card)", len(h.shown))
	}
	if !s.leaveUp() {
		t.Fatal("the leave prompt isn't up")
	}
	if !r.Source().Focused(&s.bump.keep) {
		t.Error("Keep Editing doesn't have the focus (WizardSpeedBump focuses its cancel button)")
	}
	press := func(n key.Name, mods key.Modifiers) {
		r.Queue(key.Event{Name: n, Modifiers: mods, State: key.Press}, key.Event{Name: n, Modifiers: mods, State: key.Release})
		for i := 0; i < 3; i++ {
			frame()
		}
	}
	press(key.NameEscape, 0)
	if s.leaveUp() || left != 0 || s.done {
		t.Fatalf("Escape on the prompt: up %v, left %d, modal closed %v; want it gone and the modal kept", s.leaveUp(), left, s.done)
	}
	s.showLeave(true, func() { left++ })
	for i := 0; i < 3; i++ {
		frame()
	}
	if s.bump.cont != "Continue" {
		t.Errorf("a pushed view's prompt says %q, want Continue", s.bump.cont)
	}
	press(key.NameTab, 0)
	if !r.Source().Focused(&s.bump.leave) {
		t.Fatal("Tab didn't move to the continue button")
	}
	press(key.NameTab, 0)
	if !r.Source().Focused(&s.bump.keep) {
		t.Fatal("Tab didn't wrap back to Keep Editing")
	}
	press(key.NameTab, key.ModShift)
	press(key.NameReturn, 0)
	if left != 1 || s.leaveUp() {
		t.Errorf("Enter on Continue: left %d times, prompt up %v; want left once", left, s.leaveUp())
	}
}
