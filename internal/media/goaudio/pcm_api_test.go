package goaudio

import "testing"

// Decode's cap, as the tests reach it (pcm_test.go, fuzz_test.go).

// decodeCap is the cap Decode holds to.
func decodeCap() int64 { return maxDecodeSamples }

// setDecodeCap is Decode's cap until the test ends.
func setDecodeCap(t *testing.T, n int64) {
	old := maxDecodeSamples
	maxDecodeSamples = n
	t.Cleanup(func() { maxDecodeSamples = old })
}
