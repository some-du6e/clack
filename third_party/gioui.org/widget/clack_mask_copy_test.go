// SPDX-License-Identifier: Unlicense OR MIT

package widget

import (
	"image"
	"testing"

	"gioui.org/font"
	"gioui.org/font/gofont"
	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/text"
	"gioui.org/unit"
)

// maskEd is a focused single-line Editor laid out on its own router.
type maskEd struct {
	r   input.Router
	gtx layout.Context
	e   *Editor
	sh  *text.Shaper
}

func newMaskEd(mask rune, s string) *maskEd {
	m := &maskEd{e: &Editor{SingleLine: true, Mask: mask}, sh: text.NewShaper(text.NoSystemFonts(), text.WithCollection(gofont.Collection()))}
	m.e.SetText(s)
	m.gtx = layout.Context{Ops: new(op.Ops), Constraints: layout.Exact(image.Pt(300, 40)), Locale: english, Source: m.r.Source()}
	m.gtx.Execute(key.FocusCmd{Tag: m.e})
	m.frame()
	m.frame()
	return m
}

func (m *maskEd) frame() {
	m.gtx.Ops.Reset()
	m.e.Layout(m.gtx, m.sh, font.Font{}, unit.Sp(10), op.CallOp{}, op.CallOp{})
	m.r.Frame(m.gtx.Ops)
}

func (m *maskEd) shortcut(name key.Name) {
	m.r.Queue(key.Event{Name: name, Modifiers: key.ModShortcut, State: key.Press})
	m.frame()
}

// Clack patch 67: a masked Editor is a password field, and Chromium copies
// and cuts nothing from one. Shortcut+C writes no clipboard there, and
// Shortcut+X neither writes it nor deletes the selection; an unmasked
// Editor does both.
func TestClackMaskedEditorCopiesNothing(t *testing.T) {
	for _, c := range []struct {
		name string
		mask rune
	}{{"plain", 0}, {"masked", '•'}} {
		m := newMaskEd(c.mask, "hunter2")
		m.e.SetCaret(0, m.e.Len())
		m.shortcut("C")
		_, got, ok := m.r.WriteClipboard()
		switch {
		case c.mask != 0 && ok:
			t.Errorf("%s: Shortcut+C copied %q", c.name, got)
		case c.mask == 0 && (!ok || string(got) != "hunter2"):
			t.Errorf("%s: Shortcut+C copied %q (%v), want the selection", c.name, got, ok)
		}
		m.shortcut("X")
		_, got, ok = m.r.WriteClipboard()
		switch {
		case c.mask != 0 && (ok || m.e.Text() != "hunter2"):
			t.Errorf("%s: Shortcut+X copied %q (%v) and left %q, want nothing copied or cut", c.name, got, ok, m.e.Text())
		case c.mask == 0 && m.e.Text() != "":
			t.Errorf("%s: Shortcut+X left %q, want the selection cut", c.name, m.e.Text())
		}
	}
}

// A masked Editor with the key focus calls MaskedFocus as it lays out,
// each frame; an unmasked one, or a masked one without the focus, doesn't.
func TestClackMaskedFocusHook(t *testing.T) {
	calls := 0
	MaskedFocus = func() { calls++ }
	t.Cleanup(func() { MaskedFocus = nil })
	m := newMaskEd('•', "hunter2")
	if calls == 0 {
		t.Errorf("a focused password field: MaskedFocus not called")
	}
	m.gtx.Execute(key.FocusCmd{Tag: nil})
	m.frame()
	calls = 0
	m.frame()
	if calls != 0 {
		t.Errorf("a password field without the focus: MaskedFocus called %d times", calls)
	}
	newMaskEd(0, "plain")
	if calls != 0 {
		t.Errorf("a focused unmasked Editor: MaskedFocus called %d times", calls)
	}
}
