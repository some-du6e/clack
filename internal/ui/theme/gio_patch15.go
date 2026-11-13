//go:build !stockgio

package theme

import "gioui.org/op/clip"

// PathBytes is roughly the memory an outline Shape or EmboldenedShape
// made holds (Gio patch 15, PathSpec.Size): the shaped-word and
// emboldened-run caches budget with it.
func PathBytes(p clip.PathSpec, glyphs int) int { return p.Size() }
