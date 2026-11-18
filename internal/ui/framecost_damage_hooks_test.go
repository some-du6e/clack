package ui

import (
	"fmt"

	"gioui.org/gpu"

	"github.com/some-du6e/clack/internal/imgcache"
)

// The parts of TestFrameCostDamage that read Gio patch 46 (partial frames),
// apart so a run on code without it can stand them in.

// imgStats is the image cache's stats.
type imgStats = imgcache.Stats

// damageModes sets how a headless window draws: whole frames, partial
// frames into it, or partial frames through the frame texture and its copy
// (a window's way), each mode from a fresh start.
func damageModes(partial, frameTex bool) {
	gpu.ClackPartialTextures(partial)
	gpu.ClackFrameTexture(frameTex)
	gpu.ClackPartialReset()
}

// damageWhole draws the frames whole while partial frames' state (the atlas
// places, the signatures) goes on: the whole frame a partial one must
// equal.
func damageWhole(on bool) { gpu.ClackPartialWhole(on) }

// damageOf is what a frame's stats say of its damage: whether it was
// partial, in how many rectangles, how many pixels.
func damageOf(s gpu.FrameStats) (partial bool, rects int, px int64) {
	return s.Partial, s.DamageRects, s.DamagePx
}

// damagePart describes a frame's damage.
func damagePart(s gpu.FrameStats) string {
	if !s.Partial {
		return "drawn whole"
	}
	return fmt.Sprintf("partial, %d px damaged in %d rects, %d px copied", s.DamagePx, s.DamageRects, s.CopyPx)
}

// damageTessellated is how many clip paths the frame tessellated (its
// buildVerts calls: Gio's CPU work for a path).
func damageTessellated(s gpu.FrameStats) int { return s.PathsTessellated }
