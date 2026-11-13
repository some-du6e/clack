//go:build stockgio

package richview

import "gioui.org/op/clip"

// Stock Gio v0.10.3 can't size a path (gio_patch15.go): about 1 KB a
// glyph, a Lato outline's.
func pathBytes(p clip.PathSpec, glyphs int) int { return glyphs << 10 }
