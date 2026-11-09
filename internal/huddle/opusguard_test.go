package huddle

import (
	"math"
	"testing"
)

// TestOpusRoundTrip: the fake's Opus (VoIP, mono 48 kHz, the 440 Hz tone
// device) decodes with the receive side's stereo decoder. Isolates the
// codec from WebRTC (it aborted on darwin/arm64 in CI).
func TestOpusRoundTrip(t *testing.T) {
	enc, err := newOpusEnc()
	if err != nil {
		t.Fatal(err)
	}
	dec, err := newOpusDec()
	if err != nil {
		t.Fatal(err)
	}
	frames := make(chan []int16, 200)
	tone := NewToneDevice(660).(*toneDevice)
	// render 100 frames synchronously, as the tone device would
	mic := make([]int16, frameSamples)
	n := 0
	for f := 0; f < 100; f++ {
		for i := range mic {
			ph := float64(n) / sampleRate
			mic[i] = int16(0.25 * 32767 * math.Sin(2*math.Pi*tone.hz*ph))
			n++
		}
		frames <- append([]int16(nil), mic...)
	}
	close(frames)
	buf := make([]byte, 0, 1500)
	var peak float64
	i := 0
	for p := range frames {
		pkt, err := enc.encode(p, buf)
		if err != nil {
			t.Fatalf("encode %d: %v", i, err)
		}
		pcm, err := dec.decode(pkt)
		if err != nil {
			t.Fatalf("decode %d (%d bytes, TOC %#x): %v", i, len(pkt), pkt[0], err)
		}
		if lv := level(pcm); lv > peak {
			peak = lv
		}
		i++
	}
	if peak < 0.05 {
		t.Errorf("decoded peak level %.3f", peak)
	}
	t.Logf("100 frames round trip, peak %.3f", peak)
}
