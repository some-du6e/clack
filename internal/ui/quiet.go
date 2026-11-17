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
// asked for it:
//
//   - no App.Invalidate since the last frame (data, posted work, a load
//     that landed, a plugin's tree, a timer), nor posted work waiting, nor
//     UI-goroutine work between the frames (drainIdle's posted work, a
//     window event Run handles: they set layoutDue);
//   - nothing the window's own frames asked for that's due:
//     FrameEvent.Asked/AskedAt (Gio patch 35) are the router's wake-ups
//     (an op.InvalidateCmd: a caret's blink, a transition), input or a
//     focus change a handler took, a key press the key log kept, an
//     Option, but not Window.Invalidate, which every one of Clack's
//     repaints goes through (the invalidator);
//   - the same size, scale, insets and zoom as the last frame, which was
//     the main window's own full frame (not under the boot cover, sign-in
//     or the inspect tool), and no other window drew patch points since.
//
// A quiet frame submits that frame's ops again, the router keeping the
// event filters its widgets asked for and the key log (Source.Replay), the
// image cache counting that frame's images as drawn now (FrameRedrawn: the
// idle sweep, the LRU, the hot queue), and asks again for a later wake-up
// that frame asked for, which a frame takes from the window. Everything
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
// goroutine): a frame only it asks for may be a quiet one. The huddle
// windows draw the cache's images too, and get it as imagesReady's.
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
// frame's ops again, and notes why not (quietWhy). It takes the frame's
// reasons (Invalidate's and animReady's), so call it once per frame. UI
// goroutine, frame lock held.
func (a *App) quietFrame(now time.Time, size image.Point, m unit.Metric, insets app.Insets, asked bool, askedAt time.Time) bool {
	layout := a.layoutDue.Swap(false)
	anim := a.animDue.Swap(false)
	q := &a.quiet
	a.quietWhy = ""
	switch {
	case layout:
		a.quietWhy = whyInvalidate
	case !anim:
		a.quietWhy = whyNoAnimation
	case !q.ok:
		a.quietWhy = whyNotShowable
	case asked && !askedAt.After(now):
		a.quietWhy = whyWindowAsked // input, an op.InvalidateCmd
	case size != q.size || m != q.metric || insets != q.insets || a.zoomFactor() != q.zoom:
		a.quietWhy = whyResized
	case slackc.SubFrames() != q.subs:
		a.quietWhy = whyOtherWindow
	case a.login != nil || a.insp != nil:
		a.quietWhy = whySignInInspect
	default:
		a.mu.Lock()
		if len(a.pending) > 0 { // postLazy's work waits for a frame
			a.quietWhy = whyPosted
		}
		a.mu.Unlock()
	}
	return a.quietWhy == ""
}

// Why a frame wasn't quiet (quietWhy).
const (
	whyInvalidate    = "an Invalidate"
	whyNoAnimation   = "no animation asked"
	whyNotShowable   = "the last frame can't be shown again"
	whyWindowAsked   = "the window's frames asked"
	whyResized       = "another size or scale"
	whyOtherWindow   = "another window drew"
	whySignInInspect = "sign-in or the inspect tool"
	whyPosted        = "posted work"
)

// tryQuietFrame is App.Run's quiet frame: if only an animation asked for
// the frame (quietFrame), it does a quiet frame's work before its ops go out
// again and reports true; the caller then submits the last full frame's ops
// as they are. The work: the router keeps the event filters and the key log
// the last full frame's widgets left (Source.Replay, Gio patch 35: none ask
// in this one, and without it the focused composer lost the focus at every
// quiet frame and got it back at the next full one, a blur and a focus for
// plugins each time); the image cache counts that frame's images as drawn
// now (FrameRedrawn); and a wake-up it asked for that isn't due yet is
// asked for again (the window takes it with every frame). UI goroutine,
// frame lock held.
func (a *App) tryQuietFrame(now time.Time, size image.Point, m unit.Metric, insets app.Insets, asked bool, askedAt time.Time, src input.Source) bool {
	if !a.quietFrame(now, size, m, insets, asked, askedAt) {
		return false
	}
	src.Replay()
	if asked && askedAt.After(now) {
		src.Execute(op.InvalidateCmd{At: askedAt})
	}
	a.images.FrameRedrawn()
	a.quietFrames.Add(1)
	return true
}

// beginFullFrame says a full frame's layout starts: the images it Gets are
// the ones a quiet frame draws again (imgcache.FrameBegin).
func (a *App) beginFullFrame() {
	a.images.FrameBegin()
}

// noteFullFrame records what a full frame was drawn for; covered: only the
// boot cover went to the GPU, so its ops can't be shown again.
func (a *App) noteFullFrame(size image.Point, m unit.Metric, insets app.Insets, covered bool) {
	a.quiet = quietState{ok: !covered && a.login == nil && a.insp == nil, size: size, metric: m, insets: insets,
		zoom: a.zoomFactor(), subs: slackc.SubFrames()}
}

// QuietFrames counts the frames drawn again from the last full frame's ops
// so far (any goroutine; tests, CLACK_FRAMESTATS).
func (a *App) QuietFrames() uint64 { return a.quietFrames.Load() }
