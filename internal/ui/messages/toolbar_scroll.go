package messages

import (
	"image"

	"gioui.org/gesture"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
)

// The wheel over the message actions. Slack's hover toolbar
// (MessageActions, position: absolute) and the simplified layout's
// "Message actions" button sit in the message row, so inside the list's
// scroller: a wheel over their buttons scrolls the message pane, as the
// browser scrolls the nearest scrollable box around the event's target.
// Clack draws them over the list, after it, and Gio routes the wheel only
// to the handlers under the pointer and their ancestors. The list's own
// handler (layout.List's gesture.Scroll) is an ancestor of the rows only,
// and the buttons' input areas hide the rows, so the wheel reached nothing
// that scrolls (Karim: "i cant seem to scroll when hovering over any of
// those icons").
//
// actScroll stands in for the list's handler there. The card (or the
// button) puts it under its buttons with the list's scroll range, and what
// it reports moves the list before the list lays out, the way layout.List
// applies its own gesture. It is a gesture.Scroll like the List's, so a
// notch animates on the same curve (Gio patch 9) and a middle click can
// start autoscroll over a button (patch 15), as Chromium's does.

// actionsScroll moves the list by what the actions' scroll handler reports
// and returns the distance. Call it before the list lays out.
func (s *listState) actionsScroll(gtx layout.Context) int {
	d := s.actScroll.Update(gtx.Metric, gtx.Source, gtx.Now, gesture.Vertical, pointer.ScrollRange{}, s.scrollRange())
	if d != 0 {
		s.list.Position.BeforeEnd = true // List.ScrollBy: the end-aligned list keeps the offset
		s.list.Position.Offset += d
	}
	return d
}

// actionsScrollEnd stops the actions' wheel animation or fling once the
// list is at the end it moved toward (d this frame), as layout.List stops
// its own. Call it after the list lays out.
func (s *listState) actionsScrollEnd(d int) {
	p := s.list.Position
	if d < 0 && p.First == 0 && p.Offset <= 0 || d > 0 && !p.BeforeEnd {
		s.actScroll.Stop()
	}
}

// scrollRange is how far the list can scroll from where it is, worked out
// from its Position as layout.List does for its own gesture (List.update):
// unbounded toward an end whose item wasn't laid out.
func (s *listState) scrollRange() pointer.ScrollRange {
	const inf = 1e6 // layout.List's
	p := s.list.Position
	r := pointer.ScrollRange{Min: -inf, Max: inf}
	if p.First == 0 {
		r.Min = min(-p.Offset, 0)
	}
	if p.First+p.Count == len(s.items) {
		r.Max = max(-p.OffsetLast, 0)
	}
	return r
}

// actionsScrollArea puts the actions' scroll handler over r: what is drawn
// before the returned stack pops is inside it, as the buttons are inside
// the scroller in Slack. The area clips that drawing to r, so a card draws
// its shadow first.
func (s *listState) actionsScrollArea(gtx layout.Context, r image.Rectangle) clip.Stack {
	st := clip.Rect(r).Push(gtx.Ops)
	s.actScroll.Add(gtx.Ops)
	return st
}
