package composer

import (
	"testing"

	"gioui.org/io/key"
)

// Smart quotes and symbols (Advanced > "Enable smart quotes and symbols"):
// Slack's TextSubstitutionsModule rules, each its own undo step, none in
// code; off, typing stays literal.
func TestSmartSymbols(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"a -- b", "a — b"},
		{"a --- b", "a --- b"},
		{"go -> here <- back <-> both", "go → here ← back ↔ both"},
		{"x --> y", "x --> y"},
		{"wait...", "wait…"},
		{"wait....", "wait…."},
		{`say "hi" it's 5'`, "say “hi” it’s 5'"},
		{`('quoted')`, "(‘quoted’)"},
	} {
		e := NewEditor()
		e.smart = true
		edType(e, c.in)
		if got := e.doc.Text(); got != c.want {
			t.Errorf("%q: got %q, want %q", c.in, got, c.want)
		}
	}
	e := NewEditor()
	e.smart = true
	edType(e, "a --")
	edType(e, " ")
	e.Undo()
	if got := e.doc.Text(); got != "a -- " {
		t.Fatalf("undo after the em dash: %q", got)
	}
	e = NewEditor()
	edType(e, `a -- "b"...`)
	if got := e.doc.Text(); got != `a -- "b"...` {
		t.Fatalf("smart symbols off: %q", got)
	}
	e = NewEditor()
	e.smart, e.plainText = true, true
	edType(e, "`a -- b` c -- d")
	if got := e.doc.Text(); got != "`a -- b` c — d" {
		t.Fatalf("markup kept: %q", got)
	}
}

// Accessibility > "Press ↑ in the empty message field to:" (onUpArrow):
// LIST focuses the list at the start of the first line, LIST_IF_EMPTY only
// when empty, EDIT and unset edit when empty; Mod+↑ in an empty field
// always edits.
func TestUpArrowShortcut(t *testing.T) {
	for _, c := range []struct {
		mode     int
		browse   bool
		text     string
		caret0   bool
		mods     key.Modifiers
		wantList bool
		wantEdit bool
	}{
		{mode: 0, text: "", wantEdit: true},
		{mode: 0, browse: true, text: "", wantList: true},
		{mode: 0, browse: true, text: "hi", caret0: true, wantList: true},
		{mode: 1, text: "hi", caret0: true, wantList: true},
		{mode: 1, text: "hi"},
		{mode: 1, text: "", wantList: true},
		{mode: 2, text: "hi", caret0: true},
		{mode: 2, text: "", wantList: true},
		{mode: 3, text: "", wantEdit: true},
		{mode: 3, text: "hi", caret0: true},
		{mode: 2, text: "", mods: key.ModShortcut, wantEdit: true},
	} {
		e := NewEditor()
		e.upMode, e.upBrowse = c.mode, c.browse
		edType(e, c.text)
		if c.caret0 {
			e.SetSelection(0, 0)
		}
		e.events = nil
		edPress(e, key.NameUpArrow, c.mods)
		if got := edHas(e, EvFocusList); got != c.wantList {
			t.Errorf("%+v: focus list %v", c, got)
		}
		if got := edHas(e, EvEditLast); got != c.wantEdit {
			t.Errorf("%+v: edit last %v", c, got)
		}
	}
}

// Mod+Z in an empty composer asks to unsend (onCmdZWhenEmpty); with text
// it undoes.
func TestUndoEmptyUnsends(t *testing.T) {
	e := NewEditor()
	edPress(e, "Z", key.ModShortcut)
	if !edHas(e, EvUndoEmpty) {
		t.Fatal("no EvUndoEmpty in an empty editor")
	}
	e = NewEditor()
	edType(e, "ab")
	edPress(e, "Z", key.ModShortcut)
	if edHas(e, EvUndoEmpty) || e.doc.Text() == "ab" {
		t.Fatalf("Mod+Z with text: %q", e.doc.Text())
	}
}
