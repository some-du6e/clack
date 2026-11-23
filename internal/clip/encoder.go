package clip

import (
	"errors"
	"time"

	"github.com/some-du6e/clack/internal/huddle"
)

// Encoder is MediaRecorder's audio side for a clip: AudioTrackOpusEncoder
// (third_party/blink/.../mediarecorder/audio_track_opus_encoder.cc) into
// the Muxer. The microphone's PCM collects in a FIFO and every 60 ms of it
// (kOpusPreferredBufferDurationMs) becomes one Opus packet: 48 kHz,
// OPUS_APPLICATION_AUDIO, OPUS_SET_BITRATE(audioBitsPerSecond),
// OPUS_SET_VBR(1) (MediaRecorder's default bitrate mode is variable).
// A packet's timestamp is its first sample's capture time, which for a
// microphone that never skips is its sample count. What's left in the
// FIFO at stop (under 60 ms) is never encoded, as in Chromium.
type Encoder struct {
	channels int
	enc      *huddle.OpusEncoder
	mux      *Muxer
	fifo     []int16
	pkt      []byte
	packets  int
	err      error
	done     bool
}

// opusMaxDataBytes is kOpusMaxDataBytes.
const opusMaxDataBytes = 4000

// NewEncoder makes the encoder for a mono (1) or stereo (2) clip at
// bitrate bps (BitRate is Slack's).
func NewEncoder(channels, bps int) (*Encoder, error) {
	if channels < 1 || channels > 2 { // third_party/libopus: up to 2
		return nil, errors.New("clip: 1 or 2 channels")
	}
	enc, err := huddle.NewOpusEncoder(channels, huddle.OpusApplicationAudio, bps, true)
	if err != nil {
		return nil, err
	}
	return &Encoder{channels: channels, enc: enc, mux: NewMuxer(channels), pkt: make([]byte, opusMaxDataBytes)}, nil
}

// Muxer is the encoder's muxer (tests set its clock).
func (e *Encoder) Muxer() *Muxer { return e.mux }

// Write adds interleaved PCM (48 kHz) and encodes every whole 60 ms of
// it. Not safe for concurrent use.
func (e *Encoder) Write(pcm []int16) error {
	if e.done {
		return errors.New("clip: encoder finished")
	}
	e.fifo = append(e.fifo, pcm...)
	n := FrameSamples * e.channels
	for len(e.fifo) >= n {
		pkt, err := e.enc.Encode(e.fifo[:n], FrameSamples, e.pkt)
		if err != nil {
			// Chromium's Opus encoder keeps running after a failed frame
			// (NotifyError); the frame is lost
			e.err = err
		} else {
			e.mux.Add(pkt, time.Duration(e.packets)*FrameSamples*time.Second/Rate)
		}
		e.packets++ // the next packet's capture time moves on either way
		e.fifo = append(e.fifo[:0], e.fifo[n:]...)
	}
	return nil
}

// Packets is how many 60 ms packets have been encoded.
func (e *Encoder) Packets() int { return e.packets }

// Finish ends the recording and returns the MP4 (nil for a recording too
// short for one packet). The encoder is freed. A frame libopus failed on
// is only lost; the error is the last such failure when no packet at all
// made it.
func (e *Encoder) Finish() ([]byte, error) {
	if e.done {
		return nil, errors.New("clip: encoder finished")
	}
	e.done = true
	out := e.mux.Finish()
	e.enc.Close()
	e.fifo = nil
	if out == nil {
		return nil, e.err
	}
	return out, nil
}

// Close frees the encoder without finishing (a cancelled recording).
func (e *Encoder) Close() {
	if !e.done {
		e.done = true
		e.enc.Close()
		e.fifo = nil
	}
}
