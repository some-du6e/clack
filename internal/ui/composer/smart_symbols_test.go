package composer

import "testing"

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
