package huddle

import "testing"

// The codec goroutine's stack never moves (codecstack.go): a second of the
// huddle's audio each way, and the probe the goroutine checks after every
// call never saw its stack anywhere else.
func TestCodecGoroutineStackStill(t *testing.T) {
	enc, err := newOpusEnc()
	if err != nil {
		t.Fatal(err)
	}
	dec, err := newOpusDec()
	if err != nil {
		t.Fatal(err)
	}
	m0, c0 := codecStackMoves()
	for _, f := range speechFrames(1.0, -24) {
		pkt, err := enc.encode(f, make([]byte, 0, 1500))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := dec.decode(pkt); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := dec.decode(nil); err != nil { // packet-loss concealment
		t.Fatal(err)
	}
	m1, c1 := codecStackMoves()
	if c1-c0 < 101 {
		t.Fatalf("%d codec calls ran on the codec goroutine, want every encode and decode (≥ 101)", c1-c0)
	}
	if m1 != m0 {
		t.Fatalf("the codec goroutine's stack moved during %d of %d calls", m1-m0, c1-c0)
	}
	t.Logf("%d codec calls, the codec goroutine's stack never moved (%d moves in the process)", c1-c0, m1)
}
