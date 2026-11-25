package jsrt

import (
	"bytes"
	"errors"
	"fmt"
	"testing"
)

// aesGCM checks the tag length itself, whoever calls it: only Web Crypto's
// lengths, never a 0-bit tag that would open anything unchecked or one
// past 128 bits that would slice out of range.
func TestAESGCMTagLengths(t *testing.T) {
	key, iv, data := make([]byte, 16), make([]byte, 12), []byte("hidden emoji")
	// a panic is this test's failure, not the whole package's
	gcm := func(seal bool, data []byte, bits int) (out []byte, err error) {
		defer func() {
			if p := recover(); p != nil {
				err = fmt.Errorf("panicked: %v", p)
			}
		}()
		return aesGCM(seal, key, iv, nil, data, bits)
	}
	for _, bits := range []int{0, 8, 31, 33, 100, 136, 256, -8} {
		var je *JSError
		if _, err := gcm(true, data, bits); !errors.As(err, &je) || je.Name != "OperationError" {
			t.Errorf("seal with a %d-bit tag: %v; want OperationError", bits, err)
		}
		if _, err := gcm(false, make([]byte, 64), bits); !errors.As(err, &je) || je.Name != "OperationError" {
			t.Errorf("open with a %d-bit tag: %v; want OperationError", bits, err)
		}
	}
	for _, bits := range []int{32, 64, 96, 104, 112, 120, 128} {
		ct, err := aesGCM(true, key, iv, []byte("aad"), data, bits)
		if err != nil || len(ct) != len(data)+bits/8 {
			t.Fatalf("%d-bit tag: %d bytes, %v", bits, len(ct), err)
		}
		pt, err := aesGCM(false, key, iv, []byte("aad"), ct, bits)
		if err != nil || !bytes.Equal(pt, data) {
			t.Errorf("%d-bit tag: opened %q, %v", bits, pt, err)
		}
		ct[len(ct)-1] ^= 1
		if _, err := aesGCM(false, key, iv, []byte("aad"), ct, bits); err == nil {
			t.Errorf("%d-bit tag: a bent tag opened", bits)
		}
	}
}
