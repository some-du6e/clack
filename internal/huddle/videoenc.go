package huddle

import (
	"bytes"
	"fmt"
)

// videoEncoder turns camera frames into H.264 (constrained baseline, no
// B-frames: what Chime and every Slack client decode).
type videoEncoder interface {
	// Encode returns one Annex B access unit, with SPS and PPS before every
	// keyframe; key forces one (a PLI or FIR from Chime). nil, nil means the
	// encoder dropped the frame (rate control).
	Encode(f *CameraFrame, key bool) ([]byte, error)
	// SetBitrate adjusts the target average bitrate (Chime's REMB).
	SetBitrate(bps int)
	Close()
	Name() string
}

// newHWEncoder is the OS's H.264 encoder: VideoToolbox on macOS
// (h264enc_darwin.go). Nil elsewhere for now.
var newHWEncoder func(w, h, fps, bps int) (videoEncoder, error)

// newVideoEncoder picks the OS encoder, falling back to the pure-Go I_PCM
// one for pictures small enough (the test camera).
func newVideoEncoder(w, h, fps, bps int, logf func(string, ...any)) (videoEncoder, error) {
	if newHWEncoder != nil {
		e, err := newHWEncoder(w, h, fps, bps)
		if err == nil {
			return e, nil
		}
		logf("huddle: %dx%d hardware encoder: %v", w, h, err)
	}
	if w*h <= maxPCMPixels {
		return newPCMEncoder(w, h)
	}
	return nil, fmt.Errorf("no H.264 encoder for %dx%d video in this build", w, h)
}

// hasIDR reports whether an Annex B access unit holds an IDR slice.
func hasIDR(au []byte) bool {
	for {
		i := bytes.Index(au, []byte{0, 0, 1})
		if i < 0 || i+3 >= len(au) {
			return false
		}
		if au[i+3]&0x1f == 5 {
			return true
		}
		au = au[i+3:]
	}
}
