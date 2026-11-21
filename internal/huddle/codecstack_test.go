package huddle

import (
	"fmt"
	"math"
	"runtime"
	"slices"
	"testing"

	opus "github.com/kazzmir/opus-go/opus"
)

// The transpiled libopus keeps Go stack addresses as uintptr
// (codecstack.go): when the goroutine running a codec call has its stack
// grown mid-call, the call writes into the old copy and the frame comes
// out wrong. These tests run each call on a fresh goroutine whose stack is
// already used to a different depth, so the call chain crosses the stack's
// end (and the runtime copies it) at a different point each time, and
// compare every frame with the same packets decoded (or the same frames
// encoded) on a stack that never moves. libopus is deterministic: any
// difference is the stack move, heard as static.

const (
	stackSweep = 360 // eatStack depths tried (~100 bytes each: 0–36 KiB)
	stackStep  = 3
)

// eatStack uses about n × 100 bytes of the goroutine's stack, then runs f.
//
//go:noinline
func eatStack(n int, f func()) byte {
	var pad [64]byte
	pad[n%len(pad)] = byte(n)
	var k byte
	if n > 0 {
		k = eatStack(n-1, f)
	} else {
		f()
	}
	return k ^ pad[(n*7)%len(pad)]
}

// atDepth runs f on a fresh goroutine at eatStack depth n and waits.
func atDepth(n int, f func()) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		eatStack(n, f)
	}()
	<-done
}

// onStillStack runs f on a goroutine whose stack is grown first and kept
// in use (pinStack's trick, inlined here so the reference never depends
// on the code under test), so f's calls never move it: ~150 KiB in use of
// 256 KiB, ~100 KiB free for a codec call, and over a quarter in use, so
// the GC never shrinks it. Then two collections, so new goroutines start
// on stacks sized by the usual goroutines again (the runtime starts them
// at the average size the last collection saw, and this one is big).
func onStillStack(f func()) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		eatStack(1500, f)
	}()
	<-done
	runtime.GC()
	runtime.GC()
}

// speechFrames is a speech-like signal (synthSpeech) at rmsDB, in 20 ms
// mono frames.
func speechFrames(seconds, rmsDB float64) [][]int16 {
	x, _ := synthSpeech(seconds, rmsDB)
	var out [][]int16
	for off := 0; off+frameSamples <= len(x); off += frameSamples {
		f := make([]int16, frameSamples)
		for i := range f {
			f[i] = clamp16(float32(x[off+i]))
		}
		out = append(out, f)
	}
	return out
}

// stackTestPackets are what the huddle sends (Clack's encoder: VoIP,
// 32 kb/s, mono: SILK and hybrid frames for speech) and stereo music-mode
// CELT frames (a screen share's system audio), encoded on a still stack.
func stackTestPackets(t *testing.T) [][]byte {
	t.Helper()
	frames := speechFrames(1.0, -24)
	var pkts [][]byte
	var fail error
	onStillStack(func() {
		enc, err := newOpusEnc()
		if err != nil {
			fail = err
			return
		}
		for _, f := range frames {
			pkt, err := enc.encode(f, make([]byte, 0, 1500))
			if err != nil {
				fail = err
				return
			}
			pkts = append(pkts, slices.Clone(pkt))
		}
		st, err := opus.NewEncoder(sampleRate, 2, opus.ApplicationAudio)
		if err != nil {
			fail = err
			return
		}
		_ = st.SetBitrate(96000)
		for i := 0; i < 20; i++ {
			pcm := make([]int16, frameSamples*2)
			for j := 0; j < frameSamples; j++ {
				ts := float64(i*frameSamples+j) / sampleRate
				pcm[2*j] = int16(9000 * math.Sin(2*math.Pi*440*ts) * (0.6 + 0.4*math.Sin(2*math.Pi*3*ts)))
				pcm[2*j+1] = int16(7000 * math.Sin(2*math.Pi*660*ts+1))
			}
			out := make([]byte, 1500)
			n, err := st.Encode(pcm, frameSamples, out)
			if err != nil {
				fail = err
				return
			}
			pkts = append(pkts, out[:n])
		}
	})
	if fail != nil {
		t.Fatal(fail)
	}
	return pkts
}

// TestOpusDecodeIgnoresCallerStack: Clack's decoder (opusDec, what plays
// the huddle) gives the same frames whatever the calling goroutine's
// stack is doing.
func TestOpusDecodeIgnoresCallerStack(t *testing.T) {
	pkts := stackTestPackets(t)
	ref := make([][]int16, len(pkts))
	var fail error
	onStillStack(func() {
		dec, err := newOpusDec()
		if err != nil {
			fail = err
			return
		}
		for i, p := range pkts {
			pcm, err := dec.decode(p)
			if err != nil {
				fail = fmt.Errorf("packet %d: %v", i, err)
				return
			}
			ref[i] = slices.Clone(pcm)
		}
	})
	if fail != nil {
		t.Fatal(fail)
	}
	runs, bad := 0, 0
	var first string
	var worst float64 // the noise a bad frame carries, dBFS
	worst = minDBFS
	for depth := 0; depth < stackSweep; depth += stackStep {
		dec, err := newOpusDec()
		if err != nil {
			t.Fatal(err)
		}
		runs++
		for i, p := range pkts {
			var got []int16
			var derr error
			atDepth(depth, func() {
				pcm, err := dec.decode(p)
				got, derr = slices.Clone(pcm), err
			})
			if derr == nil && slices.Equal(got, ref[i]) {
				continue
			}
			bad++
			e := frameError(got, ref[i])
			worst = max(worst, e)
			if first == "" {
				first = fmt.Sprintf("depth %d, packet %d (%d bytes, toc %#02x): err %v, %d samples vs %d, difference %.1f dBFS",
					depth, i, len(p), p[0], derr, len(got), len(ref[i]), e)
			}
			break // the decoder's state has gone its own way: next depth
		}
	}
	if bad > 0 {
		t.Fatalf("%d of %d decoders gave a different frame when the caller's goroutine stack moved mid-call (first: %s; worst difference %.1f dBFS)",
			bad, runs, first, worst)
	}
	t.Logf("%d decoders × %d packets decoded at %d stack depths: every frame as on a still stack", runs, len(pkts), runs)
}

// TestOpusEncodeIgnoresCallerStack: the same for opusEnc, the huddle's
// microphone encoder, with the frame and the packet buffer on the caller's
// stack (the screen share's system audio passes a buffer like that).
func TestOpusEncodeIgnoresCallerStack(t *testing.T) {
	frames := speechFrames(0.8, -24)
	ref := make([][]byte, len(frames))
	var fail error
	onStillStack(func() {
		enc, err := newOpusEnc()
		if err != nil {
			fail = err
			return
		}
		for i, f := range frames {
			pkt, err := enc.encode(f, make([]byte, 0, 1500))
			if err != nil {
				fail = err
				return
			}
			ref[i] = slices.Clone(pkt)
		}
	})
	if fail != nil {
		t.Fatal(fail)
	}
	runs, bad := 0, 0
	var first string
	for depth := 0; depth < stackSweep; depth += stackStep {
		enc, err := newOpusEnc()
		if err != nil {
			t.Fatal(err)
		}
		runs++
		for i, f := range frames {
			var got []byte
			var eerr error
			atDepth(depth, func() {
				var pcm [frameSamples]int16
				var out [1500]byte
				copy(pcm[:], f)
				pkt, err := enc.encode(pcm[:], out[:0])
				got, eerr = slices.Clone(pkt), err
			})
			if eerr == nil && slices.Equal(got, ref[i]) {
				continue
			}
			bad++
			if first == "" {
				first = fmt.Sprintf("depth %d, frame %d: err %v, %d bytes vs %d", depth, i, eerr, len(got), len(ref[i]))
			}
			break
		}
	}
	if bad > 0 {
		t.Fatalf("%d of %d encoders sent a different packet when the caller's goroutine stack moved mid-call (first: %s)", bad, runs, first)
	}
	t.Logf("%d encoders × %d frames encoded at %d stack depths: every packet as on a still stack", runs, len(frames), runs)
}

// frameError is the RMS of got − want in dBFS (a missing or short frame
// counts its missing samples as the difference).
func frameError(got, want []int16) float64 {
	n := max(len(got), len(want))
	if n == 0 {
		return minDBFS
	}
	var s float64
	for i := 0; i < n; i++ {
		var a, b float64
		if i < len(got) {
			a = float64(got[i])
		}
		if i < len(want) {
			b = float64(want[i])
		}
		s += (a - b) * (a - b)
	}
	return dbfs(math.Sqrt(s / float64(n)))
}
