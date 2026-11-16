// SPDX-License-Identifier: Unlicense OR MIT

package input

import "gioui.org/io/event"

// Clack patch 34 (CLACK_PATCHES.md): where the key focus is. A Source
// answers only "is this tag focused?". Clack's overlay layers (internal/ui/
// slackc) need what the DOM gives a modal: the element that has the focus
// (document.activeElement) when it opens, whether that one can take it
// back when it closes (still in the tree, focusable), and whether the
// focus is inside a layer's content (Node.contains), so an Escape goes to
// the innermost layer holding the focus. The answers come from the last
// frame's handlers and clip areas, as the router's own routing does.

// FocusTag returns the tag that has the key focus, or nil.
func (s Source) FocusTag() event.Tag {
	if !s.Enabled() {
		return nil
	}
	return s.r.state().keyState.focus
}

// CanFocus reports whether tag would keep a key.FocusCmd past the frame:
// in the last frame it was drawn (an event.Op) and asked for focus events
// (key.FocusFilter), which is what keyQueue.Frame keeps the focus on.
func (s Source) CanFocus(tag event.Tag) bool {
	if !s.Enabled() || tag == nil {
		return false
	}
	h, ok := s.r.handlers[tag]
	return ok && h.filter.focusable && h.key.visible
}

// AskedFocus reports whether tag asked for focus events (a key.FocusFilter
// read) in the frame in progress, since the last Frame: at the end of a
// frame's layout, whether a key.FocusCmd for it would keep the focus past
// that frame (drawn widgets ask as they lay out). CanFocus answers for the
// last frame, which a tag that went in this one still passes.
func (s Source) AskedFocus(tag event.Tag) bool {
	if !s.Enabled() || tag == nil {
		return false
	}
	h, ok := s.r.handlers[tag]
	return ok && h.nextFilter.focusable
}

// Within reports whether tag was drawn inside container in the last frame:
// tag's event.Op is in container's clip area or in one nested in it. An op
// outside every clip area is inside nothing. Deferred ops (op.Defer) run
// from the root, so a container holding deferred content draws its own op
// inside the deferred macro.
func (s Source) Within(tag, container event.Tag) bool {
	if !s.Enabled() || tag == nil || container == nil {
		return false
	}
	return s.r.within(tag, container)
}

func (q *Router) within(tag, container event.Tag) bool {
	h, ok := q.handlers[tag]
	c, ok2 := q.handlers[container]
	if !ok || !ok2 || h.pointer.areaPlusOne == 0 || c.pointer.areaPlusOne == 0 {
		return false
	}
	areas := q.pointer.queue.areas
	want := c.pointer.areaPlusOne - 1
	for a := h.pointer.areaPlusOne - 1; a >= 0 && a < len(areas); a = areas[a].parent {
		if a == want {
			return true
		}
	}
	return false
}
