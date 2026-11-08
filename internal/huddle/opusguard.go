package huddle

import "sync/atomic"

// opus-go is libopus transpiled to Go: a failed libopus assertion
// (celt_fatal) panics with "libcshim: abort". Seen once per session, on an
// early packet of the fake's audio, on darwin/arm64 (CI) and linux/amd64
// (huddle-demo): silk_resampler's inLen >= Fs_in_kHz. One bad packet must
// not take the app down: opusDec.decode and opusEnc.encode (audio.go)
// recover, replace the codec (its state is half-updated) and return an
// error. opusAborts counts the decoder's, for the log.
var opusAborts atomic.Int64
