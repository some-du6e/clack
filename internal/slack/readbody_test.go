package slack

import (
	"bytes"
	"errors"
	"io"
	"testing"
	"testing/iotest"
)

// TestReadBodyAsReadAll: readBody reads what io.ReadAll reads, whatever
// the length, the hint (right, wrong, none) and how the reader hands the
// bytes over, and stops with the reader's error and what came before it.
func TestReadBodyAsReadAll(t *testing.T) {
	body := func(n int) []byte {
		b := make([]byte, n)
		for i := range b {
			b[i] = byte('a' + i*7%26)
		}
		return b
	}
	for _, n := range []int{0, 1, 511, 8 << 10, 8<<10 + 1, 100_000, 3 << 20, 9<<20 + 17} {
		want := body(n)
		for _, hint := range []int64{-1, int64(n), int64(n) / 3, int64(n) * 2} {
			for name, wrap := range map[string]func(io.Reader) io.Reader{
				"whole": func(r io.Reader) io.Reader { return r },
				"half":  iotest.HalfReader,
				"bytes": iotest.OneByteReader,
				"eof":   iotest.DataErrReader,
			} {
				if name == "bytes" && n > 100_000 {
					continue
				}
				got, err := readBody(wrap(bytes.NewReader(want)), hint)
				if err != nil || !bytes.Equal(got, want) || got == nil {
					t.Fatalf("n %d hint %d %s: %d bytes, err %v (nil %v)", n, hint, name, len(got), err, got == nil)
				}
				if cap(got) > 2*len(got)+8<<10 {
					t.Errorf("n %d hint %d %s: holds a cap of %d for %d bytes", n, hint, name, cap(got), len(got))
				}
			}
		}
	}
	boom := errors.New("synthetic cut")
	for _, n := range []int{0, 5000, 50_000, 5 << 20} {
		want := body(n)
		got, err := readBody(io.MultiReader(bytes.NewReader(want), iotest.ErrReader(boom)), -1)
		if !errors.Is(err, boom) || !bytes.Equal(got, want) {
			t.Errorf("n %d cut: %d bytes, err %v", n, len(got), err)
		}
	}
}
