// SPDX-License-Identifier: Unlicense OR MIT

package input

import (
	"image"

	"gioui.org/io/event"
)

// Clack patch 53: AreaBounds is the window rectangle of the clip area tag
// was registered on (event.Op) in the last frame, and that rectangle
// clipped to every clip area it was in: what of it showed. A widget in a
// scrolling list learns its share of the list's viewport from it, wherever
// the list is (Clack's video tiles pause under 10% of it, Slack's
// useScrollDockTrigger). ok is false when tag had no area last frame.
func (q *Router) AreaBounds(tag event.Tag) (full, shown image.Rectangle, ok bool) {
	h, found := q.handlers[tag]
	if !found || h.pointer.areaPlusOne <= 0 {
		return image.Rectangle{}, image.Rectangle{}, false
	}
	i := h.pointer.areaPlusOne - 1
	if i >= len(q.pointer.queue.areas) {
		return image.Rectangle{}, image.Rectangle{}, false
	}
	full = q.pointer.queue.areas[i].bounds()
	return full, q.pointer.queue.ClipFor(i, full), true
}

// AreaBounds is the router's (Clack patch 53); false without one.
func (s Source) AreaBounds(tag event.Tag) (full, shown image.Rectangle, ok bool) {
	if s.r == nil {
		return image.Rectangle{}, image.Rectangle{}, false
	}
	return s.r.AreaBounds(tag)
}
