package mediatest

import (
	"bytes"
	_ "embed"
	"math"
	"net/http"
	"time"
)

// The audio fixtures (testdata/gen-audio.sh): 3.000 s of a sine at
// 1/8 amplitude, 440 Hz for the first second, 660 Hz for the second, 880 Hz
// for the third, in each format a message's audio player gets.
var (
	// TonesWAV is 16-bit PCM WAV, 16 kHz mono.
	//go:embed testdata/tones-3s.wav
	TonesWAV []byte
	// TonesMP3 is MP3 (LAME), 44.1 kHz stereo, 64 kbps, with its ID3v2 tag
	// and Xing frame.
	//go:embed testdata/tones-3s.mp3
	TonesMP3 []byte
	// TonesAAC is AAC-LC in MP4 (an m4a), 48 kHz mono: an aac rendition.
	//go:embed testdata/tones-3s-aac.m4a
	TonesAAC []byte
	// TonesOpusMP4 is Opus in MP4 (60 ms packets, pre-skip 312, the last
	// packet cut to 312 samples by its duration), moov first.
	//go:embed testdata/tones-3s-opus.m4a
	TonesOpusMP4 []byte
	// TonesOpusFrag is the same Opus in fragmented MP4: a moov with no
	// samples, then a moof and mdat a second, as Chromium's MediaRecorder
	// writes a voice clip.
	//go:embed testdata/tones-3s-opus-frag.m4a
	TonesOpusFrag []byte
	// TonesWebM is Opus in WebM (20 ms SimpleBlocks, CodecDelay, and the
	// last block's DiscardPadding).
	//go:embed testdata/tones-3s.webm
	TonesWebM []byte
	// TonesOgg is Opus in Ogg.
	//go:embed testdata/tones-3s.ogg
	TonesOgg []byte
)

// AudioDuration is the audio fixtures' length.
const AudioDuration = 3 * time.Second

// AudioFixture is one of the audio fixtures, as a source names it.
type AudioFixture struct {
	Name, Ext, Mime string
	Data            []byte
}

// AudioFixtures are the audio fixtures, each with the extension and
// mimetype a message's file gives it.
func AudioFixtures() []AudioFixture {
	return []AudioFixture{
		{"wav", "wav", "audio/wav", TonesWAV},
		{"mp3", "mp3", "audio/mpeg", TonesMP3},
		{"aac", "m4a", "audio/mp4", TonesAAC},
		{"opus-mp4", "m4a", "audio/mp4", TonesOpusMP4},
		{"opus-frag", "m4a", "audio/mp4", TonesOpusFrag},
		{"webm", "webm", "audio/webm", TonesWebM},
		{"ogg", "ogg", "audio/ogg", TonesOgg},
	}
}

// Tone is the frequency the audio fixtures play at t.
func Tone(t time.Duration) float64 {
	switch {
	case t < time.Second:
		return 440
	case t < 2*time.Second:
		return 660
	}
	return 880
}

// Tones are the three frequencies, in order.
var Tones = [3]float64{440, 660, 880}

// Power is the Goertzel power of freq in mono samples at rate,
// normalized by the length: a full-scale sine reads 0.25, the fixtures'
// (1/8 amplitude) about 0.004, and a frequency that isn't there next to
// nothing.
func Power(samples []float32, rate int, freq float64) float64 {
	if len(samples) == 0 {
		return 0
	}
	w := 2 * math.Pi * freq / float64(rate)
	c := 2 * math.Cos(w)
	var s1, s2 float64
	for _, x := range samples {
		s0 := float64(x) + c*s1 - s2
		s2, s1 = s1, s0
	}
	p := s1*s1 + s2*s2 - c*s1*s2
	n := float64(len(samples))
	return p / (n * n)
}

// Loudest is which of Tones is loudest in samples (its index), and how
// many times louder it is than the next.
func Loudest(samples []float32, rate int) (int, float64) {
	best, next := -1, 0.0
	var pw [3]float64
	for i, f := range Tones {
		pw[i] = Power(samples, rate, f)
		if best < 0 || pw[i] > pw[best] {
			best = i
		}
	}
	for i := range pw {
		if i != best && pw[i] > next {
			next = pw[i]
		}
	}
	if next <= 0 {
		return best, math.Inf(1)
	}
	return best, pw[best] / next
}

// AudioHandler serves data with ranges as files.slack.com would, as mime.
func AudioHandler(data []byte, mime string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", mime)
		http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(data))
	})
}
