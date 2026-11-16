// SPDX-License-Identifier: Unlicense OR MIT

package input

import (
	"image"
	"testing"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/op"
	"gioui.org/op/clip"
)

// Clack patch 35: a frame that shows the last frame's ops again
// (Source.Replay), with no handler asking for events in it, keeps the
// focus, routes the next press to the handler that asked for presses last
// time, and keeps the key log on (patch 28: the inspect tool's Konami
// code reads KeyPresses in every laid-out frame, none in a replayed one).
// The same frames without Replay are the upstream behaviour: the focused
// tag asked for no focus events, so it loses the focus, and the key log
// stops.
func TestReplayKeepsFilters(t *testing.T) {
	var r Router
	tag := new(int)
	var ops op.Ops
	cl := clip.Rect(image.Rect(0, 0, 50, 50)).Push(&ops)
	event.Op(&ops, tag)
	cl.Pop()
	ask := func() []event.Event {
		return events(&r, -1, key.FocusFilter{Target: tag}, pointer.Filter{Target: tag, Kinds: pointer.Press})
	}
	ask()
	r.Frame(&ops) // laid out: the handler asked for the focus and presses
	r.Source().Execute(key.FocusCmd{Tag: tag})
	ask()
	r.Frame(&ops)
	if !r.Source().Focused(tag) {
		t.Fatal("the handler isn't focused after a FocusCmd")
	}
	r.Source().Replay()
	r.Frame(&ops) // the same ops, nothing laid out, nobody asked
	if !r.Source().Focused(tag) {
		t.Error("a replayed frame dropped the focus")
	}
	r.Queue(pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Position: f32.Pt(10, 10), Buttons: pointer.ButtonPrimary})
	pressed := false
	for _, e := range ask() {
		if p, ok := e.(pointer.Event); ok && p.Kind == pointer.Press {
			pressed = true
		}
	}
	if !pressed {
		t.Error("a press after a replayed frame didn't reach the handler")
	}
	r.Frame(&ops)
	r.Frame(&ops) // a frame that asked for nothing, without Replay
	if r.Source().Focused(tag) {
		t.Error("a frame whose handlers asked for nothing kept the focus without Replay: the test shows nothing")
	}
}

func TestReplayKeepsKeyLog(t *testing.T) {
	var r Router
	var ops op.Ops
	laidOut := func() { // a frame whose layout reads the key log, as Clack's inspectBegin does
		r.Source().KeyPresses()
		r.Frame(&ops)
	}
	laidOut()
	laidOut() // logging from here
	r.Source().Replay()
	r.Frame(&ops) // nothing read it
	r.Queue(key.Event{Name: "B", State: key.Press})
	if !r.KeyLogged() {
		t.Fatal("a press after a replayed frame wasn't kept: the key log stopped")
	}
	if got := r.Source().KeyPresses(); len(got) != 1 || got[0].Name != "B" {
		t.Errorf("KeyPresses after a replayed frame = %v, want [B]", got)
	}
	r.Frame(&ops)
	r.Frame(&ops) // a frame that read nothing, without Replay
	r.Queue(key.Event{Name: "A", State: key.Press})
	if r.KeyLogged() {
		t.Error("the key log kept logging after a frame that didn't read it, without Replay: the test shows nothing")
	}
}
