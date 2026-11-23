package goaudio

// The readers under fuzzing (ci's linux-build runs each target for a fixed
// time; go test runs their seeds): no bytes may panic one, hang it (each
// input has 5 s, the readers look at their context, and one still running
// at 10 s crashes the worker with every goroutine's stack, so the fuzzer
// keeps it) or make it allocate far past the input. The seeds are the fixtures and their cuts, and
// TestReadersAreTotal's cases are what the review found by hand. The
// readers are called as they are, so a panic is theirs; go-mp3's decoder,
// another's code, runs behind the decode boundary (safe.go) as in the
// player, and FuzzMP3 calls this package's frame index and tag skip
// directly.

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"runtime/pprof"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/media/mediatest"
)

// stuck crashes the process if the input b is still running after limit:
// its bytes and every goroutine's stack on stderr, then a panic the
// fuzzer takes for a crasher (Go's fuzzing reports no hang of its own).
// The returned func ends the watch.
func stuck(b []byte, limit time.Duration) func() {
	done := make(chan struct{})
	go func() {
		select {
		case <-done:
		case <-time.After(limit):
			var buf bytes.Buffer
			fmt.Fprintf(&buf, "an input ran over %v (%d bytes):\n%s\n", limit, len(b), hex.Dump(b[:min(len(b), 4096)]))
			pprof.Lookup("goroutine").WriteTo(&buf, 2)
			os.Stderr.Write(buf.Bytes())
			panic(fmt.Sprintf("goaudio: an input ran over %v", limit))
		}
	}()
	return func() { close(done) }
}

func addSeeds(f *testing.F, seeds ...[]byte) {
	for _, s := range seeds {
		f.Add(s)
		for _, n := range []int{len(s) / 2, 200, 64, 12} {
			if n < len(s) {
				f.Add(s[:n])
			}
		}
	}
}

// fuzzStream reads a little of an opened stream, seeks to its middle,
// its end and its start, and reads again.
func fuzzStream(t *testing.T, s stream, safe bool) {
	defer s.Close()
	if c := s.Channels(); c < 1 || c > maxChannels {
		t.Fatalf("%d channels", c)
	}
	if s.Rate() <= 0 || s.Frames() < 0 {
		t.Fatalf("rate %d, %d frames", s.Rate(), s.Frames())
	}
	read, seek := s.Read, s.SeekFrame
	if safe {
		read = func(b []float32) (int, error) { return safeRead(s, b) }
		seek = func(f int64) error { return safeSeek(s, f) }
	}
	buf := make([]float32, 512*s.Channels())
	for i := 0; i < 8; i++ {
		n, err := read(buf)
		if n < 0 || n > 512 {
			t.Fatalf("Read gave %d frames of 512", n)
		}
		if err != nil {
			break
		}
	}
	for _, at := range []int64{s.Frames() / 2, s.Frames(), 0} {
		if err := seek(at); err != nil {
			return
		}
		if n, _ := read(buf); n < 0 || n > 512 {
			t.Fatalf("Read after a seek to %d gave %d frames of 512", at, n)
		}
	}
}

func fuzzReader(f *testing.F, reader func(context.Context, []byte) (stream, error), seeds ...[]byte) {
	addSeeds(f, seeds...)
	f.Fuzz(func(t *testing.T, b []byte) {
		defer stuck(b, 10*time.Second)()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s, err := reader(ctx, b)
		if err != nil {
			return
		}
		fuzzStream(t, s, false)
	})
}

func FuzzMP4(f *testing.F) {
	fuzzReader(f, openMP4, mediatest.TonesOpusMP4, mediatest.TonesOpusFrag, mediatest.TonesAAC)
}

func FuzzWebM(f *testing.F) {
	fuzzReader(f, openWebM, mediatest.TonesWebM)
}

func FuzzOgg(f *testing.F) {
	fuzzReader(f, openOgg, mediatest.TonesOgg)
}

func FuzzWAV(f *testing.F) {
	fuzzReader(f, func(_ context.Context, b []byte) (stream, error) { return openWAV(b) }, mediatest.TonesWAV)
}

func FuzzMP3(f *testing.F) {
	addSeeds(f, mediatest.TonesMP3)
	f.Fuzz(func(t *testing.T, b []byte) {
		defer stuck(b, 10*time.Second)()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		// this package's: the tag skip and the frame index, as they are
		start := skipID3(b)
		if start < 0 || start > len(b) {
			t.Fatalf("tags end at %d of %d", start, len(b))
		}
		frames, total, rate, err := indexMP3(ctx, b, start)
		if err != nil {
			return
		}
		var last int64 = -1
		for _, fr := range frames {
			if fr.off <= last || fr.off >= int64(len(b)) || fr.at < 0 || fr.at > total {
				t.Fatalf("frame at %d (sample %d) after %d, of %d bytes and %d samples", fr.off, fr.at, last, len(b), total)
			}
			last = fr.off
		}
		if len(frames) > 0 && (rate < 8000 || rate > 48000) {
			t.Fatalf("rate %d", rate)
		}
		// go-mp3's decoding, behind the boundary as in the player
		s, err := safeOpen(ctx, b, "audio/mpeg")
		if err != nil {
			return
		}
		fuzzStream(t, s, true)
	})
}
