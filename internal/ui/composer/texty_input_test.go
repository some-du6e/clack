package composer

import (
	"strings"
	"testing"

	"gioui.org/io/key"
)

// A URL pasted over selected text in the topic input replaces it as text:
// Texty's acceptSingleUrl needs useWysiwyg && !usePlainTextMode, and a
// plain Texty input has no link format (LPC/: slackemoji, unlink,
// slackmention). The URL is in the value, so Save sends it.
func TestTextyPasteURLOverSelection(t *testing.T) {
	in := newTestTexty("see docs")
	in.ed.SetSelection(4, 8)
	in.ed.paste("https://example.com/docs")
	if got := in.Text(); got != "see https://example.com/docs" {
		t.Errorf("after the paste the input shows %q", got)
	}
	for i, a := range in.ed.Doc().Attrs {
		if a.Link != "" {
			t.Fatalf("rune %d is a link to %q", i, a.Link)
		}
	}
	if !in.Changed() || in.Value() != "see https://example.com/docs" {
		t.Errorf("value %q (changed %v), want the pasted URL in it", in.Value(), in.Changed())
	}
	if got := in.Submit(); got != "see https://example.com/docs" {
		t.Errorf("Save sends %q", got)
	}
}

// Cmd/Ctrl+Z undoes in an input emptied by the user: useChannelTopicInput
// passes no onCmdZWhenEmpty, so LPC/ binds no empty-Z key and Quill's
// history undoes.
func TestTextyUndoWhenEmpty(t *testing.T) {
	in := newTestTexty("hello there")
	e := in.ed
	edPress(e, "A", key.ModShortcut)
	edPress(e, key.NameDeleteBackward, 0)
	if in.Text() != "" {
		t.Fatalf("select-all + Backspace left %q", in.Text())
	}
	edPress(e, "Z", key.ModShortcut)
	if got := in.Text(); got != "hello there" {
		t.Errorf("Mod+Z in the emptied input: %q, want the text back", got)
	}
	if edHas(e, EvUndoEmpty) {
		t.Error("the input asked to unsend a message")
	}
}

// The text-only keys: formatting keys do nothing, Enter adds a line for
// any modifiers (onTextyEnter returns true), and Tab and Escape are not
// the editor's (they go on to the dialog).
func TestTextyKeys(t *testing.T) {
	in := newTestTexty("ab")
	e := in.ed
	e.SetSelection(0, 2)
	for _, k := range []key.Name{"B", "I", "U"} {
		edPress(e, k, key.ModShortcut)
	}
	edPress(e, "X", key.ModShortcut|key.ModShift)
	edPress(e, "C", key.ModShortcut|key.ModShift)
	for i, a := range e.Doc().Attrs {
		if a.Style != 0 || a.Link != "" {
			t.Errorf("rune %d formatted: %+v", i, a)
		}
	}
	for _, l := range e.Doc().Lines {
		if l.Kind != Para {
			t.Errorf("line kind %v", l.Kind)
		}
	}
	e.SetSelection(2, 2)
	edPress(e, key.NameReturn, 0)
	edPress(e, key.NameReturn, key.ModShortcut)
	if got := in.Text(); got != "ab\n\n" || edHas(e, EvSubmit) {
		t.Errorf("Enter: %q (submit %v), want two new lines", got, edHas(e, EvSubmit))
	}
	for _, f := range e.keyFilters() {
		if kf, ok := f.(key.Filter); ok && (kf.Name == key.NameTab || kf.Name == key.NameEscape) {
			t.Errorf("the input takes %v", kf.Name)
		}
	}
}

// TextyLengthWarning past the input's maxLength (250): Texty counts the
// text as shown (pills by their labels) and only warns; nothing is cut.
func TestTextyOverLimit(t *testing.T) {
	in := newTestTexty(strings.Repeat("a", 249))
	if n := textyOver(in); n != 0 {
		t.Errorf("249 characters: %d over", n)
	}
	edType(in.ed, "bc")
	if n := textyOver(in); n != 1 || len(in.Text()) != 251 {
		t.Errorf("251 characters: %d over, text %d long", n, len(in.Text()))
	}
	// no names: the pill shows @U0FAKE00003 (12)
	in = newTestTexty(strings.Repeat("a", 240) + " <@U0FAKE00003>")
	if n := textyOver(in); n != 3 {
		t.Errorf("240 + space + a 12-character pill: %d over, want 3", n)
	}
}
