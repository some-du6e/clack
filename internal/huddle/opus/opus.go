//go:build !js

// Package opus is the part of kazzmir/opus-go (libopus transpiled to Go,
// BSD-3) that package huddle uses, which imports it under its own name.
// Everywhere but the browser it is opus-go itself: type aliases (so every
// method comes along) and functions that call it. Under GOOS=js the
// browser demo has no huddles (internal/huddle/rtc), so opus_js.go has
// stand-ins that never open a codec, and opus-go stays out of its build.
package opus

import upstream "github.com/kazzmir/opus-go/opus"

type (
	Encoder = upstream.Encoder
	Decoder = upstream.Decoder
)

const (
	ApplicationVoIP  = upstream.ApplicationVoIP
	ApplicationAudio = upstream.ApplicationAudio
)

func NewEncoder(sampleRate, channels, application int) (*Encoder, error) {
	return upstream.NewEncoder(sampleRate, channels, application)
}

func NewDecoder(sampleRate, channels int) (*Decoder, error) {
	return upstream.NewDecoder(sampleRate, channels)
}
