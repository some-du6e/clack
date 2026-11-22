package composer

import "testing"

// setEditorTrailingSpace turns Windows' trailing whitespace on a double
// click's word (edSelectTrailingSpace) on or off for the test.
func setEditorTrailingSpace(t *testing.T, on bool) {
	t.Helper()
	old := edSelectTrailingSpace
	edSelectTrailingSpace = on
	t.Cleanup(func() { edSelectTrailingSpace = old })
}
