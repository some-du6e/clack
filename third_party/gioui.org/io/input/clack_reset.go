// SPDX-License-Identifier: Unlicense OR MIT

package input

import (
	"gioui.org/io/event"
	"gioui.org/io/pointer"
)

// Clack patch 49 (CLACK_PATCHES.md §49): every handler's pointer state
// started over where the pointer is.

// ResetPointers gives every handler a pointer.Cancel, as a window that
// loses the focus does, and then hit-tests the mouse where it last was,
// as if it had just moved there: every handler under it gets an Enter,
// whatever filter it asked for in the last frame. Hover and press states
// start over from where the pointer is. A pointer that left the window,
// and a touch, end with the Cancel. It takes effect at once, before the
// rest of the frame reads its events, even when a handler already read
// its own this frame (that one sees the change at its next read).
//
// Clack's inspect tool calls it when a frozen frame ends: while the
// window drew a frame again from its old ops no widget laid out or read
// its events, so the router's idea of what the pointer is over moved on
// without them, and a widget the pointer left during the freeze would
// have stayed hovered (gesture.Hover, a Clickable's hover) until the
// pointer came back over it.
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

// resetPointers is ResetPointers on state: a Cancel for every handler,
// then the mouse's Enter for every handler under it, by the current hit
// tree.
func (q *pointerQueue) resetPointers(handlers map[event.Tag]*handler, state pointerState) (pointerState, []taggedEvent) {
	var evts []taggedEvent
	for k := range handlers {
		evts = append(evts, taggedEvent{tag: k, event: pointer.Event{Kind: pointer.Cancel}})
	}
	ptrs := state.pointers
	state.pointers = nil
	for _, p := range ptrs {
		e := p.last
		if e.Source != pointer.Mouse || e.Kind == pointer.Leave || e.Kind == pointer.Cancel {
			continue
		}
		e.Kind, e.Buttons = pointer.Move, 0
		var hits []event.Tag
		state.cursor = q.hitTest(e.Position, func(n *hitNode) bool {
			if _, ok := handlers[n.tag]; ok {
				hits = addHandler(hits, n.tag)
			}
			return true
		})
		for _, k := range hits {
			en := e
			en.Kind = pointer.Enter
			en.Position = q.invTransform(handlers[k].pointer.areaPlusOne-1, e.Position)
			evts = append(evts, taggedEvent{tag: k, event: en})
		}
		if len(hits) > 0 {
			state.pointers = append(state.pointers, pointerInfo{id: p.id, handlers: hits, entered: hits, last: e})
		}
	}
	return state, evts
}
