// SPDX-License-Identifier: Unlicense OR MIT

package op

// Size is roughly how much memory the Ops list c was recorded in holds
// (Clack patch 16, CLACK_PATCHES.md). A CallOp whose list is its own,
// such as a text.Shaper.Shape outline, costs that much.
func (c CallOp) Size() int { return c.ops.Size() }
