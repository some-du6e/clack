package ui

import (
	"os"
	"sync"
	"sync/atomic"

	"github.com/some-du6e/clack/internal/ui/uipref"
)

// Reduced motion: the introduction and the sign-in card only fade when the
// user asked for less motion, in the OS (macOS Accessibility > Display >
// Reduce motion, Windows' "Animation effects" off, GNOME's or the XDG
// portal's reduced-motion) or in Slack (Accessibility > "Allow
// animations" off, a11y_animations). The OS setting is read and watched
// off the UI goroutine (osmotion_*.go) into an atomic, like the colour
// scheme (osscheme.go); layout only loads it.

// osMotion: 0 not known (yet), 1 full motion, 2 reduced.
var osMotion atomic.Int32

var osMotionStart sync.Once

// setOSReduceMotion records a read. Any goroutine or OS thread (the macOS
// observer calls it on the main thread): an atomic store, nothing else.
func setOSReduceMotion(reduce bool) {
	n := int32(1)
	if reduce {
		n = 2
	}
	osMotion.Store(n)
}

// osReduceMotion is the OS setting as last read; ok is false until a
// watcher reported (or where the OS has none).
func osReduceMotion() (reduce, ok bool) {
	switch osMotion.Load() {
	case 1:
		return false, true
	case 2:
		return true, true
	}
	return false, false
}

// watchOSMotion starts the platform watcher once.
func watchOSMotion() { osMotionStart.Do(startOSMotionWatcher) }

// reduceMotion reports whether Clack's own animations should be fades
// only. CLACK_REDUCE_MOTION=1 forces it (tests, screenshots); =0 ignores
// the OS and Slack settings.
func (a *App) reduceMotion() bool {
	if a.motionOverride != 0 {
		return a.motionOverride > 0
	}
	switch os.Getenv("CLACK_REDUCE_MOTION") {
	case "1", "true":
		return true
	case "0", "false":
		return false
	}
	if r, ok := osReduceMotion(); ok && r {
		return true
	}
	return uipref.Get().NoAnimations
}
