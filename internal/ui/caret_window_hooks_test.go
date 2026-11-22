//go:build linux || windows || (darwin && cgo)

package ui

import (
	"gioui.org/app"
	"gioui.org/gpu"
)

// The parts of TestRealWindowCaretCost that read Gio patch 46 (partial
// frames), apart so a run on code without it can stand them in.

// caretGPUFrame is what the GPU did for one of the window's frames.
type caretGPUFrame struct {
	partial  bool
	damagePx int64
	draws    int
	apiCalls int64
	copyPx   int64 // the frame texture's copy into the window
	// the window read back after the copy (caretVerifyCopy), and the
	// pixels of it that differed from the frame texture's
	copyChecked    bool
	copyMismatchPx int64
	// externals are the external images' paints the frame asked for a
	// texture (Gio patch 51), wraps the textures it wrapped
	externals, wraps int
	// damageRects are a partial frame's damage rectangles
	damageRects int
	atlasPx     int64  // the coverage atlas's pages
	whole       string // why a frame was drawn whole (gpu.FrameStats.Whole)
}

// caretGPUHook has f called for every frame a window draws (on the
// window's render goroutine); stop removes it.
func caretGPUHook(f func(caretGPUFrame)) (stop func()) {
	app.SetGPUFrameHook(func(s gpu.FrameStats) {
		copyPx := s.CopyPx // less the clear colour a partial frame copies into its damage
		if s.Partial {
			copyPx -= s.DamagePx
		}
		f(caretGPUFrame{partial: s.Partial, damagePx: s.DamagePx, draws: s.Draws, apiCalls: s.APICalls, copyPx: copyPx,
			copyChecked: s.CopyChecked, copyMismatchPx: s.CopyMismatchPx, externals: s.Externals, wraps: s.ExternalWraps, damageRects: s.DamageRects, atlasPx: s.AtlasPx, whole: s.Whole})
	})
	return func() { app.SetGPUFrameHook(nil) }
}

// caretPartialFrames turns partial frames on or off (CLACK_FULL_FRAMES).
func caretPartialFrames(on bool) { gpu.ClackPartialFrames(on) }

// caretWhole draws every frame whole while partial frames' state goes on
// (gpu.ClackPartialWhole): the whole frame a partial one must equal.
func caretWhole(on bool) { gpu.ClackPartialWhole(on) }

// caretVerifyCopy has the window's GPU read it back after each copy of the
// frame texture into it (gpu.ClackVerifyCopy).
func caretVerifyCopy(on bool) { gpu.ClackVerifyCopy(on) }
