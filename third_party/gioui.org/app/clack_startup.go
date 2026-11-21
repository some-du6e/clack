package app

import (
	"sync/atomic"
	"time"
)

// Clack patch (CLACK_PATCHES.md §55): the driver's startup points, for
// the app's startup timeline. Each point is reported once per process,
// the first time any window reaches it:
//
//	gio-init      Window.init done (the fallback decorations measured)
//	launched      macOS: NSApplication finished launching (the Dock
//	              stops bouncing the icon)
//	window-shown  the OS window is on screen: makeKeyAndOrderFront
//	              (macOS), ShowWindow (Windows), XMapWindow (X11) returned
//	gpu-context   the first GPU context (device, queue, surface) made
//	gpu           the first gpu.New (pipelines, shaders) done
//	presented     the first frame's Present returned
//
// The hook runs on whatever thread reached the point, the AppKit main
// thread included, so it must not block.

var startupHook atomic.Pointer[func(name string, at time.Time)]

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
	startupGioInit     = &startupPoint{name: "gio-init"}
	startupLaunched    = &startupPoint{name: "launched"}
	startupWindowShown = &startupPoint{name: "window-shown"}
	startupGPUContext  = &startupPoint{name: "gpu-context"}
	startupGPU         = &startupPoint{name: "gpu"}
	startupPresented   = &startupPoint{name: "presented"}
)
