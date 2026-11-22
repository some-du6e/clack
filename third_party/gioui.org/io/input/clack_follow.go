// SPDX-License-Identifier: Unlicense OR MIT

package input

import (
	"image"
	"time"

	"gioui.org/io/event"
	"gioui.org/op"
)

// Clack patch 57: FollowArea is AreaBounds (patch 53) for a widget that
// places something by where an area is (a hover card by its trigger).
// AreaBounds answers with the last frame's area, so the frame being laid
// out may move the area and nothing ask for the frame that would see it:
// a layout change sends no pointer event to an area that moves under a
// resting pointer. FollowArea notes where it said the area was, and the
// Frame given the ops it was asked from asks for another frame at once
// when they put the area elsewhere (or give it one, or none).
func (q *Router) FollowArea(tag event.Tag) (full, shown image.Rectangle, ok bool) {
	full, shown, ok = q.AreaBounds(tag)
	if q.follow == nil {
		q.follow = map[event.Tag]followedArea{}
	}
	q.follow[tag] = followedArea{full: full, ok: ok}
	return full, shown, ok
}

// FollowArea is the router's (Clack patch 57); false without one.
func (s Source) FollowArea(tag event.Tag) (full, shown image.Rectangle, ok bool) {
	if s.r == nil {
		return image.Rectangle{}, image.Rectangle{}, false
	}
	return s.r.FollowArea(tag)
}

// followedArea is where FollowArea said a tag's area was.
type followedArea struct {
	full image.Rectangle
	ok   bool
}

// followFrame runs once a frame's areas are collected: a tag read through
// FollowArea whose area isn't where its reader was told asks for a frame
// now. The reads are the laid-out frame's: the next layout reads again.
func (q *Router) followFrame() {
	for tag, f := range q.follow {
		full, _, ok := q.AreaBounds(tag)
		if ok != f.ok || ok && full != f.full {
			if h := ExecuteHook; h != nil {
				h(op.InvalidateCmd{}) // patch 10: the idle probe names this frame's asker (followFrame)
			}
			q.wakeup, q.wakeupTime = true, time.Time{}
		}
	}
	clear(q.follow)
}
