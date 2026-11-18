// Package inputidle tells how long ago the person last gave Clack input
// (the pointer moving, pressing or dragging, a wheel or trackpad scroll, a
// key, text), for work that should wait for a pause in it: cmd/clack's
// forced collections, whose stop-the-world phases and full scavenge would
// land in the middle of a scroll or of keyboard navigation.
//
// The clock is Gio's input router's (Clack's Gio patch 33): every event a
// window's router is queued counts, whichever widget takes it, focused
// list or text field, a grabbed drag, a popover drawn with op.Defer, and
// in any window. Frames don't count: a focused composer's caret redraws
// the window twice a second for as long as it's focused.
package inputidle

import "time"

// Since is how long ago the last input was (how long the process has run,
// when there was none), on the monotonic clock. Any goroutine.
func Since() time.Duration { return routerSince() }

// Presses counts the pointer presses, key presses and text edits Clack's
// windows were given (Gio patch 33's count): the person acted between two
// reads when it moved. Moves, scrolls and releases aren't acts. Any
// goroutine.
func Presses() uint64 { return routerPresses() }
