package slackc

import (
	"image/color"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/ui/slackc/css"
)

// slackc's patched path keeps per-frame state for one UI goroutine at a
// time: the instance map, the element stack, the render context and the
// window size. Clack's main window and its huddle call window run their
// own event loops, so every window that draws patch points holds the
// frame lock for its whole frame. Uncontended it's one mutex per frame;
// the unpatched fast paths never touch it.
var frameMu sync.Mutex

// LockFrame / UnlockFrame bracket a window's frame (the ui root's main
// window and huddleui's call window).
func LockFrame()   { frameMu.Lock() }
func UnlockFrame() { frameMu.Unlock() }

var (
	subFrame bool
	subBg    color.NRGBA // the secondary window's background (SubWindow.FrameBg; zero: the theme's)
)

// A SubWindow is a secondary window that draws patch points: the huddle
// call window, the screen share's presenter panel. Each keeps its own
// frame clock, so its patched roots age only by its own frames (see
// frameClock). The zero value is ready; use one per window (never
// copied), and Close it when the window goes.
type SubWindow struct{ c frameClock }

// Frame starts the window's frame (call it with the frame lock held): the
// element stack starts at that window's root element (classes, e.g.
// "p-huddle_window", and its text color fg, which plugin content
// inherits), and vw/vh/fixed positioning use this window's size; JS→Go
// callbacks stay queued for the main window. The returned func restores
// the main window's state.
func (w *SubWindow) Frame(gtx layout.Context, classes string, fg color.NRGBA) (restore func()) {
	return w.FrameBg(gtx, classes, fg, color.NRGBA{})
}

// FrameBg is Frame for a window whose background isn't the theme's (the
// huddle window's dark frame): translucent plugin colors composite over
// bg (srgb.go).
func (w *SubWindow) FrameBg(gtx layout.Context, classes string, fg, bg color.NRGBA) (restore func()) {
	saved, savedSize, savedBg, savedClock := stack, windowSize, subBg, curClock
	w.c.begin(now(gtx))
	curClock = &w.c
	subBg = bg
	st := css.Initial(fg)
	root := &docEl{tag: "div", class: strings.Fields(classes)}
	stack = append(make([]stackEntry, 0, 8), stackEntry{el: root, style: &st, key: mix(hstr(fnvOff, classes) ^ uint64(fg.R)<<16 ^ uint64(fg.G)<<8 ^ uint64(fg.B))})
	windowSize, subFrame = gtx.Constraints.Max, true
	return func() { stack, windowSize, subFrame, subBg, curClock = saved, savedSize, false, savedBg, savedClock }
}

// Close says the window is gone: the next sweep (any window's frame)
// unmounts the roots it drew last. Any goroutine, no frame lock: a
// window's DestroyEvent must not wait for another window's frame.
func (w *SubWindow) Close() { w.c.closed.Store(true) }

// A patched root goes (its View closed, its JS root unmounted) once the
// window that drew it last has drawn staleFrames frames without it and
// staleAfter has passed since that draw. Counting the window's own frames
// is what keeps a root that's still on screen: a window that sat without
// a frame (unfocused, so no caret blinking) came back to every root
// unmounted and mounted again (plugin state lost, the native drawn until
// JS answered: Show Real User's names flashed back to the relay bot), and
// the huddle window's frames swept the still main window's roots, while
// slackc compared every root's last draw with whichever frame ran. The
// time keeps a row scrolled away and back within seconds as it was.
const (
	staleFrames = 3
	staleAfter  = 5 * time.Second
	sweepEvery  = 2 * time.Second // between one window's sweeps
)

// frameClock counts the frames of one window that draws patch points:
// the ui root's main window (Frame) or a SubWindow.
type frameClock struct {
	frame  uint64    // frames begun
	now    time.Time // the time of the frame in progress, and of the last one between frames
	swept  time.Time // its last sweep (now then)
	closed atomic.Bool
}

var (
	mainClock frameClock
	// curClock is the clock of the window whose frame runs (frame lock):
	// the main window's but while a SubWindow's frame runs
	curClock = &mainClock
)

// begin starts the clock's next frame, at t. First its roots that the
// frames so far left undrawn unmount: the sweep runs after the last
// frame's draws and before this one's, so a root that frame drew isn't
// stale however long the window then went without one.
func (c *frameClock) begin(t time.Time) {
	c.sweep()
	c.frame++
	c.now = t
}

// sweep unmounts the roots c's window stopped drawing (scrolled away,
// closed) and those of windows that closed.
func (c *frameClock) sweep() {
	if c.now.Sub(c.swept) < sweepEvery {
		return
	}
	c.swept = c.now
	b := bridge()
	for k, in := range insts {
		gone := in.clock != nil && in.clock.closed.Load()
		if gone || in.clock == c && c.frame-in.frame >= staleFrames && c.now.Sub(in.used) > staleAfter {
			delete(insts, k)
			dropRoot(in, b)
		}
	}
}
