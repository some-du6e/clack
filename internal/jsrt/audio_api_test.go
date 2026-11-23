package jsrt

import (
	"context"

	"github.com/some-du6e/clack/internal/clip"
	"github.com/some-du6e/clack/internal/media/goaudio"
	"github.com/some-du6e/clack/internal/webaudio"
)

// withAudio gives the runtime the client's audio host (Options.Audio).
func withAudio(o *Options) { o.Audio = webaudio.Host{} }

// recordingOf is a MediaRecorder file as Go reads it: Opus in MP4's
// length, and its first channel decoded at 48 kHz with the channel count.
func recordingOf(b []byte) (dur float64, first []float32, channels int, err error) {
	info, err := clip.Probe(b)
	if err != nil {
		return 0, nil, 0, err
	}
	p, err := goaudio.Decode(context.Background(), b, "audio/mp4", 48000)
	if err != nil {
		return 0, nil, 0, err
	}
	return info.Duration().Seconds(), p.Channels[0], len(p.Channels), nil
}
