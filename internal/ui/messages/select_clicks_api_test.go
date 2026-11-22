package messages

import (
	"testing"

	"github.com/some-du6e/clack/internal/ui/richview"
)

// setTrailingSpace turns Windows' trailing whitespace on a double click's
// word (richview.SelectTrailingSpace) on or off for the test.
func setTrailingSpace(t *testing.T, on bool) {
	t.Helper()
	old := richview.SelectTrailingSpace
	richview.SelectTrailingSpace = on
	t.Cleanup(func() { richview.SelectTrailingSpace = old })
}
