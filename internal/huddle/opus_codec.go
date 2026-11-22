package huddle

import (
	"fmt"

	opus "github.com/kazzmir/opus-go/opus"
)

// libopus for voice clips and audio playback (internal/clip,
// internal/media/goaudio): the huddle's transpiled libopus with every call on
// the codec goroutine (codecstack.go).

// Opus applications (opus_encoder_create's application argument).
const (
	OpusApplicationVoIP  = opus.ApplicationVoIP
	OpusApplicationAudio = opus.ApplicationAudio
)

// OpusEncoder is libopus's 48 kHz encoder with every call on the codec
// goroutine (codecstack.go), for voice clips.
type OpusEncoder struct {
	e             *opus.Encoder
	channels, app int
	bps           int
	vbr           bool
}

// NewOpusEncoder makes a 48 kHz encoder for channels (1 or 2) with
// OPUS_SET_BITRATE(bps) and OPUS_SET_VBR(vbr).
func NewOpusEncoder(channels, application, bps int, vbr bool) (*OpusEncoder, error) {
	o := &OpusEncoder{channels: channels, app: application, bps: bps, vbr: vbr}
	if err := o.reset(); err != nil {
		return nil, err
	}
	return o, nil
}

func (o *OpusEncoder) reset() (err error) {
	codecCall(func() {
		var e *opus.Encoder
		if e, err = opus.NewEncoder(sampleRate, o.channels, o.app); err != nil {
			return
		}
		if err = e.SetBitrate(o.bps); err != nil {
			return
		}
		if err = e.SetVBR(o.vbr); err != nil {
			return
		}
		o.e = e
	})
	return err
}

// Encode encodes one frame (frame samples per channel, interleaved) into
// out's capacity and returns the packet.
func (o *OpusEncoder) Encode(pcm []int16, frame int, out []byte) (pkt []byte, err error) {
	// libopus's assertions abort (a Go panic in the transpiled build): the
	// frame is lost and a fresh encoder takes the next one
	defer func() {
		if r := recover(); r != nil {
			pkt, err = nil, fmt.Errorf("opus encoder: %v", r)
			_ = o.reset()
		}
	}()
	var n int
	codecCall(func() { n, err = o.e.Encode(pcm, frame, out[:cap(out)]) })
	if err != nil {
		return nil, err
	}
	return out[:n], nil
}

// Close frees the encoder.
func (o *OpusEncoder) Close() {
	if o.e != nil {
		e := o.e
		o.e = nil
		codecCall(func() { _ = e.Close() })
	}
}

// OpusDecoder is libopus's 48 kHz decoder on the codec goroutine (a
// clip's playback, tests).
type OpusDecoder struct {
	d        *opus.Decoder
	channels int
	pcm      []int16
}

// NewOpusDecoder makes a 48 kHz decoder for channels (1 or 2).
func NewOpusDecoder(channels int) (dec *OpusDecoder, err error) {
	codecCall(func() {
		var d *opus.Decoder
		if d, err = opus.NewDecoder(sampleRate, channels); err != nil {
			return
		}
		dec = &OpusDecoder{d: d, channels: channels, pcm: make([]int16, 120*sampleRate/1000*channels)}
	})
	return dec, err
}

// Decode returns one packet's PCM, interleaved; it's valid until the next
// call.
func (o *OpusDecoder) Decode(pkt []byte) (pcm []int16, err error) {
	defer func() {
		if r := recover(); r != nil {
			pcm, err = nil, fmt.Errorf("opus decoder: %v", r)
		}
	}()
	var n int
	codecCall(func() { n, err = o.d.Decode(pkt, o.pcm, len(o.pcm)/o.channels, false) })
	if err != nil {
		return nil, err
	}
	return o.pcm[:n*o.channels], nil
}

// Close frees the decoder.
func (o *OpusDecoder) Close() {
	if o.d != nil {
		d := o.d
		o.d = nil
		codecCall(func() { _ = d.Close() })
	}
}
