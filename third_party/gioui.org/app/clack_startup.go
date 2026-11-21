package app

import (
	"sync/atomic"
	"time"
)

// Clack patch (CLACK_PATCHES.md §56): the driver's startup points, for
// the app's startup timeline. Each point is reported once per process,
// the first time any window reaches it:
//
//	gio-init      Window.init done (the fallback decorations measured)
//	appkit-app    macOS: NSApplication made, its delegate set
//	appkit-run    macOS: [NSApp run] about to start
//	will-finish-launching  macOS: applicationWillFinishLaunching
//	launched      macOS: NSApplication finished launching (the Dock
//	              stops bouncing the icon)
//	activated     macOS: the delegate's applicationDidFinishLaunching made
//	              the app regular and active; windows asked for during the
//	              launch open right after
//	window-create macOS: the main thread starts making the window
//	window-view   macOS: its view (the Metal layer, the display link)
//	window-ns     macOS: its NSWindow
//	window-shown  the OS window is on screen: makeKeyAndOrderFront
//	              (macOS), ShowWindow (Windows), XMapWindow (X11) returned
//	gpu-context   the first GPU context (device, queue, surface) made
//	gpu           the first gpu.New (pipelines, shaders) done
//	presented     the first frame's Present returned
//
// The hook runs on whatever thread reached the point, the AppKit main
// thread included, so it must not block.

var startupHook atomic.Pointer[func(name string, at time.Time)]

var quitHook atomic.Pointer[func()]

// SetQuitHandler makes f the macOS stub menu's Quit (Cmd+Q until an app
// installs a menu of its own), run on its own goroutine; nil, the default,
// is NSApplication's terminate:, which exits at once. Elsewhere a no-op.
func SetQuitHandler(f func()) {
	if f == nil {
		quitHook.Store(nil)
		return
	}
	quitHook.Store(&f)
}

// SetStartupMark makes f hear the startup points (nil: none). Set it
// before the first window (and before Main or Events, for launched).
func SetStartupMark(f func(name string, at time.Time)) {
	if f == nil {
		startupHook.Store(nil)
		return
	}
	startupHook.Store(&f)
}

type startupPoint struct {
	name string
	done atomic.Bool
}

func (p *startupPoint) mark() {
	if p.done.Load() || !p.done.CompareAndSwap(false, true) {
		return
	}
	if f := startupHook.Load(); f != nil {
		(*f)(p.name, time.Now())
	}
}

var (
	startupGioInit      = &startupPoint{name: "gio-init"}
	startupAppKitApp    = &startupPoint{name: "appkit-app"}
	startupAppKitRun    = &startupPoint{name: "appkit-run"}
	startupWillFinish   = &startupPoint{name: "will-finish-launching"}
	startupLaunched     = &startupPoint{name: "launched"}
	startupActivated    = &startupPoint{name: "activated"}
	startupWindowCreate = &startupPoint{name: "window-create"}
	startupWindowView   = &startupPoint{name: "window-view"}
	startupWindowNS     = &startupPoint{name: "window-ns"}
	startupWindowShown  = &startupPoint{name: "window-shown"}
	startupGPUContext   = &startupPoint{name: "gpu-context"}
	startupGPU          = &startupPoint{name: "gpu"}
	startupPresented    = &startupPoint{name: "presented"}
)
