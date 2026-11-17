// SPDX-License-Identifier: Unlicense OR MIT

package gpu

// Red evidence only (branch damage-redraw-red1, never merged): the API of
// Clack patch 45 on the renderer from before it, so its tests build and run
// against frames drawn whole, as this renderer draws every frame. Each
// switch does nothing; FrameStats' partial fields stay zero.

func ClackPartialFrames(on bool)   {}
func ClackPartialTextures(on bool) {}
func ClackFrameTexture(on bool)    {}
func ClackPartialWhole(on bool)    {}
func ClackPartialReset()           {}
