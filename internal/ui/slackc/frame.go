package slackc

import (
	"image/color"
	"strings"
	"sync"

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

var subFrame bool

// SubFrame starts a secondary window's frame (call it with the frame lock
// held): the element stack starts at that window's root element (classes,
// e.g. "p-huddle_window", and its text color fg, which plugin content
// inherits), and vw/vh/fixed positioning use this window's size; JS→Go
// callbacks stay queued for the main window. The returned func restores
// the main window's state.
func SubFrame(gtx layout.Context, classes string, fg color.NRGBA) (restore func()) {
	saved, savedSize := stack, windowSize
	st := css.Initial(fg)
	root := &docEl{tag: "div", class: strings.Fields(classes)}
	stack = append(make([]stackEntry, 0, 8), stackEntry{el: root, style: &st, key: mix(hstr(fnvOff, classes) ^ uint64(fg.R)<<16 ^ uint64(fg.G)<<8 ^ uint64(fg.B))})
	windowSize, subFrame = gtx.Constraints.Max, true
	return func() { stack, windowSize, subFrame = saved, savedSize, false }
}
