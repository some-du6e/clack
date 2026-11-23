package goaudio

import (
	"context"
	"errors"
	"io"
	"math"
)

// PCM is a decoded file's audio, as Web Audio's AudioBuffer holds it: one
// slice of float samples (-1..1) per channel, at Rate.
type PCM struct {
	Rate     int
	Channels [][]float32
}

// Frames is the length in sample frames.
func (p *PCM) Frames() int {
	if p == nil || len(p.Channels) == 0 {
		return 0
	}
	return len(p.Channels[0])
}

// MaxDecodeSamples caps what Decode makes (frames × channels, 128 MiB of
// float32: about six minutes of stereo at 48 kHz). Chromium sets no such
// limit, but a page asking to decode an hour of audio would hold gigabytes.
const MaxDecodeSamples = 1 << 25

// ErrDecodeTooLong is a file whose audio is past MaxDecodeSamples.
var ErrDecodeTooLong = errors.New("goaudio: the audio is too long to decode")

// Decode is Web Audio's decodeAudioData as Chromium does it: the whole
// file decoded (AudioFileReader: every channel the file has, from the
// formats a media element plays here: WAV, MP3, Opus or Vorbis in Ogg,
// Opus in WebM, Opus or AAC in MP4) and each channel converted to rate
// (AudioBus::CreateBySampleRateConverting; 0 keeps the file's rate) with
// the players' windowed-sinc resampler. mime is the file's type when
// known (the OS decoders' hint). An error is a file that isn't audio this
// decodes: decodeAudioData's EncodingError.
func Decode(ctx context.Context, b []byte, mime string, rate int) (*PCM, error) {
	s, err := open(ctx, b, mime)
	if err != nil {
		return nil, err
	}
	defer s.Close()
	ch, in := s.Channels(), s.Rate()
	if ch < 1 || ch > maxChannels || in <= 0 {
		return nil, errUnsupported
	}
	if rate <= 0 {
		rate = in
	}
	var inter []float32
	if n := s.Frames(); n > 0 && n*int64(ch) <= MaxDecodeSamples {
		inter = make([]float32, 0, n*int64(ch))
	}
	buf := make([]float32, 4096*ch)
	for step := 0; ; step++ {
		if done(ctx, step) {
			return nil, ctx.Err()
		}
		n, err := s.Read(buf)
		inter = append(inter, buf[:n*ch]...)
		if len(inter) > MaxDecodeSamples {
			return nil, ErrDecodeTooLong
		}
		if err == io.EOF || err == nil && n == 0 {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	frames := len(inter) / ch
	if rate != in && frames > 0 {
		outFrames := int(math.Ceil(float64(frames) * float64(rate) / float64(in)))
		if int64(outFrames)*int64(ch) > MaxDecodeSamples {
			return nil, ErrDecodeTooLong
		}
		r := newRateResampler(in, rate, ch)
		r.push(inter)
		out := make([]float32, outFrames*ch)
		frames = r.pull(out, true)
		inter = out[:frames*ch]
	}
	p := &PCM{Rate: rate, Channels: make([][]float32, ch)}
	for c := range p.Channels {
		p.Channels[c] = make([]float32, frames)
	}
	for f := 0; f < frames; f++ {
		for c := 0; c < ch; c++ {
			p.Channels[c][f] = inter[f*ch+c]
		}
	}
	return p, nil
}

// Resample converts interleaved float frames of ch channels from rate in
// to rate out with the players' resampler (Chromium's MediaRecorder takes
// a track to its encoder's rate the same way).
func Resample(pcm []float32, ch, in, out int) []float32 {
	if ch < 1 || in <= 0 || out <= 0 || in == out || len(pcm) < ch {
		return pcm
	}
	frames := len(pcm) / ch
	n := int(math.Ceil(float64(frames) * float64(out) / float64(in)))
	r := newRateResampler(in, out, ch)
	r.push(pcm[:frames*ch])
	dst := make([]float32, n*ch)
	return dst[:r.pull(dst, true)*ch]
}
