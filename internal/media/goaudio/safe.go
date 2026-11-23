package goaudio

import (
	"context"
	"errors"
	"fmt"
)

// The decode boundary: the readers here are total, but the decoders under
// them are another's (go-mp3, the transpiled libopus behind the huddle's
// own recover): a panic in opening a file, reading it or seeking in it is
// the file's MEDIA_ERR_DECODE (the card's Error), never Clack's crash
// (the device callback that reads runs on the sound card's thread).

// errCrashed is a decoder that panicked on this file.
var errCrashed = errors.New("goaudio: the decoder failed on this file")

func safeOpen(ctx context.Context, b []byte, mime string) (s stream, err error) {
	defer func() {
		if r := recover(); r != nil {
			s, err = nil, fmt.Errorf("%w: %v", errCrashed, r)
		}
	}()
	return open(ctx, b, mime)
}

func safeRead(s stream, buf []float32) (n int, err error) {
	defer func() {
		if r := recover(); r != nil {
			n, err = 0, fmt.Errorf("%w: %v", errCrashed, r)
		}
	}()
	return s.Read(buf)
}

func safeSeek(s stream, f int64) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%w: %v", errCrashed, r)
		}
	}()
	return s.SeekFrame(f)
}
