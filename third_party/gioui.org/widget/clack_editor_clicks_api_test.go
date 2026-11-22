// SPDX-License-Identifier: Unlicense OR MIT

package widget

import "testing"

// setSelectTrailingSpace turns Windows' trailing whitespace on a double
// click's word (clackSelectTrailingSpace) on or off for the test.
func setSelectTrailingSpace(t *testing.T, on bool) {
	t.Helper()
	old := clackSelectTrailingSpace
	clackSelectTrailingSpace = on
	t.Cleanup(func() { clackSelectTrailingSpace = old })
}
