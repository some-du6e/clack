package imgcache

// Test helpers for the budgeted index and decoder (decmem.go): tests that
// aren't about memory index and decode with no limit, and fail at once on
// an error that shouldn't happen.

import (
	"testing"

	"github.com/some-du6e/clack/internal/imgcache/internal/budget"
)

func testIndexGIF(b []byte) (*gifIndex, error)  { return indexGIF(b, budget.Unlimited()) }
func testIndexWebP(b []byte) (*gifIndex, error) { return indexWebP(b, budget.Unlimited()) }
func testIndexAnim(b []byte) (*gifIndex, error) { return indexAnim(b, budget.Unlimited()) }

// testDecoder is a decoder of ix with no memory limit.
func testDecoder(t testing.TB, ix *gifIndex, data []byte) *testGIFDecoder {
	t.Helper()
	d, err := newGIFDecoder(ix, data, budget.Unlimited())
	if err != nil {
		t.Fatalf("newGIFDecoder: %v", err)
	}
	return &testGIFDecoder{gifDecoder: d, t: t}
}

// testGIFDecoder is a gifDecoder whose next1 composes the next frame,
// failing the test on an error (a budget's: there is none).
type testGIFDecoder struct {
	*gifDecoder
	t testing.TB
}

func (d *testGIFDecoder) next1() int {
	d.t.Helper()
	i, err := d.compose()
	if err != nil {
		d.t.Fatalf("compose frame %d: %v", i, err)
	}
	return i
}
