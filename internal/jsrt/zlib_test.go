package jsrt

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"strings"
	"testing"
)

func TestZlibOp(t *testing.T) {
	// made by Python's gzip module: another implementation's output inflates
	py, _ := base64.StdEncoding.DecodeString("H4sIAAAAAAACA0vMU8jPSVEozsxJzStRKMjPSwEA9skZVxIAAAA=")
	if out, err := zlibOp("d", "gzip", py); err != nil || string(out) != "an old silent pond" {
		t.Fatalf("inflate python gzip: %q %v", out, err)
	}
	in := []byte(strings.Repeat("splash silence again ", 1000))
	for _, f := range []string{"gzip", "deflate", "deflate-raw"} {
		z, err := zlibOp("c", f, in)
		if err != nil || len(z) == 0 || len(z) >= len(in) {
			t.Fatalf("%s compress: %d bytes, %v", f, len(z), err)
		}
		back, err := zlibOp("d", f, z)
		if err != nil || !bytes.Equal(back, in) {
			t.Fatalf("%s round trip: %v", f, err)
		}
	}
	if _, err := zlibOp("d", "gzip", []byte{1, 2, 3}); err == nil {
		t.Error("garbage inflated")
	}
	if _, err := zlibOp("c", "brotli", in); err == nil {
		t.Error("unknown format accepted")
	}
	// a bomb past maxInflate is refused, not buffered whole
	old := maxInflate
	maxInflate = 4 << 20
	defer func() { maxInflate = old }()
	var bomb bytes.Buffer
	w := gzip.NewWriter(&bomb)
	zero := make([]byte, 1<<20)
	for i := int64(0); i < maxInflate>>20+1; i++ {
		w.Write(zero)
	}
	w.Close()
	if _, err := zlibOp("d", "gzip", bomb.Bytes()); err == nil {
		t.Error("output cap not enforced")
	}
}
