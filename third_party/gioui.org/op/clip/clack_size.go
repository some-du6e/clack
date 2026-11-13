// SPDX-License-Identifier: Unlicense OR MIT

package clip

// Size is roughly how much memory p's recorded path holds (Clack patch
// 15, CLACK_PATCHES.md). text.Shaper.Shape records each outline into an
// Ops list of its own, so for those it is the outline's whole cost.
func (p PathSpec) Size() int { return p.spec.Size() }
