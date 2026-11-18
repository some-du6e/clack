// SPDX-License-Identifier: Unlicense OR MIT

package input

import (
	"slices"
	"time"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
)

// Clack patch 49 (CLACK_PATCHES.md §49): every handler's pointer state
// started over where the pointer is.

// ResetPointers catches every handler up with where the mouse is, as if
// each had read every event the router routed while it didn't. It sends
// what the router would have sent then:
//   - every handler gets a pointer.Cancel, which ends a press, a drag or
//     a grab;
//   - a handler that last read an Enter or a Move, with no Leave or
//     Cancel since, and isn't under the mouse now gets a Leave;
//   - every handler under the mouse gets an Enter and a Move there,
//     whatever filter it asked for in the last frame (none, for a frame
//     drawn again from old ops).
//
// Each event carries the mouse's own PointerID (the one a handler's
// Enter carried, for its Cancel and Leave): Windows numbers the mouse 1,
// and gesture.Hover drops a Cancel or a Leave of another pointer. A mouse
// whose last event was a Leave (it left the window) gets no Enter
// anywhere. Gio sends that Leave only on Windows (patch 22); elsewhere a
// mouse that left is entered again where it was last seen in the window,
// as upstream leaves that widget hovered. Touches end with the Cancel. It
// takes effect at once, before the rest of the frame reads its events,
// even when a handler already read its own this frame (that one sees the
// change at its next read).
//
// Clack's inspect tool calls it when a frozen frame ends: while the
// window drew a frame again from its old ops no widget laid out or read
// its events, so the router's idea of what the pointer is over moved on
// without them. A widget the pointer left during the freeze would have
// stayed hovered (gesture.Hover, a Clickable's hover, a message body's
// link and tooltip, a plugin tree's :hover) until the pointer came back
// over it, and a Cancel alone doesn't clear the hover trackers that
// clear only on a Leave.
func (s Source) ResetPointers() {
	if s.Enabled() {
		s.r.execute(resetPointersCmd{})
	}
}

// resetPointersCmd is ResetPointers' command: executeCommand runs it, and
// execute runs it at once (immediateCmd).
type resetPointersCmd struct{}

func (resetPointersCmd) ImplementsCommand() {}

// immediateCmd reports a command that execute runs at once even when a
// handler already read events like the ones it makes (ResetPointers).
func immediateCmd(c Command) bool {
	_, ok := c.(resetPointersCmd)
	return ok
}

// mouseInfo is the mouse's last event, as Push saw it: its id, where it
// was, and whether it is in the window (no Leave since).
type mouseInfo struct {
	id  pointer.ID
	pos f32.Point
	t   time.Duration
	in  bool
}

// noteMouse keeps e if it is the mouse's (Push).
func (q *pointerQueue) noteMouse(e pointer.Event) {
	if e.Source != pointer.Mouse {
		return
	}
	switch e.Kind {
	case pointer.Move, pointer.Press, pointer.Release:
		q.mouse = mouseInfo{id: e.PointerID, pos: e.Position, t: e.Time, in: true}
	case pointer.Leave:
		q.mouse = mouseInfo{id: e.PointerID, pos: e.Position, t: e.Time}
	}
}

// notePointerRead keeps what a handler read of the pointer (Router.Event):
// an Enter or a Move puts that pointer over it, a Leave or a Cancel takes
// it off.
func (q *Router) notePointerRead(evt taggedEvent) {
	e, ok := evt.event.(pointer.Event)
	if !ok {
		return
	}
	h, ok := q.handlers[evt.tag]
	if !ok {
		return
	}
	switch e.Kind {
	case pointer.Enter, pointer.Move:
		h.pointer.seen, h.pointer.seenID = true, e.PointerID
	case pointer.Leave, pointer.Cancel:
		h.pointer.seen = false
	}
}

// resetPointers is ResetPointers on state, by the current hit tree.
func (q *pointerQueue) resetPointers(handlers map[event.Tag]*handler, state pointerState) (pointerState, []taggedEvent) {
	m := q.mouse
	var hits []event.Tag
	if m.in {
		state.cursor = q.hitTest(m.pos, func(n *hitNode) bool {
			if _, ok := handlers[n.tag]; ok {
				hits = addHandler(hits, n.tag)
			}
			return true
		})
	}
	var evts []taggedEvent
	for k, h := range handlers {
		id := m.id
		if h.pointer.seen {
			id = h.pointer.seenID
		}
		evts = append(evts, taggedEvent{tag: k, event: pointer.Event{Kind: pointer.Cancel, Source: pointer.Mouse, PointerID: id, Time: m.t}})
		e := pointer.Event{Source: pointer.Mouse, PointerID: m.id, Position: q.invTransform(h.pointer.areaPlusOne-1, m.pos), Time: m.t}
		switch {
		case slices.Contains(hits, k):
			e.Kind = pointer.Enter
			evts = append(evts, taggedEvent{tag: k, event: e})
			e.Kind = pointer.Move
			evts = append(evts, taggedEvent{tag: k, event: e})
		case h.pointer.seen:
			e.Kind, e.PointerID = pointer.Leave, id
			evts = append(evts, taggedEvent{tag: k, event: e})
		}
	}
	state.pointers = nil
	if len(hits) > 0 {
		last := pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, PointerID: m.id, Position: m.pos, Time: m.t}
		state.pointers = []pointerInfo{{id: m.id, handlers: hits, entered: hits, last: last}}
	}
	return state, evts
}
