package richview

import (
	"testing"

	rt "github.com/some-du6e/clack/internal/richtext"
)

// Compact's inline sender (View.SetLead): the first paragraph's first
// line starts at the lead and wraps back to the left edge
// (.p-rich_text_block--inline .p-rich_text_section { display: inline });
// a doc that opens with a block element (here a list) ignores it, and a
// first word too wide for what's left still keeps a full first line.
func TestLeadFirstLine(t *testing.T) {
	st := newTestStyle()
	v := New(doc(para(txt("alpha beta gamma delta epsilon zeta eta theta"))), Options{})
	v.SetLead(120)
	v.relayout(st, 300, 1, 1)
	a := findFrag(t, &v.d, "alpha")
	if a.x != 120 || a.line != 0 {
		t.Errorf("first word at x %v line %d, want 120 on line 0", a.x, a.line)
	}
	if len(v.d.lines) < 2 {
		t.Fatalf("%d lines, want a wrap", len(v.d.lines))
	}
	ln := v.d.lines[1]
	if f := &v.d.frags[ln.f0]; f.x != 0 {
		t.Errorf("second line starts at %v, want 0", f.x)
	}

	l := New(doc(rt.Block{Kind: rt.List, Items: [][]rt.Span{{txt("item")}}}), Options{})
	l.SetLead(120)
	l.relayout(st, 300, 1, 1)
	if f := findFrag(t, &l.d, "item"); f.x >= 120 {
		t.Errorf("a list took the lead: item at %v", f.x)
	}

	w := New(doc(para(txt("averyveryverylongwordthatwillnotfit"))), Options{})
	w.SetLead(290)
	w.relayout(st, 300, 1, 1)
	if len(w.d.lines) < 2 || w.d.lines[0].bot-w.d.lines[0].top != w.d.lines[1].bot-w.d.lines[1].top {
		t.Errorf("the sender's line isn't a full line: %+v", w.d.lines)
	}
	if f := findFrag(t, &w.d, "averyveryverylongwordthatwillnotfit"); f.line != 1 || f.x != 0 {
		t.Errorf("the long word at line %d x %v, want the next line at 0", f.line, f.x)
	}
}
