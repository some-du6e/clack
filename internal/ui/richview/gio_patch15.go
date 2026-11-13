//go:build !stockgio

package richview

import "gioui.org/op/clip"

// pathBytes is the memory a word's outline holds (Gio patch 15,
// PathSpec.Size).
func pathBytes(p clip.PathSpec, glyphs int) int { return p.Size() }
