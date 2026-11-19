//go:build js

package bgblur

// The browser demo has no huddles or video (internal/huddle/rtc), so its
// build has no background blur: not the MediaPipe selfie model
// (Apache-2.0) model.go embeds, nor the internal/tflite interpreter that
// runs it. These are the names package huddle uses; NewEffect fails, as
// it does when the model can't load, and the camera goes out unchanged.

import (
	"errors"
	"time"
)

// Frame is effect.go's.
type Frame struct {
	W, H      int
	Y, UV     []byte
	YStride   int
	UVStride  int
	FullRange bool
	At        time.Time
}

type Effect struct{}

func NewEffect() (*Effect, error) {
	return nil, errors.New("background blur isn't part of the browser demo")
}

func (*Effect) Close() {}

func (*Effect) Apply(*Frame) {}
