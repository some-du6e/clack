package huddle

import (
	"fmt"
	"sync/atomic"

	opus "github.com/kazzmir/opus-go/opus"
)

// opus-go is libopus transpiled to Go: a failed libopus assertion
// (celt_fatal) panics with "libcshim: abort". Seen on darwin/arm64 in CI
// (silk_resampler: inLen >= Fs_in_kHz) on the fake's audio. One bad packet
// must not take the app down, so decoding recovers, replaces the decoder
// (its state is half-updated) and reports an error.

var opusAborts atomic.Int64

func (o *opusDec) decodeSafe(pkt []byte) (pcm []int16, err error) {
	defer func() {
		if r := recover(); r != nil {
			opusAborts.Add(1)
			err = fmt.Errorf("opus decode aborted: %v", r)
			if d, e := opus.NewDecoder(sampleRate, playChannels); e == nil {
				o.d = d
			}
		}
	}()
	return o.decode(pkt)
}
