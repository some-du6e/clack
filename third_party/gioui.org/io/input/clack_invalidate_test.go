package input

// Clack patch 61: an op.InvalidateCmd executed in the middle of layout
// asks for its frame and replays nothing; FocusCmd and the other commands
// that change the input state keep the immediate path.

import (
	"image"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/op"
	"gioui.org/op/clip"
)

// invRouter is a router whose last frame laid out a (0..100), b
// (100..200) and a focusable f (300..400), each having asked for its
// events.
func invRouter(a, b, f *int) *Router {
	r := new(Router)
	var ops op.Ops
	st := clip.Rect(image.Rect(0, 0, 100, 100)).Push(&ops)
	event.Op(&ops, a)
	st.Pop()
	st = clip.Rect(image.Rect(100, 0, 200, 100)).Push(&ops)
	event.Op(&ops, b)
	st.Pop()
	st = clip.Rect(image.Rect(300, 0, 400, 100)).Push(&ops) // its own area: an unclipped tag covers the window and takes every press
	event.Op(&ops, f)
	st.Pop()
	events(r, -1, pointer.Filter{Target: a, Kinds: pointer.Press | pointer.Release})
	events(r, -1, pointer.Filter{Target: b, Kinds: pointer.Press | pointer.Release})
	events(r, -1, key.FocusFilter{Target: f})
	r.Frame(&ops)
	return r
}

func clickAt(x float32) []event.Event {
	p := f32.Pt(x, 50)
	return []event.Event{
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: p},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: p},
	}
}

func pointerKinds(evs []event.Event) []pointer.Kind {
	var out []pointer.Kind
	for _, e := range evs {
		if pe, ok := e.(pointer.Event); ok {
			out = append(out, pe.Kind)
		}
	}
	return out
}

// a lays out first and ticks; b's press and release are still queued:
// nothing is replayed, b reads its press and release once each, and the
// wakeup is a's.
func TestClackInvalidateReplaysNothing(t *testing.T) {
	var a, b, f int
	r := invRouter(&a, &b, &f)
	r.Queue(clickAt(150)...)
	at := time.Unix(1_800_000_000, 0)
	events(r, -1, pointer.Filter{Target: &a, Kinds: pointer.Press | pointer.Release}) // a: none
	r.Source().Execute(op.InvalidateCmd{At: at})                                      // a ticks
	r.Source().Execute(op.InvalidateCmd{At: at.Add(time.Second)})                     // a later one keeps the earlier
	if r.replayed != 0 {
		t.Errorf("a mid-layout InvalidateCmd replayed %d queued events", r.replayed)
	}
	got := pointerKinds(events(r, -1, pointer.Filter{Target: &b, Kinds: pointer.Press | pointer.Release}))
	if len(got) != 2 || got[0] != pointer.Press || got[1] != pointer.Release {
		t.Errorf("b read %v, want [Press Release]", got)
	}
	var ops op.Ops
	r.Frame(&ops)
	if w, ok := r.WakeupTime(); !ok || !w.Equal(at) {
		t.Errorf("WakeupTime %v (%v), want %v: the tick's frame on time", w, ok, at)
	}
}

// An InvalidateCmd while the router defers (a command waiting for the
// next frame) is recorded too.
func TestClackInvalidateWhileDeferring(t *testing.T) {
	var a, b, f int
	r := invRouter(&a, &b, &f)
	events(r, -1, key.FocusFilter{Target: &f}) // f has read its events: its focus change waits
	r.Source().Execute(key.FocusCmd{Tag: &f})
	if !r.deferring {
		t.Fatal("the focus change didn't defer")
	}
	at := time.Unix(1_800_000_000, 0)
	r.Source().Execute(op.InvalidateCmd{At: at})
	if !r.wakeup || !r.wakeupTime.Equal(at) {
		t.Errorf("wakeup %v at %v, want at %v", r.wakeup, r.wakeupTime, at)
	}
	for _, c := range r.commands {
		if _, ok := c.(op.InvalidateCmd); ok {
			t.Error("the InvalidateCmd waits with the deferred commands")
		}
	}
}

// FocusCmd keeps the immediate path: with input queued the queue is
// replayed from the new state and f reads its focus in the same frame.
func TestClackFocusCmdStillImmediate(t *testing.T) {
	var a, b, f int
	r := invRouter(&a, &b, &f)
	r.Queue(clickAt(150)...)
	r.Source().Execute(key.FocusCmd{Tag: &f})
	if r.replayed != 2 {
		t.Errorf("FocusCmd replayed %d events, want the 2 queued", r.replayed)
	}
	focused := false
	for _, e := range events(r, -1, key.FocusFilter{Target: &f}) {
		if fe, ok := e.(key.FocusEvent); ok && fe.Focus {
			focused = true
		}
	}
	if !focused {
		t.Error("f didn't read its focus in the frame the FocusCmd ran")
	}
	got := pointerKinds(events(r, -1, pointer.Filter{Target: &b, Kinds: pointer.Press | pointer.Release}))
	if len(got) != 2 {
		t.Errorf("b read %v after the replay, want [Press Release]", got)
	}
}
