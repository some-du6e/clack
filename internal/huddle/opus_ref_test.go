package huddle

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"runtime"
	"testing"
)

// The Opus codec is libopus transpiled to Go (kazzmir/opus-go), generated
// on linux/amd64. testdata/opus-ref-amd64.bin holds what it encodes there
// for refPCM, so other platforms (the macOS arm64 job) can check that they
// decode those packets without tripping libopus's assertions, and whether
// they encode the same bytes. CLACK_OPUS_REF=write regenerates it (on
// linux/amd64 only). The decoded PCM isn't compared: the transpiled
// decoder's output varies a little from process to process even on amd64.

const refFrames = 150 // 3 s

// refPCM is frame i of a speech-like test signal: a 220 Hz tone with
// harmonics and a 4 Hz envelope, plus a silent stretch (DTX/CNG paths).
func refPCM(i int) []int16 {
	pcm := make([]int16, frameSamples)
	for j := range pcm {
		n := i*frameSamples + j
		ts := float64(n) / sampleRate
		env := 0.5 + 0.5*math.Sin(2*math.Pi*4*ts)
		if i >= 100 && i < 120 {
			env = 0
		}
		v := 0.3*math.Sin(2*math.Pi*220*ts) + 0.15*math.Sin(2*math.Pi*440*ts) + 0.07*math.Sin(2*math.Pi*1320*ts)
		pcm[j] = int16(8000 * env * v / 0.52)
	}
	return pcm
}

type opusRef struct{ pkts [][]byte }

func encodeRef(t *testing.T) [][]byte {
	enc, err := newOpusEnc()
	if err != nil {
		t.Fatal(err)
	}
	var out [][]byte
	for i := 0; i < refFrames; i++ {
		pkt, err := enc.encode(refPCM(i), make([]byte, 0, 1500))
		if err != nil {
			t.Fatalf("encode %d: %v", i, err)
		}
		out = append(out, append([]byte(nil), pkt...))
	}
	return out
}

// decodeRef decodes pkts with a fresh decoder and returns the loudest
// frame's level; any error (a recovered libopus assertion) fails t.
func decodeRef(t *testing.T, pkts [][]byte) float64 {
	t.Helper()
	dec, err := newOpusDec()
	if err != nil {
		t.Fatal(err)
	}
	peak := 0.0
	for i, p := range pkts {
		pcm, err := dec.decode(p)
		if err != nil {
			t.Fatalf("%s/%s: decode packet %d (%s): %v", runtime.GOOS, runtime.GOARCH, i, describe(p), err)
		}
		peak = max(peak, level(pcm))
	}
	return peak
}

func readRef(t *testing.T) opusRef {
	b, err := os.ReadFile("testdata/opus-ref-amd64.bin")
	if err != nil {
		t.Fatal(err)
	}
	var r opusRef
	for len(b) > 0 {
		n := int(binary.BigEndian.Uint16(b))
		r.pkts = append(r.pkts, b[2:2+n])
		b = b[2+n:]
	}
	return r
}

func TestOpusMatchesAMD64(t *testing.T) {
	if os.Getenv("CLACK_OPUS_REF") == "write" {
		if runtime.GOOS+"/"+runtime.GOARCH != "linux/amd64" {
			t.Fatal("write the reference on linux/amd64")
		}
		pkts := encodeRef(t)
		var buf bytes.Buffer
		for _, p := range pkts {
			_ = binary.Write(&buf, binary.BigEndian, uint16(len(p)))
			buf.Write(p)
		}
		if err := os.WriteFile("testdata/opus-ref-amd64.bin", buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	ref := readRef(t)
	if len(ref.pkts) != refFrames {
		t.Fatalf("reference has %d packets", len(ref.pkts))
	}
	// 1. amd64's packets decode here
	if peak := decodeRef(t, ref.pkts); peak < 0.05 {
		t.Errorf("amd64's packets decode to level %.3f", peak)
	}
	// 2. this platform encodes the same packets (logged: a different
	// float rounding, e.g. arm64's fused multiply-add, may legitimately
	// change them; 3. checks they still decode)
	got := encodeRef(t)
	diff := 0
	first := -1
	for i := range got {
		if !bytes.Equal(got[i], ref.pkts[i]) {
			diff++
			if first < 0 {
				first = i
			}
		}
	}
	if diff > 0 {
		t.Logf("%s/%s encodes %d of %d frames differently from amd64 (first: frame %d, %s vs %s)", runtime.GOOS, runtime.GOARCH,
			diff, len(got), first, describe(got[first]), describe(ref.pkts[first]))
	}
	// 3. and decodes its own packets, on amd64's decoder too (the file)
	if peak := decodeRef(t, got); peak < 0.05 {
		t.Errorf("own packets decode to level %.3f", peak)
	}
	t.Logf("%s/%s: %d of %d frames encode like amd64", runtime.GOOS, runtime.GOARCH, len(got)-diff, len(got))
}

// A packet that trips a libopus assertion must not take the app down.
func TestOpusDecodeRecovers(t *testing.T) {
	dec, err := newOpusDec()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range [][]byte{{0xff, 0xff, 0xff}, {0x0b, 0xff, 0x00, 0x13, 0x37}, make([]byte, 1275)} {
		_, _ = dec.decode(p) // an error or noise, but no panic
	}
	ref := readRef(t)
	for i, p := range ref.pkts[:20] {
		if _, err := dec.decode(p); err != nil {
			t.Fatalf("packet %d after junk: %v", i, err)
		}
	}
}

func describe(p []byte) string {
	if len(p) == 0 {
		return "empty"
	}
	return fmt.Sprintf("%d bytes toc %#02x", len(p), p[0])
}
