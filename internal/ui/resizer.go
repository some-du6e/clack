package ui

// colResizer is the pointer side of Slack's BaseResizer (client-boot-shared
// 7M48), shared by the sidebar, wide sidebar, secondary pane and split
// view handles.
//
// BaseResizer listens for mousemove and mouseup on the document, not the
// handle: once a press lands on the 8px strip, every move anywhere in the
// window (and, with the button held, outside it) resizes the column until
// the button comes up, and a fixed .p-resizer-overlay (z-index 999,
// cursor col-resize) covers the page meanwhile. In Gio that is a grab:
// the press executes pointer.GrabCmd, so the handle alone keeps getting
// Drag events wherever the pointer goes, other handlers can't steal the
// gesture, and the Release reaches it even outside the window.
//
// The handle sits on the edge it moves, so the events are read in the
// frame of the gtx it is laid out in (its area is clipped there, not
// offset): a position means the same thing whichever frame the edge was
// drawn at. Callers read the events (next) before they size their
// columns, then draw the handle (area), so a drag shows in the frame it
// arrives in.

import (
	"image"
	"time"

	"gioui.org/gesture"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
)

type colResizer struct {
	drag gesture.Drag
	// edge is the column edge the handle was last drawn at, px in its
	// gtx's frame; off the press's distance from it
	edge int
	off  float32
	// lastPress is the last press: another within 500 ms is onReset's
	// double click, and reset holds until its release (no drag follows)
	lastPress time.Time
	reset     bool
	active    bool // a press is being dragged (BaseResizer allowDragEvents)
	drawn     bool // area ran this frame (live)
}

type resizeKind uint8

const (
	resizeNone  resizeKind = iota
	resizeStart            // a press (onResizeStart)
	resizeReset            // a double click (onReset)
	resizeMove             // the edge follows the pointer to pos
	resizeEnd              // the button came up after a press (onResizeEnd)
)

// resizeEvent is one of the handle's events; pos is the edge's new
// position (px, the handle's gtx frame) for resizeMove.
type resizeEvent struct {
	kind resizeKind
	pos  float32
}

// next returns the handle's next event since the last frame.
func (r *colResizer) next(gtx layout.Context) (resizeEvent, bool) {
	for {
		e, ok := r.drag.Update(gtx.Metric, gtx.Source, gesture.Horizontal)
		if !ok {
			return resizeEvent{}, false
		}
		switch e.Kind {
		case pointer.Press:
			// take the pointer now, not after the touch slop: nothing else
			// sees this gesture until the release
			gtx.Execute(pointer.GrabCmd{Tag: &r.drag, ID: e.PointerID})
			if !r.lastPress.IsZero() && gtx.Now.Sub(r.lastPress) < 500*time.Millisecond {
				r.lastPress, r.reset, r.active = time.Time{}, true, false
				return resizeEvent{kind: resizeReset}, true
			}
			r.lastPress, r.reset, r.active = gtx.Now, false, true
			r.off = e.Position.X - float32(r.edge)
			return resizeEvent{kind: resizeStart}, true
		case pointer.Drag:
			if r.reset || !r.active {
				continue
			}
			return resizeEvent{kind: resizeMove, pos: e.Position.X - r.off}, true
		case pointer.Release, pointer.Cancel:
			r.reset = false
			if !r.active {
				continue
			}
			r.active = false
			return resizeEvent{kind: resizeEnd}, true
		}
	}
}

// dragging reports a press being dragged.
func (r *colResizer) dragging() bool { return r.active }

// area registers the handle: hit, its strip, and edge, the column edge
// it was drawn for, both in gtx's frame.
func (r *colResizer) area(gtx layout.Context, hit image.Rectangle, edge int) {
	r.edge, r.drawn = edge, true
	defer clip.Rect(hit).Push(gtx.Ops).Pop()
	event.Op(gtx.Ops, &r.drag)
	pointer.CursorColResize.Add(gtx.Ops)
}

// live reports a drag whose handle this frame drew, once per frame (it
// clears the mark). A handle that wasn't drawn ends its drag: Gio drops
// its handler without an event (a Cancel comes only if it returns).
func (r *colResizer) live() bool {
	if !r.drawn {
		r.active = false
	}
	r.drawn = false
	return r.active
}

// resizeOverlay is .p-resizer-overlay while a handle is dragged: the
// col-resize cursor over the whole window. Laid out last; it passes
// everything through (the handle's grab already has the pointer).
func (a *App) resizeOverlay(gtx layout.Context) {
	sb, sec, sp := a.sidebar.rz.live(), a.secondary.rz.live(), a.split.rz.live()
	wide, xwide := a.sbWide.rz.live(), a.sbXWide.rz.live()
	if !sb && !sec && !sp && !wide && !xwide {
		return
	}
	defer pointer.PassOp{}.Push(gtx.Ops).Pop()
	defer clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops).Pop()
	event.Op(gtx.Ops, &a.rzOverlayTag)
	pointer.CursorColResize.Add(gtx.Ops)
}
