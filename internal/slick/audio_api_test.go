package slick

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"

	"github.com/hajimehoshi/go-mp3"

	"github.com/some-du6e/clack/internal/taut"
)

// withPageAudio gives the plugins' page an audio host (taut.Options.Audio)
// for decodeAudioData: harnessAudio.
func withPageAudio(o *taut.Options) { o.Audio = harnessAudio{} }

// harnessAudio is the harness's decodeAudioData: MP3s only (what Slick's
// CustomNameRecording reads), decoded with go-mp3 and taken to the
// context's rate linearly; no MediaRecorder. The client's host
// (internal/webaudio, tested there and in ui) brings the players' codecs,
// which need cgo this package's workflows don't build.
type harnessAudio struct{}

func (harnessAudio) Decode(ctx context.Context, data []byte, rate int) ([][]float32, error) {
	d, err := mp3.NewDecoder(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	pcm, err := io.ReadAll(io.LimitReader(d, 64<<20))
	if err != nil {
		return nil, err
	}
	in := d.SampleRate()
	n := len(pcm) / 4 // 16-bit stereo frames
	if n == 0 || in <= 0 || rate <= 0 {
		return nil, errors.New("harness audio: no frames")
	}
	out := n * rate / in
	chans := [][]float32{make([]float32, out), make([]float32, out)}
	at := func(f, c int) float32 {
		if f >= n {
			f = n - 1
		}
		return float32(int16(binary.LittleEndian.Uint16(pcm[4*f+2*c:]))) / 32768
	}
	for i := 0; i < out; i++ {
		pos := float64(i) * float64(in) / float64(rate)
		f := int(pos)
		w := float32(pos - float64(f))
		for c := range chans {
			chans[c][i] = at(f, c) + w*(at(f+1, c)-at(f, c))
		}
	}
	return chans, nil
}

func (harnessAudio) Supports(string) bool { return false }

func (harnessAudio) Encode([]float32, int, int, int, string) ([]byte, error) {
	return nil, errors.New("harness audio: no MediaRecorder")
}
