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
// Views can be shared). A quiet frame submits that frame's ops again and
// asks for any later wake-up the last full frame asked for, which a frame
// takes from the window. Everything else is laid out as before.

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
// goroutine): a frame only it asks for may be a quiet one.
func (a *App) animReady() {
	a.animDue.Store(true)
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
	if layout || !anim || !q.ok || asked && !askedAt.After(now) {
		return false
	}
	if size != q.size || m != q.metric || insets != q.insets || a.zoomFactor() != q.zoom || slackc.SubFrames() != q.subs {
		return false
	}
	if a.login != nil || a.insp != nil {
		return false
	}
	a.mu.Lock()
	posted := len(a.pending) > 0 // postLazy's work waits for a frame
	a.mu.Unlock()
	return !posted
}

// replayFrame is a quiet frame's work before its ops go out again: the
// wake-up the last full frame asked for, which isn't due yet, asked for
// again (the window takes it with every frame).
func (a *App) replayFrame(src input.Source, asked bool, askedAt, now time.Time) {
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
