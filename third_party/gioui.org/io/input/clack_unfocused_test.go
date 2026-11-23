package input

// Clack patch 64: typed text with no handler focused goes to the handler
// SetUnfocusedText named; the focus stays empty.

import (
	"testing"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/op"
)

// unfocusedRouter is a router whose last frame had body and ed read their
// focus events (ed an editor's handler).
func unfocusedRouter(body, ed *int) *Router {
	r := new(Router)
	events(r, -1, key.FocusFilter{Target: body})
	events(r, -1, key.FocusFilter{Target: ed})
	var ops op.Ops
	event.Op(&ops, body)
	event.Op(&ops, ed)
	r.Frame(&ops)
	return r
}

func editTexts(evs []event.Event) []string {
	var out []string
	for _, e := range evs {
		if ee, ok := e.(key.EditEvent); ok {
			out = append(out, ee.Text)
		}
	}
	return out
}

func TestClackUnfocusedText(t *testing.T) {
	var body, ed int
	r := unfocusedRouter(&body, &ed)
	r.Queue(key.EditEvent{Text: "a"})
	if got := editTexts(events(r, -1, key.FocusFilter{Target: &body})); len(got) != 0 {
		t.Errorf("with no handler named, the unfocused text went to %v; Gio drops it", got)
	}

	r.Source().SetUnfocusedText(&body)
	r.Queue(key.EditEvent{Text: "b"})
	if got := editTexts(events(r, -1, key.FocusFilter{Target: &body})); len(got) != 1 || got[0] != "b" {
		t.Errorf("the named handler read %v, want [b]", got)
	}
	if !r.Source().Focused(nil) {
		t.Error("the unfocused text gave its handler the focus")
	}

	// a focused handler takes the text, as before
	r.Source().Execute(key.FocusCmd{Tag: &ed})
	events(r, -1, key.FocusFilter{Target: &ed})
	r.Queue(key.EditEvent{Text: "c"})
	if got := editTexts(events(r, -1, key.FocusFilter{Target: &ed})); len(got) != 1 || got[0] != "c" {
		t.Errorf("the focused editor read %v, want [c]", got)
	}
	if got := editTexts(events(r, -1, key.FocusFilter{Target: &body})); len(got) != 0 {
		t.Errorf("with an editor focused, the named handler read %v too", got)
	}

	// nil drops it again
	r.Source().Execute(key.FocusCmd{Tag: nil})
	r.Source().SetUnfocusedText(nil)
	r.Queue(key.EditEvent{Text: "d"})
	if got := editTexts(events(r, -1, key.FocusFilter{Target: &body})); len(got) != 0 {
		t.Errorf("after SetUnfocusedText(nil) the text went to %v", got)
	}
}
