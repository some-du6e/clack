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

// A handler that takes a key, then drops the focus before reading the
// key's text (a list's E opening an edit: kbLeave's FocusCmd{nil}), has
// had the text too: the command's replay finds it unfocused and drops it,
// as Gio did before the patch, instead of handing it to the named handler
// (where it was typed into the edit that opened, "eHello").
func TestClackUnfocusedTextNotOnReplay(t *testing.T) {
	var body, list int
	r := unfocusedRouter(&body, &list)
	r.Source().SetUnfocusedText(&body)
	r.Source().Execute(key.FocusCmd{Tag: &list})
	events(r, -1, key.FocusFilter{Target: &list}, key.Filter{Focus: &list, Name: "E"})
	// a new frame, whose first read of the list's is the key (as kbKeys
	// is): the command then takes the immediate path and replays the rest
	var ops op.Ops
	event.Op(&ops, &body)
	event.Op(&ops, &list)
	r.Frame(&ops)
	r.Queue(key.Event{Name: "E", State: key.Press}, key.EditEvent{Text: "e"}) // WM_KEYDOWN, then WM_CHAR
	ev, ok := r.Event(key.FocusFilter{Target: &list}, key.Filter{Focus: &list, Name: "E"})
	if ke, isKey := ev.(key.Event); !ok || !isKey || ke.Name != "E" {
		t.Fatalf("the list read %v first, want the E key", ev)
	}
	r.Source().Execute(key.FocusCmd{Tag: nil})
	if got := editTexts(events(r, -1, key.FocusFilter{Target: &body})); len(got) != 0 {
		t.Errorf("the text of a key the list took went to the unfocused text's handler: %v", got)
	}
	if !r.Source().Focused(nil) {
		t.Error("the focus didn't go")
	}

	// text that comes after, with nothing focused, goes there as before
	r.Queue(key.EditEvent{Text: "k"})
	if got := editTexts(events(r, -1, key.FocusFilter{Target: &body})); len(got) != 1 || got[0] != "k" {
		t.Errorf("then typed with nothing focused, the named handler read %v, want [k]", got)
	}
}

// An IME's (or a dead key's) marked text comes with the composition over
// it: with nothing focused both go to the named handler, which can tell
// the marked text from committed text.
func TestClackUnfocusedComposition(t *testing.T) {
	var body, ed int
	r := unfocusedRouter(&body, &ed)
	r.Source().SetUnfocusedText(&body)
	r.Queue(key.EditEvent{Text: "k"}, key.CompositionEvent{Start: 0, End: 1}) // setMarkedText
	var comp []key.CompositionEvent
	for _, e := range events(r, -1, key.FocusFilter{Target: &body}) {
		if c, ok := e.(key.CompositionEvent); ok {
			comp = append(comp, c)
		}
	}
	if len(comp) != 1 || comp[0] != (key.CompositionEvent{Start: 0, End: 1}) {
		t.Errorf("the named handler read the compositions %v, want [{0 1}]", comp)
	}
}
