//go:build linux || windows || (darwin && cgo)

package ui

import (
	"gioui.org/app"
	"gioui.org/gpu"
)

// The parts of TestRealWindowCaretCost that read Gio patch 45 (partial
// frames), apart so a run on code without it can stand them in.

// caretGPUFrame is what the GPU did for one of the window's frames.
type caretGPUFrame struct {
	partial  bool
	damagePx int64
	draws    int
	apiCalls int64
}

// caretGPUHook has f called for every frame a window draws (on the
// window's render goroutine); stop removes it.
func caretGPUHook(f func(caretGPUFrame)) (stop func()) {
	app.SetGPUFrameHook(func(s gpu.FrameStats) {
		f(caretGPUFrame{partial: s.Partial, damagePx: s.DamagePx, draws: s.Draws, apiCalls: s.APICalls})
	})
	return func() { app.SetGPUFrameHook(nil) }
}

// caretPartialFrames turns partial frames on or off (CLACK_FULL_FRAMES).
func caretPartialFrames(on bool) { gpu.ClackPartialFrames(on) }
