package ui

import (
	"image"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/inputidle"
)

// cmd/clack's trim waits for a pause in input (inputidle), and input is
// whatever the window's router is queued, whichever widget takes it (Gio
// patch 32): the review of 2026-11-16 found keyboard navigation and text
// fields other than the composer, grabbed drags and popovers reading as
// idle when only root handlers and the composer noted input.

// rootFrame lays out the App's root input handling in App.Layout's order
// around body, which registers its own handlers: the root pointer reader
// and the shortcut area first (under everything), the window-wide
// pass-through tracker last (over everything).
func rootFrame(f *navFrame, body func(gtx layout.Context)) {
	f.ops.Reset()
	gtx := layout.Context{Ops: &f.ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Exact(image.Pt(800, 600)), Source: f.r.Source(), Now: time.Now()}
	f.a.readRootPointer(gtx)
	f.a.keys.process(gtx, f.a)
	body(gtx)
	f.a.rootPointer(gtx)
	f.r.Frame(&f.ops)
}

// noted reports whether input was noted since t0.
func noted(t0 time.Time) bool { return inputidle.Since() <= time.Since(t0) }

// A focused widget other than the composer (the message list's arrow-key
// navigation, a plugin's or Block Kit's text field) takes its keys, and
// they count.
func TestInputIdleSeesFocusedWidgetKeys(t *testing.T) {
	f := &navFrame{a: navTestApp(t)}
	var list int
	got := 0
	focus := true
	body := func(gtx layout.Context) {
		for {
			e, ok := gtx.Event(key.FocusFilter{Target: &list}, key.Filter{Focus: &list, Name: key.NameDownArrow})
			if !ok {
				break
			}
			if ke, ok := e.(key.Event); ok && ke.State == key.Press {
				got++
			}
		}
		area := clip.Rect{Max: image.Pt(800, 600)}.Push(gtx.Ops)
		event.Op(gtx.Ops, &list)
		area.Pop()
		if focus {
			gtx.Execute(key.FocusCmd{Tag: &list})
			focus = false
		}
	}
	for i := 0; i < 3; i++ { // the focus lands a frame later
		rootFrame(f, body)
	}
	time.Sleep(30 * time.Millisecond)
	t0 := time.Now()
	for i := 0; i < 5; i++ {
		f.r.Queue(key.Event{Name: key.NameDownArrow, State: key.Press}, key.Event{Name: key.NameDownArrow, State: key.Release})
		rootFrame(f, body)
	}
	if got != 5 {
		t.Fatalf("the focused widget took %d of 5 presses", got)
	}
	if !noted(t0) {
		t.Errorf("5 presses a focused widget took didn't count as input (the last input %v ago)", inputidle.Since())
	}
}

// A wheel scroll counts, and the scrollable under the root still gets all
// of it.
func TestInputIdleSeesScroll(t *testing.T) {
	f := &navFrame{a: navTestApp(t)}
	var list int
	var got float32
	body := func(gtx layout.Context) {
		for {
			e, ok := gtx.Event(pointer.Filter{Target: &list, Kinds: pointer.Scroll, ScrollY: pointer.ScrollRange{Min: -10000, Max: 10000}})
			if !ok {
				break
			}
			if pe, ok := e.(pointer.Event); ok && pe.Kind == pointer.Scroll {
				got += pe.Scroll.Y
			}
		}
		area := clip.Rect{Max: image.Pt(800, 600)}.Push(gtx.Ops)
		event.Op(gtx.Ops, &list)
		area.Pop()
	}
	rootFrame(f, body)
	rootFrame(f, body)
	time.Sleep(30 * time.Millisecond)
	t0 := time.Now()
	f.r.Queue(pointer.Event{Kind: pointer.Scroll, Source: pointer.Mouse, Position: f32.Pt(400, 300), Scroll: f32.Pt(0, 120)})
	rootFrame(f, body)
	rootFrame(f, body)
	if got != 120 {
		t.Errorf("the scrollable got %v of the 120 scrolled", got)
	}
	if !noted(t0) {
		t.Errorf("the scroll didn't count as input (the last input %v ago)", inputidle.Since())
	}
}

// A drag a widget grabbed (a text selection, a column resize) goes to that
// widget alone, the root's handler included out, and counts.
func TestInputIdleSeesGrabbedDrag(t *testing.T) {
	f := &navFrame{a: navTestApp(t)}
	var grip int
	drags := 0
	body := func(gtx layout.Context) {
		for {
			e, ok := gtx.Event(pointer.Filter{Target: &grip, Kinds: pointer.Press | pointer.Drag | pointer.Release})
			if !ok {
				break
			}
			pe, ok := e.(pointer.Event)
			if !ok {
				continue
			}
			switch pe.Kind {
			case pointer.Press:
				gtx.Execute(pointer.GrabCmd{Tag: &grip, ID: pe.PointerID})
			case pointer.Drag:
				drags++
			}
		}
		area := clip.Rect{Max: image.Pt(800, 600)}.Push(gtx.Ops)
		event.Op(gtx.Ops, &grip)
		area.Pop()
	}
	rootFrame(f, body)
	f.r.Queue(pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: f32.Pt(100, 100)})
	rootFrame(f, body)
	rootFrame(f, body)
	time.Sleep(30 * time.Millisecond)
	t0 := time.Now()
	before := f.a.ptrWin
	for i := 1; i <= 5; i++ {
		// a move with the button held: the router makes it a Drag
		f.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: f32.Pt(100+float32(20*i), 100)})
		rootFrame(f, body)
	}
	if drags == 0 {
		t.Fatal("the grabbing widget got no drags")
	}
	if f.a.ptrWin != before {
		t.Fatalf("the root tracker followed the grabbed drag (%v to %v), so this no longer tests input only the router sees", before, f.a.ptrWin)
	}
	if !noted(t0) {
		t.Errorf("a grabbed drag didn't count as input (the last input %v ago)", inputidle.Since())
	}
}
