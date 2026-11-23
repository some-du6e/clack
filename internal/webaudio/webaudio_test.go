package webaudio

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/clip"
	"github.com/some-du6e/clack/internal/media/goaudio"
	"github.com/some-du6e/clack/internal/media/mediatest"
)

// tonesAt checks that pcm (one channel at rate) is the fixtures' tones: in
// each second (shifted by lead), the tone the fixture plays then is the
// loudest of the three, by far.
func tonesAt(t *testing.T, what string, pcm []float32, rate int, lead time.Duration) {
	t.Helper()
	for s, want := range mediatest.Tones {
		from := int((time.Duration(s)*time.Second + lead + 200*time.Millisecond).Seconds() * float64(rate))
		to := from + rate*6/10
		if to > len(pcm) {
			t.Errorf("%s: %d frames, too short for second %d", what, len(pcm), s)
			return
		}
		if i, by := mediatest.Loudest(pcm[from:to], rate); i != s || by < 4 {
			t.Errorf("%s: second %d's loudest tone is %d (%.1fx the next), want %.0f Hz's", what, s, i, by, want)
		}
	}
}

func peak(pcm []float32) float64 {
	m := 0.0
	for _, v := range pcm {
		m = math.Max(m, math.Abs(float64(v)))
	}
	return m
}

// TestDecodeLikeChromium: decodeAudioData's decode, as Chromium's
// AudioFileReader and sample-rate conversion give it: every channel the
// file has, at the context's rate, the whole file. The 44.1 kHz stereo MP3
// and the 16 kHz mono WAV come out at 48 kHz, 3 s long (the MP3 within its
// encoder's delay), as loud as they are (a sine at 1/8) and playing the
// fixture's tones; at rate 0 the WAV keeps its own 16 kHz. A file that
// isn't audio is an error (decodeAudioData's EncodingError).
func TestDecodeLikeChromium(t *testing.T) {
	ctx := context.Background()
	var h Host
	for _, tc := range []struct {
		name     string
		data     []byte
		rate     int
		channels int
		slack    int // frames the decoded length may differ by
	}{
		{"mp3", mediatest.TonesMP3, 48000, 2, 48000 * 60 / 1000},
		{"wav", mediatest.TonesWAV, 48000, 1, 2},
		{"wav at its own rate", mediatest.TonesWAV, 0, 1, 1},
		{"ogg", mediatest.TonesOgg, 48000, 1, 48000 * 30 / 1000},
	} {
		chans, err := h.Decode(ctx, tc.data, tc.rate)
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		rate := tc.rate
		if rate == 0 {
			rate = 16000
		}
		if len(chans) != tc.channels {
			t.Errorf("%s: %d channels, want %d", tc.name, len(chans), tc.channels)
			continue
		}
		if n, want := len(chans[0]), 3*rate; n < want-tc.slack || n > want+tc.slack {
			t.Errorf("%s: %d frames at %d Hz, want %d (±%d): the whole 3 s", tc.name, n, rate, want, tc.slack)
		}
		if p := peak(chans[0]); p < 0.11 || p > 0.14 {
			t.Errorf("%s: peak %.3f, want the fixtures' 1/8", tc.name, p)
		}
		tonesAt(t, tc.name, chans[0], rate, 0)
	}
	if _, err := h.Decode(ctx, []byte("this isn't audio at all, not even close"), 48000); err == nil {
		t.Error("text decoded as audio")
	}
}

// TestEncodeOpusInMP4: MediaRecorder's audio/mp4, as Chromium records it:
// Opus in MP4 (clip's encoder and muxer), the channels it was given (two
// at most), converted to Opus's 48 kHz from the context's rate. Decoded
// again it is the recording: as long, the same tones. Supports answers
// isTypeSupported: audio/mp4 with no codec or with Opus, nothing else.
func TestEncodeOpusInMP4(t *testing.T) {
	ctx := context.Background()
	var h Host
	for _, typ := range []string{"audio/mp4", "audio/mp4;codecs=opus", `audio/mp4; codecs="opus"`, "AUDIO/MP4"} {
		if !h.Supports(typ) {
			t.Errorf("Supports(%q) false", typ)
		}
	}
	for _, typ := range []string{"audio/webm", "audio/webm;codecs=opus", "audio/mp4;codecs=mp4a.40.2", "video/mp4", "audio/ogg", "", "nonsense"} {
		if h.Supports(typ) {
			t.Errorf("Supports(%q) true: Clack can't record it", typ)
		}
	}
	for _, rate := range []int{48000, 44100} {
		chans, err := h.Decode(ctx, mediatest.TonesMP3, rate)
		if err != nil {
			t.Fatal(err)
		}
		n := len(chans[0])
		pcm := make([]float32, 2*n)
		for i := 0; i < n; i++ {
			pcm[2*i], pcm[2*i+1] = chans[0][i], chans[1][i]
		}
		out, err := h.Encode(pcm, 2, rate, DefaultBitRate, "audio/mp4;codecs=opus")
		if err != nil {
			t.Fatalf("%d Hz: %v", rate, err)
		}
		info, err := clip.Probe(out)
		if err != nil {
			t.Fatalf("%d Hz: the recording isn't Opus in MP4: %v", rate, err)
		}
		if d := info.Duration(); d < 2900*time.Millisecond || d > 3100*time.Millisecond {
			t.Errorf("%d Hz: the recording is %v, want the 3 s it was given", rate, d)
		}
		back, err := goaudio.Decode(ctx, out, "audio/mp4", 48000)
		if err != nil {
			t.Fatalf("%d Hz: the recording doesn't decode: %v", rate, err)
		}
		if len(back.Channels) != 2 {
			t.Errorf("%d Hz: %d channels back, want 2", rate, len(back.Channels))
		}
		tonesAt(t, "the recording", back.Channels[0], 48000, 0)
	}
	if _, err := h.Encode(make([]float32, 100), 1, 48000, 0, "audio/webm"); err == nil {
		t.Error("encoded a type it doesn't support")
	}
}
