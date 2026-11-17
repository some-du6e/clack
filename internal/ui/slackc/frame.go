package slackc

import (
	"image/color"
	"slices"
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
	subFrames.Add(1)
	saved, savedSize, savedBg, savedClock, savedPane := stack, windowSize, subBg, curClock, curPane
	w.c.begin(now(gtx))
	curClock, curPane = &w.c, ""
	w.c.layers.begin(gtx)
	subBg = bg
	st := css.Initial(fg)
	root := &docEl{tag: "div", class: strings.Fields(classes)}
	stack = append(make([]stackEntry, 0, 8), stackEntry{el: root, style: &st, key: mix(hstr(fnvOff, classes) ^ uint64(fg.R)<<16 ^ uint64(fg.G)<<8 ^ uint64(fg.B))})
	windowSize, subFrame = gtx.Constraints.Max, true
	return func() {
		RouteEscape(gtx) // the window's Escape its layers get (layers.go)
		stack, windowSize, subFrame, subBg, curClock, curPane = saved, savedSize, false, savedBg, savedClock, savedPane
	}
}

// subFrames counts the frames SubWindows began (SubFrames).
var subFrames atomic.Uint64

// SubFrames is how many frames secondary windows have begun. A window
// that shows its last frame's ops again (ui's quiet frames) does so only
// while it hasn't changed: those ops may call a patched root's View that
// another window's frame drew anew since.
func SubFrames() uint64 { return subFrames.Load() }

// SetWake gives the window's frame a way to be asked for from any
// goroutine (the huddle window's invalidate): an Escape JS passed on to
// its layers asks for one.
func (w *SubWindow) SetWake(f func()) { w.c.wake = f }

// Close says the window is gone: its draws keep no root from then on. At
// once, not at another window's next sweep (a hidden or minimized main
// window draws no frames to sweep in, and the closed window's plugin roots
// kept running in JS), the roots no other window draws unmount and the
// rest forget the window's draws. The work waits for the frame lock on a
// goroutine of its own: a window's DestroyEvent must not wait for another
// window's frame. Any goroutine.
func (w *SubWindow) Close() {
	if w.c.closed.Swap(true) {
		return
	}
	closing.Add(1)
	go func() {
		defer closing.Done()
		LockFrame()
		defer UnlockFrame()
		sweepAll()
	}()
}

// closing counts the Close sweeps under way (tests wait for them).
var closing sync.WaitGroup

// A patched root goes (its View closed, its JS root unmounted) once no
// window draws it: every window that drew it has drawn staleFrames frames
// without it, staleAfter after that draw, or has closed. Counting each
// window's own frames is what keeps a root that's still on screen: a
// window that sat without a frame (unfocused, so no caret blinking) came
// back to every root unmounted and mounted again (plugin state lost, the
// native drawn until JS answered: Show Real User's names flashed back to
// the relay bot), and the huddle window's frames swept the still main
// window's roots, while slackc compared every root's last draw with
// whichever frame ran. Two windows can draw one root (HuddleMiniPanel is
// the main window's dock's and the presenter panel's): the panel closing
// leaves it the dock's. The time keeps a row scrolled away and back within
// seconds as it was.
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
	layers layerStack // its overlay layers (layers.go)
	wake   func()     // asks for a frame (nil: the main window's invalidate)
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

// drawRec is one window's latest draw of a root.
type drawRec struct {
	clock *frameClock
	frame uint64    // the clock's frame then
	at    time.Time // and its time
}

// stale reports whether the draw keeps its root no more: its window
// closed, or has drawn staleFrames frames without it, staleAfter after it.
func (d *drawRec) stale() bool {
	c := d.clock
	return c.closed.Load() || c.frame-d.frame >= staleFrames && c.now.Sub(d.at) > staleAfter
}

// drawn records that the window whose frame runs drew in, at t.
func (in *inst) drawn(t time.Time) {
	c := curClock
	for i := range in.draws {
		if d := &in.draws[i]; d.clock == c {
			d.frame, d.at = c.frame, t
			return
		}
	}
	in.draws = append(in.draws, drawRec{clock: c, frame: c.frame, at: t})
}

// forgetClosed drops the draws of windows that closed: a root two windows
// draw outlives one of them (HuddleMiniPanel in the dock, through screen
// share after screen share), and each closed window's record held on to
// its clock.
func (in *inst) forgetClosed() {
	in.draws = slices.DeleteFunc(in.draws, func(d drawRec) bool { return d.clock.closed.Load() })
}

// unused reports whether no window's draw keeps in.
func (in *inst) unused() bool {
	for i := range in.draws {
		if !in.draws[i].stale() {
			return false
		}
	}
	return true
}

// sweep unmounts the roots no window draws anymore: scrolled away, closed,
// or drawn only in windows that closed. Each draw ages by the frames of
// the window that made it (drawRec.stale), so any window's sweep can find
// a root none keeps, and none drops another window's.
func (c *frameClock) sweep() {
	if c.now.Sub(c.swept) < sweepEvery {
		return
	}
	c.swept = c.now
	sweepAll()
}

// sweepAll unmounts every root no window's draw keeps (frame lock).
func sweepAll() {
	b := bridge()
	for k, in := range insts {
		in.forgetClosed()
		if in.unused() {
			delete(insts, k)
			dropRoot(in, b)
		}
	}
}
