package input

// Clack patch 66: a command that takes the immediate path (or defers to
// the next frame) replays everything still queued: the events no handler
// asked for too, and the window's focus moves, not only the events that
// made a handler's events. A press stored for a handler that asks only for
// presses, then its unstored release: replaying the press alone left the
// pointer down on that handler, and once it was gone the next press went
// nowhere (the 11-17 lost click, with a focused editor's SelectionCmd
// instead of patch 61's tick).

import (
	"image"
	"io"
	"strings"
	"testing"

	"gioui.org/f32"
	"gioui.org/io/clipboard"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/io/transfer"
	"gioui.org/op"
	"gioui.org/op/clip"
)

// rsFrame lays out a focusable e (300..400) first, then t over 0..100
// asking for kinds, and ends the frame.
func rsFrame(r *Router, e, t *int, kinds pointer.Kind) {
	var ops op.Ops
	st := clip.Rect(image.Rect(300, 0, 400, 100)).Push(&ops)
	event.Op(&ops, e)
	st.Pop()
	st = clip.Rect(image.Rect(0, 0, 100, 100)).Push(&ops)
	event.Op(&ops, t)
	st.Pop()
	events(r, -1, key.FocusFilter{Target: e})
	events(r, -1, pointer.Filter{Target: t, Kinds: kinds})
	r.Frame(&ops)
}

// rsClick is a press and release at x, then (move) a move to x+10.
func rsClick(x float32, move bool) []event.Event {
	p := f32.Pt(x, 50)
	evs := []event.Event{
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: p},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: p},
	}
	if move {
		evs = append(evs, pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: p.Add(f32.Pt(10, 0))})
	}
	return evs
}

// rsAfter checks the router once the press-only handler h is gone and g
// (press and release) is in its place: no pointer still down, the mouse
// where the trailing move left it, and g's click whole.
func rsAfter(t *testing.T, r *Router, e, g *int) {
	t.Helper()
	rsFrame(r, e, g, pointer.Press|pointer.Release)
	events(r, -1, pointer.Filter{Target: g, Kinds: pointer.Press | pointer.Release})
	rsFrame(r, e, g, pointer.Press|pointer.Release)
	for _, p := range r.lastState().pointers {
		if p.pressed {
			t.Errorf("pointer %d still down after its release (handlers %d)", p.id, len(p.handlers))
		}
	}
	if m := r.pointer.queue.mouse; m.pos != f32.Pt(60, 50) {
		t.Errorf("the mouse at %v, want the trailing move's (60,50)", m.pos)
	}
	r.Queue(rsClick(50, false)...)
	got := pointerKinds(events(r, -1, pointer.Filter{Target: g, Kinds: pointer.Press | pointer.Release}))
	if len(got) != 2 || got[0] != pointer.Press || got[1] != pointer.Release {
		t.Errorf("the next click reached g as %v, want [Press Release]", got)
	}
}

// Every command that takes the immediate path, executed by e in the middle
// of layout while h's press (stored) and its release and a move (unstored:
// h asks only for presses) are queued.
func TestClackCommandReplaysUnstoredEvents(t *testing.T) {
	cmds := []struct {
		name string
		cmd  func(e *int) Command
	}{
		{"SelectionCmd", func(e *int) Command { return key.SelectionCmd{Tag: e, Range: key.Range{Start: 1, End: 1}} }},
		{"SnippetCmd", func(e *int) Command {
			return key.SnippetCmd{Tag: e, Snippet: key.Snippet{Range: key.Range{End: 1}, Text: "x"}}
		}},
		{"FocusCmd", func(e *int) Command { return key.FocusCmd{Tag: e} }},
		{"SoftKeyboardCmd", func(e *int) Command { return key.SoftKeyboardCmd{Show: true} }},
		{"clipboard.ReadCmd", func(e *int) Command { return clipboard.ReadCmd{Tag: e} }},
		{"clipboard.WriteCmd", func(e *int) Command {
			return clipboard.WriteCmd{Type: "application/text", Data: io.NopCloser(strings.NewReader("x"))}
		}},
		{"transfer.OfferCmd", func(e *int) Command {
			return transfer.OfferCmd{Tag: e, Type: "application/text", Data: io.NopCloser(strings.NewReader("x"))}
		}},
		{"pointer.GrabCmd", func(e *int) Command { return pointer.GrabCmd{Tag: e} }},
		{"ResetPointers", func(e *int) Command { return resetPointersCmd{} }},
	}
	for _, c := range cmds {
		t.Run(c.name, func(t *testing.T) {
			var e, h, g int
			r := new(Router)
			rsFrame(r, &e, &h, pointer.Press)
			r.Queue(rsClick(50, true)...)
			r.Source().Execute(c.cmd(&e)) // e, laid out first
			if got := pointerKinds(events(r, -1, pointer.Filter{Target: &h, Kinds: pointer.Press})); len(got) != 1 || got[0] != pointer.Press {
				t.Fatalf("h read %v, want its press", got)
			}
			rsAfter(t, r, &e, &g)
		})
	}
}

// The same with the command deferred to the next frame (e read its events
// first): Frame replays the queued events, unstored ones too.
func TestClackDeferredReplayKeepsUnstoredEvents(t *testing.T) {
	var e, h, g int
	r := new(Router)
	rsFrame(r, &e, &h, pointer.Press)
	r.Queue(rsClick(50, true)...)
	events(r, -1, key.FocusFilter{Target: &e})
	r.Source().Execute(key.FocusCmd{Tag: &e}) // its focus event is one e already read for: deferred
	if !r.deferring {
		t.Fatal("the FocusCmd didn't defer: nothing tested")
	}
	if got := events(r, -1, pointer.Filter{Target: &h, Kinds: pointer.Press}); len(got) != 0 {
		t.Fatalf("h read %v while deferring", got)
	}
	rsFrame(r, &e, &h, pointer.Press) // the replay
	if r.deferredReplayed != 3 || r.replayed != 0 {
		t.Errorf("Frame replayed %d events (the immediate path %d), want the queued 3", r.deferredReplayed, r.replayed)
	}
	if got := pointerKinds(events(r, -1, pointer.Filter{Target: &h, Kinds: pointer.Press})); len(got) != 1 || got[0] != pointer.Press {
		t.Fatalf("h read %v after the replay, want its press", got)
	}
	rsAfter(t, r, &e, &g)
}

// clipboard.WriteCmd changes no input state: like an InvalidateCmd, it
// replays nothing (patch 61).
func TestClackWriteCmdReplaysNothing(t *testing.T) {
	var e, h int
	r := new(Router)
	rsFrame(r, &e, &h, pointer.Press|pointer.Release)
	r.Queue(rsClick(50, false)...)
	r.Source().Execute(clipboard.WriteCmd{Type: "application/text", Data: io.NopCloser(strings.NewReader("x"))})
	if r.replayed != 0 {
		t.Errorf("a WriteCmd replayed %d queued events", r.replayed)
	}
	if got := pointerKinds(events(r, -1, pointer.Filter{Target: &h, Kinds: pointer.Press | pointer.Release})); len(got) != 2 {
		t.Errorf("h read %v, want its press and release", got)
	}
	var ops op.Ops
	r.Frame(&ops)
	if _, _, ok := r.WriteClipboard(); !ok {
		t.Error("the write never reached the window")
	}
}

// focFrame lays out the focusables fs (60 apart from x 300, each scrolling
// vertically), then h over 0..100 asking for presses, and ends the frame.
func focFrame(r *Router, h *int, fs ...*int) {
	var ops op.Ops
	for i, f := range fs {
		st := clip.Rect(image.Rect(300+60*i, 0, 340+60*i, 100)).Push(&ops)
		event.Op(&ops, f)
		st.Pop()
		events(r, -1, key.FocusFilter{Target: f}, key.Filter{Focus: f},
			pointer.Filter{Target: f, Kinds: pointer.Scroll, ScrollY: pointer.ScrollRange{Min: -100, Max: 100}})
	}
	st := clip.Rect(image.Rect(0, 0, 100, 100)).Push(&ops)
	event.Op(&ops, h)
	st.Pop()
	events(r, -1, pointer.Filter{Target: h, Kinds: pointer.Press})
	r.Frame(&ops)
}

// noPanic fails t, instead of the binary, on a panic.
func noPanic(t *testing.T) {
	if p := recover(); p != nil {
		t.Fatalf("panic: %v", p)
	}
}

// The window's ScrollFocus (RevealFocus on a shrinking window) while a
// handler's press is queued, then a FocusCmd to a tag with no handler yet
// (listview's item focuses its composer before the composer's first
// Update): the replay must not run ScrollFocus again on that focus (a nil
// handler).
func TestClackRedoFocusWithoutHandler(t *testing.T) {
	defer noPanic(t)
	var e, h, n int
	r := new(Router)
	rsFrame(r, &e, &h, pointer.Press)
	r.Source().Execute(key.FocusCmd{Tag: &e})
	rsFrame(r, &e, &h, pointer.Press)         // e focused and in the focus order
	r.Queue(rsClick(50, false)[:1]...)        // h's press stays queued
	r.ScrollFocus(image.Pt(0, 10))            // what RevealFocus does on a shrinking frame
	r.Source().Execute(key.FocusCmd{Tag: &n}) // n has no handler: immediate, and the replay
	if f := r.lastState().focus; f != &n {
		t.Errorf("focus %v after the FocusCmd, want n", f)
	}
}

// The same on the deferred path: Frame replays before the key queue's
// frame drops what isn't laid out and renumbers the focus order, so a
// ScrollFocus run again there read the old order's index (out of range
// with fewer focusables laid out).
func TestClackRedoFocusDeferredStaleOrder(t *testing.T) {
	defer noPanic(t)
	var e1, e2, h int
	r := new(Router)
	focFrame(r, &h, &e1, &e2)
	r.Source().Execute(key.FocusCmd{Tag: &e1})
	focFrame(r, &h, &e1, &e2) // e1 focused; the order e1, e2
	r.Queue(rsClick(50, false)[:1]...)
	r.ScrollFocus(image.Pt(0, 10)) // a scroll for e1
	events(r, -1, key.FocusFilter{Target: &e2})
	r.Source().Execute(key.FocusCmd{Tag: &e2}) // e2 read its focus events: deferred
	if !r.deferring {
		t.Fatal("the FocusCmd didn't defer: nothing tested")
	}
	focFrame(r, &h, &e2) // e1 gone: e2 alone in the order
	if f := r.lastState().focus; f != &e2 {
		t.Errorf("focus %v after the deferred FocusCmd, want e2", f)
	}
	if got := pointerKinds(events(r, -1, pointer.Filter{Target: &h, Kinds: pointer.Press})); len(got) != 1 {
		t.Errorf("h read %v after the replay, want its press", got)
	}
}

// ScrollFocus's scroll, queued behind a handler's press, reaches the
// focus once through a command's replay: not lost, not twice.
func TestClackReplaysScrollFocusOnce(t *testing.T) {
	defer noPanic(t)
	var e, h int
	r := new(Router)
	focFrame(r, &h, &e)
	r.Source().Execute(key.FocusCmd{Tag: &e})
	focFrame(r, &h, &e)
	r.Queue(rsClick(50, false)[:1]...)
	r.ScrollFocus(image.Pt(0, 10))
	r.Source().Execute(key.SelectionCmd{Tag: &e}) // e laid out first: the replay
	n := 0
	for _, ev := range events(r, -1, pointer.Filter{Target: &e, Kinds: pointer.Scroll, ScrollY: pointer.ScrollRange{Min: -100, Max: 100}}) {
		if pe, ok := ev.(pointer.Event); ok && pe.Kind == pointer.Scroll {
			n++
		}
	}
	if n != 1 {
		t.Errorf("e read %d scrolls, want the one ScrollFocus delivered", n)
	}
}

// A Tab the window reserves for focus moves (a SystemEvent no handler took)
// queued behind a handler's press is replayed as one: a catch-all key
// filter doesn't get it.
func TestClackReplaysSystemTabAsOne(t *testing.T) {
	var e, h int
	r := new(Router)
	focFrame(r, &h, &e)
	r.Source().Execute(key.FocusCmd{Tag: &e})
	focFrame(r, &h, &e)
	r.Queue(rsClick(50, false)[:1]...)
	r.Queue(SystemEvent{Event: key.Event{Name: key.NameTab, State: key.Press}})
	r.Source().Execute(key.SelectionCmd{Tag: &e}) // the replay
	for _, ev := range events(r, -1, key.Filter{Focus: &e}) {
		if ke, ok := ev.(key.Event); ok && ke.Name == key.NameTab {
			t.Errorf("the catch-all filter got the replayed Tab: %+v", ke)
		}
	}
}
