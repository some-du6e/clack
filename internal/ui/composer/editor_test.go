package composer

import (
	"testing"

	"gioui.org/io/key"
	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

// These tests drive the editor's logic without a window: a zero
// layout.Context has a disabled input source, so commands that talk to the
// system (clipboard, focus) are no-ops, and motions fall back to logical
// lines because no Layout has run.

func edType(e *Editor, s string) {
	for _, r := range s {
		e.typeAt(e.caret, e.caret, string(r))
	}
}

func edPress(e *Editor, name key.Name, mods key.Modifiers) {
	e.command(layout.Context{}, key.Event{Name: name, Modifiers: mods, State: key.Press})
}

func edHas(e *Editor, k EditorEventKind) bool {
	for _, ev := range e.events {
		if ev.Kind == k {
			return true
		}
	}
	return false
}

func edStyles(d *Doc) []Style {
	out := make([]Style, len(d.Attrs))
	for i, a := range d.Attrs {
		out[i] = a.Style
	}
	return out
}

func TestDocInsertSplitsLines(t *testing.T) {
	d := NewDoc()
	d.Insert(0, "ab", Attr{})
	d.Lines[0] = Line{Kind: Bullet, Indent: 1}
	d.Insert(1, "\n", Attr{})
	if got := d.Text(); got != "a\nb" {
		t.Fatalf("text %q", got)
	}
	if len(d.Lines) != 2 || d.Lines[1] != (Line{Kind: Bullet, Indent: 1}) {
		t.Fatalf("lines %+v", d.Lines)
	}
	d.Lines[1].Kind = Quote
	d.Delete(1, 2) // join keeps the first line's format
	if len(d.Lines) != 1 || d.Lines[0].Kind != Bullet {
		t.Fatalf("after join %+v", d.Lines)
	}
}

func TestDocSliceInsertDocRoundTrip(t *testing.T) {
	d := NewDoc()
	d.Insert(0, "one\ntwo", Attr{Style: Bold})
	d.Lines[1].Kind = Quote
	sl := d.Slice(2, 6) // "e\ntw"
	if len(sl.Lines) != 2 || sl.Lines[1].Kind != Quote {
		t.Fatalf("slice lines %+v", sl.Lines)
	}
	dst := NewDoc()
	dst.Insert(0, "xy", Attr{})
	end := dst.InsertDoc(1, sl)
	if end != 5 || dst.Text() != "xe\ntwy" {
		t.Fatalf("insertdoc end %d text %q", end, dst.Text())
	}
	if len(dst.Lines) != 2 || dst.Lines[1].Kind != Quote {
		t.Fatalf("lines %+v", dst.Lines)
	}
	if !dst.HasStyle(1, 5, Bold) || dst.HasStyle(0, 1, Bold) {
		t.Fatalf("styles %v", edStyles(dst))
	}
}

func TestAutoformatInline(t *testing.T) {
	cases := []struct {
		in    string
		out   string
		style Style
		ok    bool
	}{
		{"*bold*", "bold", Bold, true},
		{"say _hi_", "say hi", Italic, true},
		{"~gone~", "gone", Strike, true},
		{"run `ls -l`", "run ls -l", Code, true},
		{"(*x*", "(x", Bold, true},
		{"snake_case_", "snake_case_", 0, false}, // opening marker inside a word
		{"* x*", "* x*", 0, false},               // inner starts with a space
		{"*x *", "*x *", 0, false},               // inner ends with a space
		{"**", "**", 0, false},                   // empty
		{"a*b*", "a*b*", 0, false},
	}
	for _, c := range cases {
		d := NewDoc()
		end := d.Insert(0, c.in, Attr{})
		nc, st, ok := autoformatInline(d, end)
		if ok != c.ok || d.Text() != c.out {
			t.Errorf("%q: ok=%v text=%q", c.in, ok, d.Text())
			continue
		}
		if !ok {
			continue
		}
		if st != c.style || nc != d.Len() {
			t.Errorf("%q: style %v caret %d", c.in, st, nc)
		}
	}
	// Styles land on the inner text only.
	d := NewDoc()
	end := d.Insert(0, "a *bc*", Attr{})
	if _, _, ok := autoformatInline(d, end); !ok {
		t.Fatal("no autoformat")
	}
	if d.HasStyle(0, 2, Bold) || !d.HasStyle(2, 4, Bold) {
		t.Fatalf("styles %v", edStyles(d))
	}
	// Not inside code blocks.
	d = NewDoc()
	end = d.Insert(0, "*x*", Attr{})
	d.Lines[0].Kind = CodeBlock
	if _, _, ok := autoformatInline(d, end); ok {
		t.Fatal("autoformat inside a code block")
	}
}

func TestAutoformatBlock(t *testing.T) {
	cases := []struct {
		in   string
		kind BlockKind
		ok   bool
	}{
		// list autofill's prefix (Slack): 1. and 1) only, after any
		// spaces; ">" quotes as it is typed (autoformatQuote)
		{"> ", Para, false},
		{"- ", Bullet, true},
		{"* ", Bullet, true},
		{"• ", Bullet, true},
		{"  - ", Bullet, true},
		{"1. ", Ordered, true},
		{"1) ", Ordered, true},
		{"12. ", Para, false},
		{"2. ", Para, false},
		{"```", CodeBlock, true},
		{"x ", Para, false},
		{"1.", Para, false},
		{"a. ", Para, false},
	}
	for _, c := range cases {
		d := NewDoc()
		end := d.Insert(0, c.in, Attr{})
		nc, k, ok := autoformatBlock(d, end)
		if ok != c.ok || k != c.kind {
			t.Errorf("%q: ok=%v kind=%v", c.in, ok, k)
			continue
		}
		if ok && (nc != 0 || d.Len() != 0 || d.Lines[0].Kind != c.kind) {
			t.Errorf("%q: caret %d len %d lines %+v", c.in, nc, d.Len(), d.Lines)
		}
	}
	// Only on paragraphs, only at the line start.
	d := NewDoc()
	end := d.Insert(0, "a\n- ", Attr{})
	if _, k, ok := autoformatBlock(d, end); !ok || k != Bullet || d.Text() != "a\n" || d.Lines[1].Kind != Bullet || d.Lines[0].Kind != Para {
		t.Fatalf("second line: ok=%v text=%q lines=%+v", ok, d.Text(), d.Lines)
	}
	d = NewDoc()
	end = d.Insert(0, "- ", Attr{})
	d.Lines[0].Kind = Quote
	if _, _, ok := autoformatBlock(d, end); ok {
		t.Fatal("autoformat on a quote line")
	}
}

func TestTypingAutoformatAndUndo(t *testing.T) {
	e := NewEditor()
	edType(e, "*bold*")
	if e.doc.Text() != "bold" || !e.doc.HasStyle(0, 4, Bold) {
		t.Fatalf("text %q styles %v", e.doc.Text(), edStyles(e.doc))
	}
	if e.ActiveStyle()&Bold != 0 {
		t.Fatal("bold should not continue after the closing marker")
	}
	edType(e, "x")
	if e.doc.Attrs[4].Style&Bold != 0 {
		t.Fatal("text typed after autoformat is bold")
	}
	e.Undo() // the x
	if e.doc.Text() != "bold" {
		t.Fatalf("after undo x: %q", e.doc.Text())
	}
	e.Undo() // the autoformat: literal markers come back
	if e.doc.Text() != "*bold*" || e.doc.HasStyle(0, 6, Bold) {
		t.Fatalf("after undo autoformat: %q %v", e.doc.Text(), edStyles(e.doc))
	}
	e.Undo()
	if e.doc.Text() != "" {
		t.Fatalf("after undo typing: %q", e.doc.Text())
	}
	e.Redo()
	if e.doc.Text() != "*bold*" {
		t.Fatalf("redo: %q", e.doc.Text())
	}

	e = NewEditor()
	edType(e, "- item")
	if e.doc.Text() != "item" || e.doc.Lines[0].Kind != Bullet {
		t.Fatalf("bullet: %q %+v", e.doc.Text(), e.doc.Lines)
	}
	e = NewEditor()
	edType(e, "```")
	if e.doc.Len() != 0 || e.doc.Lines[0].Kind != CodeBlock {
		t.Fatalf("code block: %q %+v", e.doc.Text(), e.doc.Lines)
	}
	edType(e, "*x*")
	if e.doc.Text() != "*x*" {
		t.Fatalf("markdown applied in code block: %q", e.doc.Text())
	}
	e = NewEditor()
	edType(e, "snake_case_var")
	if e.doc.Text() != "snake_case_var" || e.doc.HasStyle(0, e.doc.Len(), Italic) {
		t.Fatalf("snake case: %q", e.doc.Text())
	}
}

func TestUndoCoalescing(t *testing.T) {
	e := NewEditor()
	edType(e, "hello world")
	e.Undo()
	if got := e.doc.Text(); got != "hello " {
		t.Fatalf("first undo: %q", got)
	}
	e.Undo()
	if got := e.doc.Text(); got != "" {
		t.Fatalf("second undo: %q", got)
	}
	e.Redo()
	e.Redo()
	if got := e.doc.Text(); got != "hello world" {
		t.Fatalf("redo: %q", got)
	}

	// A caret jump breaks the group.
	e = NewEditor()
	edType(e, "ab")
	e.SetSelection(0, 0)
	edType(e, "x")
	e.Undo()
	if got := e.doc.Text(); got != "ab" {
		t.Fatalf("after jump undo: %q", got)
	}

	// Consecutive backspaces are one step.
	e = NewEditor()
	edType(e, "abc")
	for i := 0; i < 3; i++ {
		edPress(e, key.NameDeleteBackward, 0)
	}
	if e.doc.Len() != 0 {
		t.Fatalf("not deleted: %q", e.doc.Text())
	}
	e.Undo()
	if got := e.doc.Text(); got != "abc" {
		t.Fatalf("undo deletes: %q", got)
	}

	// The history is capped.
	e = NewEditor()
	for i := 0; i < edUndoCap+50; i++ {
		e.InsertText("x")
	}
	if len(e.undo) != edUndoCap {
		t.Fatalf("undo len %d", len(e.undo))
	}
}

// Enter in lists, quotes and code, as Slack's composer takes it (Texty's
// keyboard module: Quill's "list empty enter" binding first, then Slack's
// enter bindings, on_enter_onEnter with shouldInsertNewline, xrjr, and
// MessageInput.onEnter): Enter in a list item with text sends, Enter on an
// empty item leaves the list without sending, Shift+Enter makes the next
// item and on an empty one leaves the list, indent and all. Enter on an
// empty quote line sends; Shift+Enter adds quote (or code) lines until
// the second empty one in a row, where the two become a plain line.
func TestEnterInLists(t *testing.T) {
	e := NewEditor()
	edType(e, "- a")
	e.events = nil
	edPress(e, key.NameReturn, 0)
	if !edHas(e, EvSubmit) || e.doc.Text() != "a" || len(e.doc.Lines) != 1 {
		t.Fatalf("Enter in a list item: submit %v, %q %+v", edHas(e, EvSubmit), e.doc.Text(), e.doc.Lines)
	}
	e.events = nil
	edPress(e, key.NameReturn, key.ModShift)
	if edHas(e, EvSubmit) || e.doc.Text() != "a\n" || len(e.doc.Lines) != 2 || e.doc.Lines[1].Kind != Bullet {
		t.Fatalf("Shift+Enter, the next item: %q %+v", e.doc.Text(), e.doc.Lines)
	}
	edPress(e, key.NameReturn, 0) // empty item: leave the list, no send
	if edHas(e, EvSubmit) || e.doc.Text() != "a\n" || e.doc.Lines[1].Kind != Para || e.doc.Lines[0].Kind != Bullet {
		t.Fatalf("Enter on an empty item: %q %+v %v", e.doc.Text(), e.doc.Lines, e.events)
	}
	edType(e, "b")
	edPress(e, key.NameReturn, 0)
	if !edHas(e, EvSubmit) {
		t.Fatal("Enter in a paragraph did not submit")
	}

	// a nested empty item leaves the list at once: "list empty enter"
	// formats list and indent off, the newline branch formatLine({list:
	// false, indent: 0})
	for _, mods := range []key.Modifiers{0, key.ModShift} {
		e = NewEditor()
		edType(e, "- a")
		e.SetSelection(0, 0) // Slack's "indent" takes a collapsed caret at the item's start
		edPress(e, key.NameTab, 0)
		if e.doc.Lines[0].Indent != 1 {
			t.Fatalf("tab indent %+v", e.doc.Lines)
		}
		e.SetSelection(e.doc.Len(), e.doc.Len())
		edPress(e, key.NameReturn, key.ModShift)
		if l := e.doc.Lines[1]; l.Kind != Bullet || l.Indent != 1 {
			t.Fatalf("the next nested item %+v", e.doc.Lines)
		}
		e.events = nil
		edPress(e, key.NameReturn, mods)
		if l := e.doc.Lines[1]; l.Kind != Para || l.Indent != 0 || edHas(e, EvSubmit) {
			t.Fatalf("mods %v on the empty nested item: %+v %v", mods, e.doc.Lines, e.events)
		}
	}

	// Shift+Enter in a code block adds code lines and leaves on the second
	// empty one; Enter submits.
	e = NewEditor()
	edType(e, "```x")
	e.events = nil
	edPress(e, key.NameReturn, key.ModShift)
	if len(e.doc.Lines) != 2 || e.doc.Lines[1].Kind != CodeBlock {
		t.Fatalf("code line %+v", e.doc.Lines)
	}
	edPress(e, key.NameReturn, key.ModShift)
	if len(e.doc.Lines) != 3 || e.doc.Lines[2].Kind != CodeBlock {
		t.Fatalf("second empty code line %+v", e.doc.Lines)
	}
	edPress(e, key.NameReturn, key.ModShift)
	if e.doc.Text() != "x\n" || len(e.doc.Lines) != 2 || e.doc.Lines[0].Kind != CodeBlock || e.doc.Lines[1].Kind != Para {
		t.Fatalf("leaving the code block: %q %+v", e.doc.Text(), e.doc.Lines)
	}
	e = NewEditor()
	edType(e, "```x")
	edPress(e, key.NameReturn, 0)
	if !edHas(e, EvSubmit) {
		t.Fatal("Enter in a code block did not submit")
	}

	// quotes: Enter sends, an empty quote line included
	e = NewEditor()
	edType(e, ">q")
	edPress(e, key.NameReturn, key.ModShift)
	e.events = nil
	edPress(e, key.NameReturn, 0)
	if !edHas(e, EvSubmit) || len(e.doc.Lines) != 2 || e.doc.Lines[1].Kind != Quote {
		t.Fatalf("Enter on an empty quote line: %+v %v", e.doc.Lines, e.events)
	}
	edPress(e, key.NameReturn, key.ModShift) // a second empty quote line
	if len(e.doc.Lines) != 3 || e.doc.Lines[2].Kind != Quote {
		t.Fatalf("second empty quote line %+v", e.doc.Lines)
	}
	edPress(e, key.NameReturn, key.ModShift)
	if e.doc.Text() != "q\n" || len(e.doc.Lines) != 2 || e.doc.Lines[0].Kind != Quote || e.doc.Lines[1].Kind != Para {
		t.Fatalf("leaving the quote: %q %+v", e.doc.Text(), e.doc.Lines)
	}

	// Slack's shouldInsertNewline table (xrjr): meta never, Ctrl on macOS
	// always, Ctrl elsewhere unless Shift or "Start a new line"; with that
	// pref anything; in code under enter_is_special_in_tbt all but Shift;
	// else Shift or Alt
	cases := []struct {
		meta, ctrl, shift, alt, mac, pref, pre, in, want bool
	}{
		{want: false},
		{shift: true, want: true},
		{alt: true, want: true},
		{meta: true, shift: true, want: false},
		{ctrl: true, want: true},
		{ctrl: true, shift: true, want: false},
		{ctrl: true, pref: true, want: false},
		{ctrl: true, pref: true, mac: true, want: true},
		{pref: true, want: true},
		{pre: true, in: true, want: true},
		{pre: true, in: true, shift: true, want: false},
		{pre: true, want: false},
	}
	for _, c := range cases {
		if got := insertsNewline(c.meta, c.ctrl, c.shift, c.alt, c.mac, c.pref, c.pre, c.in); got != c.want {
			t.Errorf("insertsNewline%+v = %v", c, got)
		}
	}
}

func TestBackspaceRemovesBlockFirst(t *testing.T) {
	e := NewEditor()
	edType(e, "x\n>a")
	if e.doc.Lines[1].Kind != Quote {
		t.Fatalf("lines %+v", e.doc.Lines)
	}
	edPress(e, key.NameHome, 0)
	edPress(e, key.NameDeleteBackward, 0)
	if e.doc.Text() != "x\na" || e.doc.Lines[1].Kind != Para {
		t.Fatalf("first backspace: %q %+v", e.doc.Text(), e.doc.Lines)
	}
	edPress(e, key.NameDeleteBackward, 0)
	if e.doc.Text() != "xa" || len(e.doc.Lines) != 1 {
		t.Fatalf("second backspace: %q %+v", e.doc.Text(), e.doc.Lines)
	}
}

func TestAtomsDeleteAsUnit(t *testing.T) {
	e := NewEditor()
	edType(e, "hi @ka")
	e.InsertAtom(3, 6, &Atom{Kind: AtomUser, ID: "U1", Text: "@karim"})
	if e.doc.Text() != "hi @karim " || e.caret != 5 {
		t.Fatalf("insert atom: %q caret %d", e.doc.Text(), e.caret)
	}
	edPress(e, key.NameDeleteBackward, 0) // the trailing space
	edPress(e, key.NameDeleteBackward, 0) // the whole atom
	if e.doc.Text() != "hi " {
		t.Fatalf("after deletes: %q", e.doc.Text())
	}
	// Word motion treats an atom as one word.
	d := NewDoc()
	p := d.Insert(0, "a ", Attr{})
	p = d.InsertAtom(p, &Atom{Kind: AtomEmoji, ID: "smile", Text: ":smile:"})
	d.Insert(p, "b", Attr{})
	if got := edWordLeft(d, 3); got != 2 {
		t.Fatalf("word left over atom %d", got)
	}
	if got := edWordRight(d, 2); got != 3 {
		t.Fatalf("word right over atom %d", got)
	}
}

func TestWordMotion(t *testing.T) {
	d := NewDoc()
	d.Insert(0, "foo bar.baz\nqux", Attr{})
	right := []int{3, 7, 8, 11, 12, 15}
	p := 0
	for _, want := range right {
		p = edWordRight(d, p)
		if p != want {
			t.Fatalf("word right: got %d want %d", p, want)
		}
	}
	left := []int{12, 11, 8, 7, 4, 0}
	for _, want := range left {
		p = edWordLeft(d, p)
		if p != want {
			t.Fatalf("word left: got %d want %d", p, want)
		}
	}
	if lo, hi := edWordAt(d, 5); lo != 4 || hi != 7 {
		t.Fatalf("word at: %d %d", lo, hi)
	}
	if lo, hi := edWordAt(d, 3); lo != 0 || hi != 3 {
		t.Fatalf("word at end of word: %d %d", lo, hi)
	}

	e := NewEditor()
	edType(e, "foo bar")
	edPress(e, key.NameDeleteBackward, key.ModShortcutAlt)
	if e.doc.Text() != "foo " {
		t.Fatalf("ctrl+backspace: %q", e.doc.Text())
	}
	edPress(e, key.NameLeftArrow, key.ModShortcutAlt)
	if e.caret != 0 {
		t.Fatalf("ctrl+left: %d", e.caret)
	}
	edPress(e, key.NameRightArrow, key.ModShortcutAlt|key.ModShift)
	if a, c := e.Selection(); a != 0 || c != 3 {
		t.Fatalf("ctrl+shift+right: %d %d", a, c)
	}
}

func TestGraphemeStops(t *testing.T) {
	e := NewEditor()
	edType(e, "aé")
	edPress(e, key.NameLeftArrow, 0)
	if e.caret != 1 {
		t.Fatalf("left over combining mark: %d", e.caret)
	}
	edPress(e, key.NameRightArrow, 0)
	edPress(e, key.NameDeleteBackward, 0)
	if e.doc.Text() != "a" {
		t.Fatalf("backspace grapheme: %q", e.doc.Text())
	}
	d := NewDoc()
	d.Insert(0, "\U0001F1EB\U0001F1F7\U0001F1E9\U0001F1EA", Attr{}) // two flags
	if got := edNextStop(d, 0); got != 2 {
		t.Fatalf("flag stop %d", got)
	}
	if got := edPrevStop(d, 4); got != 2 {
		t.Fatalf("flag prev stop %d", got)
	}
}

func TestPendingStyleAndShortcuts(t *testing.T) {
	e := NewEditor()
	edPress(e, "B", key.ModShortcut)
	if e.ActiveStyle()&Bold == 0 {
		t.Fatal("pending bold not reported")
	}
	edType(e, "ab")
	if !e.doc.HasStyle(0, 2, Bold) {
		t.Fatalf("typed text not bold: %v", edStyles(e.doc))
	}
	edPress(e, "B", key.ModShortcut)
	edType(e, "c")
	if e.doc.Attrs[2].Style&Bold != 0 {
		t.Fatal("bold not toggled off")
	}
	// Moving the caret drops the pending style.
	edPress(e, "I", key.ModShortcut)
	edPress(e, key.NameLeftArrow, 0)
	edPress(e, key.NameRightArrow, 0)
	if e.ActiveStyle()&Italic != 0 {
		t.Fatal("pending style survived a caret move")
	}

	// With a selection, styles toggle on it.
	edPress(e, "A", key.ModShortcut)
	edPress(e, "X", key.ModShortcut|key.ModShift)
	if !e.doc.HasStyle(0, 3, Strike) || e.ActiveStyle()&Strike == 0 {
		t.Fatal("strike on selection")
	}
	edPress(e, "C", key.ModShortcut|key.ModShift)
	if !e.doc.HasStyle(0, 3, Code) {
		t.Fatal("code on selection")
	}

	// Block shortcuts: X11 names ("&") and others ("8"+Shift).
	edPress(e, "&", key.ModShortcut|key.ModShift)
	if e.ActiveBlock() != Ordered {
		t.Fatalf("ordered: %v", e.ActiveBlock())
	}
	edPress(e, "8", key.ModShortcut|key.ModShift)
	if e.ActiveBlock() != Bullet {
		t.Fatalf("bullet: %v", e.ActiveBlock())
	}
	edPress(e, "(", key.ModShortcut|key.ModShift)
	if e.ActiveBlock() != Quote {
		t.Fatalf("quote: %v", e.ActiveBlock())
	}
	edPress(e, "C", key.ModShortcut|key.ModShift|key.ModAlt)
	if e.ActiveBlock() != CodeBlock {
		t.Fatalf("code block: %v", e.ActiveBlock())
	}
	e.events = nil
	edPress(e, "U", key.ModShortcut|key.ModShift)
	if !edHas(e, EvLinkDialog) {
		t.Fatal("link dialog")
	}
	edPress(e, "Z", key.ModShortcut)
	if e.ActiveBlock() != Quote {
		t.Fatalf("undo block: %v", e.ActiveBlock())
	}
	edPress(e, "Y", key.ModShortcut)
	if e.ActiveBlock() != CodeBlock {
		t.Fatalf("redo block: %v", e.ActiveBlock())
	}
}

func TestEventsAndIntercept(t *testing.T) {
	e := NewEditor()
	edPress(e, key.NameUpArrow, 0)
	if !edHas(e, EvEditLast) {
		t.Fatal("Up in an empty editor")
	}
	e.events = nil
	edPress(e, key.NameEscape, 0)
	if !edHas(e, EvCancel) {
		t.Fatal("Escape")
	}
	var got []key.Name
	e.Intercept = func(n key.Name, m key.Modifiers) bool { got = append(got, n); return n == key.NameReturn }
	e.events = nil
	edPress(e, key.NameReturn, 0)
	edPress(e, "B", key.ModShortcut)
	if edHas(e, EvSubmit) || len(got) != 1 {
		t.Fatalf("intercept: %v %v", e.events, got)
	}
	edType(e, "ab")
	if n := len(e.events); n != 1 || e.events[0].Kind != EvChange {
		t.Fatalf("EvChange not coalesced: %v", e.events)
	}
	v := e.Version()
	e.ToggleStyle(Italic) // pending only: no content change
	if e.Version() != v {
		t.Fatal("pending toggle changed the version")
	}
	if ev, ok := e.pop(); !ok || ev.Kind != EvChange {
		t.Fatal("pop")
	}
}

func TestCopyPasteKeepsFormatting(t *testing.T) {
	gtx := layout.Context{}
	e := NewEditor()
	edType(e, "hi")
	e.SetSelection(0, 2)
	e.ToggleStyle(Bold)
	if !e.copySel(gtx) || e.clipText != "hi" {
		t.Fatal("copy")
	}
	e.SetSelection(0, 0)
	e.paste("hi")
	if e.doc.Text() != "hihi" || !e.doc.HasStyle(0, 4, Bold) {
		t.Fatalf("rich paste: %q %v", e.doc.Text(), edStyles(e.doc))
	}
	e.SetSelection(0, 0)
	e.paste("zz") // foreign text: plain
	if e.doc.Text() != "zzhihi" || e.doc.HasStyle(0, 2, Bold) {
		t.Fatalf("plain paste: %q %v", e.doc.Text(), edStyles(e.doc))
	}

	// A URL pasted over a selection links it.
	e.SetSelection(2, 4)
	e.paste("https://example.com/x")
	if e.doc.Text() != "zzhihi" {
		t.Fatalf("url paste replaced text: %q", e.doc.Text())
	}
	if s, en, u := e.doc.LinkAt(3); s != 2 || en != 4 || u != "https://example.com/x" {
		t.Fatalf("link %d %d %q", s, en, u)
	}

	// Cut.
	e.SetSelection(0, 2)
	edPress(e, "X", key.ModShortcut)
	if e.doc.Text() != "hihi" || e.clipText != "zz" {
		t.Fatalf("cut: %q clip %q", e.doc.Text(), e.clipText)
	}
}

func TestSetLink(t *testing.T) {
	e := NewEditor()
	edType(e, "hello there")
	e.SetSelection(0, 5)
	e.SetLink("hello", "https://a.example")
	if _, _, u := e.doc.LinkAt(2); u != "https://a.example" || e.doc.Text() != "hello there" {
		t.Fatalf("link selection: %q %q", u, e.doc.Text())
	}
	if !e.ActiveLink() {
		t.Fatal("active link")
	}
	// Collapsed caret inside the link edits it.
	e.SetSelection(2, 2)
	e.SetLink("hey", "https://b.example")
	if e.doc.Text() != "hey there" {
		t.Fatalf("edit link: %q", e.doc.Text())
	}
	if s, en, u := e.doc.LinkAt(1); s != 0 || en != 3 || u != "https://b.example" {
		t.Fatalf("edited link %d %d %q", s, en, u)
	}
	// Typing right after a link doesn't extend it.
	edType(e, "!")
	if e.doc.Attrs[3].Link != "" {
		t.Fatal("link extended")
	}
}

func TestIMEEditsFollowRealSelection(t *testing.T) {
	// The window computes edit ranges from its own model; after the
	// autoformat removes the markers our caret is two runes earlier.
	e := NewEditor()
	for i, r := range "*b*x" {
		e.onEdit(key.EditEvent{Range: key.Range{Start: i, End: i}, Text: string(r)})
	}
	if e.doc.Text() != "bx" || !e.doc.HasStyle(0, 1, Bold) || e.doc.HasStyle(1, 2, Bold) {
		t.Fatalf("text %q styles %v", e.doc.Text(), edStyles(e.doc))
	}
	// An explicit range (composition replacement) is honoured.
	e.onEdit(key.EditEvent{Range: key.Range{Start: 0, End: 1}, Text: "B"})
	if e.doc.Text() != "Bx" {
		t.Fatalf("replace: %q", e.doc.Text())
	}
}

func TestListNumbers(t *testing.T) {
	cases := []struct {
		n, indent int
		want      string
	}{
		{1, 0, "1."}, {12, 0, "12."}, {2, 1, "b."}, {27, 1, "aa."}, {4, 2, "iv."}, {3, 3, "3."},
	}
	for _, c := range cases {
		if got := tlListNumber(c.n, c.indent); got != c.want {
			t.Errorf("%d/%d: %q", c.n, c.indent, got)
		}
	}
	l := &tlLayout{paras: []tlPara{
		{kind: Ordered}, {kind: Ordered}, {kind: Bullet, indent: 1}, {kind: Ordered}, {kind: Para}, {kind: Ordered},
	}}
	l.numberLists()
	want := []int{1, 2, 0, 3, 0, 1}
	for i, p := range l.paras {
		if p.num != want[i] {
			t.Fatalf("para %d num %d want %d", i, p.num, want[i])
		}
	}
}

// Advanced > "When typing code with ```, Enter should not send the
// message" (enter_is_special_in_tbt): Enter adds a line, Shift+Enter sends.
func TestEnterIsSpecialInCode(t *testing.T) {
	t.Cleanup(uipref.Reset)
	uipref.Set(store.Prefs{EnterIsSpecial: true})
	e := NewEditor()
	edType(e, "```x")
	e.events = nil
	edPress(e, key.NameReturn, 0)
	if edHas(e, EvSubmit) || len(e.doc.Lines) != 2 || e.doc.Lines[1].Kind != CodeBlock {
		t.Fatalf("Enter in code: submit=%v lines %+v", edHas(e, EvSubmit), e.doc.Lines)
	}
	edPress(e, key.NameReturn, key.ModShift)
	if !edHas(e, EvSubmit) {
		t.Fatal("Shift+Enter in code didn't send")
	}
	// outside code Enter still sends
	e = NewEditor()
	edType(e, "hi")
	edPress(e, key.NameReturn, 0)
	if !edHas(e, EvSubmit) {
		t.Fatal("Enter in a paragraph didn't send")
	}
}

// Advanced > "When writing a message, press Enter to… Start a new line"
// (msg_input_send_btn): Enter adds a line, Mod+Enter sends.
func TestSendOnModEnter(t *testing.T) {
	t.Cleanup(uipref.Reset)
	uipref.Set(store.Prefs{SendOnModEnter: true})
	e := NewEditor()
	edType(e, "hi")
	e.events = nil
	edPress(e, key.NameReturn, 0)
	if edHas(e, EvSubmit) || len(e.doc.Lines) != 2 {
		t.Fatalf("Enter: submit=%v lines %d", edHas(e, EvSubmit), len(e.doc.Lines))
	}
	edPress(e, key.NameReturn, key.ModShortcut)
	if !edHas(e, EvSubmit) {
		t.Fatal("Mod+Enter didn't send")
	}
	uipref.Set(store.Prefs{})
	e = NewEditor()
	edType(e, "hi")
	edPress(e, key.NameReturn, 0)
	if !edHas(e, EvSubmit) {
		t.Fatal("Enter didn't send with the pref off")
	}
}
