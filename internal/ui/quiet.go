package ui

import (
	"image"
	"time"

	"gioui.org/app"
	"gioui.org/io/input"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/slackc"
)

// Quiet frames: a frame only an animation asked for shows the last frame's
// ops again, without laying the window out.
//
// Gio draws the whole window every frame, and Clack lays all of it out for
// each one: 17–23 ms on Karim's Mac with his plugins (2026-11-16). An
// animated custom emoji (:explodes:, 13 frames of 30–70 ms) asked for a
// frame every frame of its own, so while it was on screen the window spent
// most of its time laying out what hadn't changed, the animation played
// slowly, and input waited behind it. Chromium repaints only the image's
// layer (cc's ImageAnimationController invalidates the image, not the
// page).
//
// Here the image cache hands out its playing animations as live ImageOps
// (imgcache.SetLiveOps, Gio patch 35): the GPU draws the frame due when it
// draws the window, so the same ops show an animation where it is now. A
// frame is quiet when the image cache's animation repaint
// (SetAnimInvalidate: a next frame due, a player started) is all that
// asked for it: no Invalidate from anything else (data, posted work, a
// load that landed, a timer), nothing the window's own frames asked for
// that's due (input a handler takes, an op.InvalidateCmd: FrameEvent.Asked,
// Gio patch 35), the same size, scale, insets and zoom as the last frame,
// which was the main window's own full frame (not under the boot cover or
// the inspect tool, and no other window drew patch points since: slackc
// Views can be shared). A quiet frame submits that frame's ops again, the
// router keeping the event filters its widgets asked for (Source.Replay:
// the focus stays where it is), and asks for any later wake-up the last
// full frame asked for, which a frame takes from the window. Everything
// else is laid out as before.

// quietState is what the last full frame was drawn for.
type quietState struct {
	ok     bool // it can be shown again
	size   image.Point
	metric unit.Metric
	insets app.Insets
	zoom   float32
	subs   uint64 // slackc.SubFrames then
}

// wireQuietFrames makes the image cache's animations live ImageOps that
// repaint through animReady (Run, and tests that draw like it).
func (a *App) wireQuietFrames() {
	if a.images == nil {
		return
	}
	a.images.SetLiveOps(true)
	a.images.SetAnimInvalidate(a.animReady)
}

// animReady is the image cache's repaint for animations alone (any
// goroutine): a frame only it asks for may be a quiet one. After a slow
// frame it waits (animHold).
func (a *App) animReady() {
	a.animDue.Store(true)
	if d := a.animHold(time.Now()); d > 0 {
		if a.animHeld.CompareAndSwap(false, true) {
			time.AfterFunc(d, func() {
				a.animHeld.Store(false)
				a.animReady()
			})
		}
		return
	}
	if h := invalidateHook; h != nil {
		h(a)
	}
	select {
	case a.inval <- struct{}{}:
	default: // one is already queued; it covers this one
	}
	if h := a.hudLive.Load(); h != nil {
		h.ImagesChanged()
	}
}

// quietFrame reports whether the frame for now (the window's size, metric
// and insets; asked, askedAt: FrameEvent.Asked/AskedAt) may show the last
// frame's ops again. It takes the frame's reasons (Invalidate's and
// animReady's), so call it once per frame. UI goroutine, frame lock held.
func (a *App) quietFrame(now time.Time, size image.Point, m unit.Metric, insets app.Insets, asked bool, askedAt time.Time) bool {
	layout := a.layoutDue.Swap(false)
	anim := a.animDue.Swap(false)
	q := &a.quiet
	a.quietWhy = ""
	switch {
	case layout:
		a.quietWhy = "an Invalidate"
	case !anim:
		a.quietWhy = "no animation asked"
	case !q.ok:
		a.quietWhy = "the last frame can't be shown again"
	case asked && !askedAt.After(now):
		a.quietWhy = "the window's frames asked" // input, an op.InvalidateCmd
	case size != q.size || m != q.metric || insets != q.insets || a.zoomFactor() != q.zoom:
		a.quietWhy = "another size or scale"
	case slackc.SubFrames() != q.subs:
		a.quietWhy = "another window drew"
	case a.login != nil || a.insp != nil:
		a.quietWhy = "sign-in or the inspect tool"
	default:
		a.mu.Lock()
		if len(a.pending) > 0 { // postLazy's work waits for a frame
			a.quietWhy = "posted work"
		}
		a.mu.Unlock()
	}
	return a.quietWhy == ""
}

// replayFrame is a quiet frame's work before its ops go out again: the
// router keeps the event filters the last full frame's widgets asked for
// (Source.Replay, Gio patch 35: none ask in this one, and without it the
// focused composer lost the focus at every quiet frame and got it back at
// the next full one, a blur and a focus for plugins each time), and the
// wake-up the last full frame asked for, which isn't due yet, is asked for
// again (the window takes it with every frame).
func (a *App) replayFrame(src input.Source, asked bool, askedAt, now time.Time) {
	src.Replay()
	if asked && askedAt.After(now) {
		src.Execute(op.InvalidateCmd{At: askedAt})
	}
	a.quietFrames++
}

// noteFullFrame records what a full frame was drawn for; covered: only the
// boot cover went to the GPU, so its ops can't be shown again.
func (a *App) noteFullFrame(size image.Point, m unit.Metric, insets app.Insets, covered bool) {
	a.quiet = quietState{ok: !covered && a.login == nil && a.insp == nil, size: size, metric: m, insets: insets,
		zoom: a.zoomFactor(), subs: slackc.SubFrames()}
}

// animHold is how long an animation's repaint waits after a slow frame. A
// frame whose layout took over a display frame (16 ms: a scroll, a
// plugin's render) is followed by as long again without a frame for
// animations alone, so they take at most half of the UI goroutine and input
// keeps its turn; the animations keep to their timelines meanwhile (a frame
// drawn late skips to the one due). A quiet frame lays nothing out, so
// after one nothing waits.
func (a *App) animHold(now time.Time) time.Duration {
	cost := time.Duration(a.lastLayout.Load())
	if cost < 16*time.Millisecond {
		return 0
	}
	return max(time.Unix(0, a.lastFrameEnd.Load()).Add(cost).Sub(now), 0)
}

// noteFrameCost records a submitted frame's layout time (0: quiet).
func (a *App) noteFrameCost(layout time.Duration) {
	a.lastLayout.Store(int64(layout))
	a.lastFrameEnd.Store(time.Now().UnixNano())
}
