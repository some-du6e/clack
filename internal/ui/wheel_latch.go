package ui

import (
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"

	"github.com/some-du6e/clack/internal/ui/richview"
)

// wheelObserver is a window-wide area over everything that takes none of
// the wheel and sees each wheel event: richview's carousels take a gesture
// only if it began on them, as Chromium latches a gesture to the scroller
// under its first event (richview.NoteWheel, WheelLatch).
func (a *App) wheelObserver(gtx layout.Context) {
	for {
		e, ok := gtx.Event(pointer.Filter{Target: &a.wheelTag, Kinds: pointer.Scroll}) // no range: it takes none
		if !ok {
			break
		}
		if pe, ok := e.(pointer.Event); ok && pe.Kind == pointer.Scroll {
			richview.NoteWheel(pe.Time, gtx.Now)
		}
	}
	defer pointer.PassOp{}.Push(gtx.Ops).Pop()
	defer clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops).Pop()
	event.Op(gtx.Ops, &a.wheelTag)
}
