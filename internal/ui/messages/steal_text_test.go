package messages

import (
	"testing"

	"gioui.org/io/key"
)

func TestStealText(t *testing.T) {
	letter := func(s string) bool { return s != " " && len([]rune(s)) == 1 }
	var st StealText
	// the frame's first key decides; the rest is the input's, Space too
	st.Edit(key.EditEvent{Text: " "}, letter)
	if st.Started() {
		t.Fatal("Space started a steal")
	}
	st.Edit(key.EditEvent{Text: "h"}, letter)
	st.Edit(key.EditEvent{Text: " "}, letter)
	st.Edit(key.EditEvent{Text: "i"}, letter)
	if text, keys := st.Take(); text != "h i" || len(keys) != 0 {
		t.Errorf("the frame's text %q (keys %v), want %q", text, keys, "h i")
	}
	if text, _ := st.Take(); text != "" || st.Started() {
		t.Errorf("Take didn't start a new frame: %q", text)
	}

	// marked text stays out, across frames, and the commit goes in
	st.Edit(key.EditEvent{Text: "k"}, letter)
	st.Composition(key.CompositionEvent{Start: 0, End: 1})
	if text, _ := st.Take(); text != "" {
		t.Errorf("marked text taken: %q", text)
	}
	st.Edit(key.EditEvent{Range: key.Range{Start: 0, End: 1}, Text: "かな"}, letter) // two runes: a commit
	st.Composition(key.CompositionEvent{Start: -1, End: -1})
	if text, _ := st.Take(); text != "かな" {
		t.Errorf("the committed text %q, want かな", text)
	}

	// a dead key, then its letter
	st.Edit(key.EditEvent{Text: "´"}, letter)
	st.Composition(key.CompositionEvent{Start: 0, End: 1})
	st.Edit(key.EditEvent{Range: key.Range{Start: 0, End: 1}, Text: "é"}, letter)
	st.Composition(key.CompositionEvent{Start: -1, End: -1})
	if text, _ := st.Take(); text != "é" {
		t.Errorf("a dead key and its letter: %q, want é", text)
	}

	// shortcuts, and the text after them
	st.Key(key.Event{Name: "V", Modifiers: key.ModShortcut, State: key.Press})
	st.Edit(key.EditEvent{Text: " "}, letter)
	if text, keys := st.Take(); text != " " || len(keys) != 1 || keys[0].Name != "V" {
		t.Errorf("Mod+V then Space: %q %v", text, keys)
	}
}
