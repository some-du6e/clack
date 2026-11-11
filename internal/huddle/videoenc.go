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

// encOpts is what an encoder is made for.
type encOpts struct {
	W, H, FPS, Bps int
	// Screen is screen content (the content share): text and still
	// pictures, keyframes only on request (Slack's JOIN asks Chime not to
	// request them periodically: disablePeriodicKeyframeRequestOnContentSender).
	Screen bool
}

// newHWEncoders are the OS's H.264 encoders, tried in order: VideoToolbox
// on macOS (h264enc_darwin.go), Media Foundation on Windows
// (h264enc_windows.go), OpenH264 where it is installed on Linux
// (h264enc_openh264.go).
var newHWEncoders []func(o encOpts) (videoEncoder, error)

// newVideoEncoder picks the OS encoder, falling back to the pure-Go I_PCM
// one for pictures small enough (the test camera).
func newVideoEncoder(o encOpts, logf func(string, ...any)) (videoEncoder, error) {
	for _, nw := range newHWEncoders {
		e, err := nw(o)
		if err == nil {
			return e, nil
		}
		logf("huddle: %dx%d hardware encoder: %v", o.W, o.H, err)
	}
	if o.W*o.H <= maxPCMPixels {
		return newPCMEncoder(o.W, o.H)
	}
	return nil, fmt.Errorf("no H.264 encoder for %dx%d video in this build", o.W, o.H)
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
