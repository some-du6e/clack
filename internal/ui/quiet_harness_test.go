package ui

import (
	"time"

	"gioui.org/app"
	"gioui.org/unit"
)

// onAnimRepaint wires e's image cache as the window does (Run: its
// repaints through Invalidate, its animations' through animReady, live
// ImageOps: quiet.go) and calls f at each of its repaints.
func onAnimRepaint(e *perfEnv, f func()) {
	e.a.wireQuietFrames()
	e.a.images.SetInvalidate(func() {
		e.a.Invalidate()
		f()
	})
	e.a.images.SetAnimInvalidate(func() {
		e.a.animReady()
		f()
	})
}

// quietFrame draws e's next frame as the window's loop does (App.Run): the
// last frame's ops again when only an animation asked for it, else a full
// frame. asked, askedAt are what the last frame asked for (the router's
// WakeupTime after it: input pending, an op.InvalidateCmd), as the window
// hands them over in FrameEvent.Asked. Without a GPU the live images are
// asked for their frames as a GPU drawing the ops would (ReplayLive). It
// reports whether the frame was quiet.
func (e *perfEnv) quietFrame(asked bool, askedAt time.Time) bool {
	now := time.Now()
	sc := e.scale
	if sc == 0 {
		sc = 1
	}
	if e.a.quietFrame(now, e.size, unit.Metric{PxPerDp: sc, PxPerSp: sc}, app.Insets{}, asked, askedAt) {
		e.a.replayFrame(e.r.Source(), asked, askedAt, now)
		e.r.Frame(&e.ops)
		if e.gpu != nil {
			if err := e.gpu.Frame(&e.ops); err != nil {
				e.tb.Fatal(err)
			}
		} else {
			e.a.images.ReplayLive()
		}
		return true
	}
	e.frame()
	return false
}

// frameWhy is why e's last frame wasn't quiet (quiet.go's quietWhy).
func frameWhy(e *perfEnv) string { return e.a.quietWhy }
