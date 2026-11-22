package richview

import "testing"

// setTrailingSpace turns Windows' trailing whitespace on a double click's
// word (SelectTrailingSpace) on or off for the test.
func setTrailingSpace(t *testing.T, on bool) {
	t.Helper()
	old := SelectTrailingSpace
	SelectTrailingSpace = on
	t.Cleanup(func() { SelectTrailingSpace = old })
}
