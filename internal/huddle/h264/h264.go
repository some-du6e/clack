//go:build !js

// Package h264 is the part of thesyncim/goh264 (an H.264 decoder in Go,
// LGPL-2.1) that package huddle uses, which imports it as goh264.
// Everywhere but the browser it is goh264 itself: type aliases (so every
// method and field comes along) and a function that calls it. Under
// GOOS=js the browser demo has no huddles or video (internal/huddle/rtc),
// so h264_js.go has stand-ins that decode nothing, and goh264 stays out of
// its build.
package h264

import "github.com/thesyncim/goh264"

type (
	Decoder = goh264.Decoder
	Frame   = goh264.Frame
)

func NewDecoder() *Decoder { return goh264.NewDecoder() }
