// SPDX-License-Identifier: Unlicense OR MIT

package ops

// Size is roughly how much memory o holds: its data and reference arrays'
// capacity (Clack patch 15, CLACK_PATCHES.md).
func (o *Ops) Size() int {
	if o == nil {
		return 0
	}
	return cap(o.data) + 16*cap(o.refs) + 16*cap(o.stringRefs)
}
