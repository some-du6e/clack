// Package webaudio is the plugin page's audio codecs (jsrt.AudioHost):
// what Chromium's media pipeline does under Web Audio's decodeAudioData
// and under MediaRecorder, for the plugins that read and convert audio
// (CustomNameRecording's upload).
//
//   - decodeAudioData decodes the whole file (Chromium's AudioFileReader)
//     and converts it to the context's rate (AudioBus::
//     CreateBySampleRateConverting): goaudio.Decode, the formats a media
//     element plays here (WAV, MP3, Opus or Vorbis in Ogg, Opus in WebM,
//     Opus or AAC in MP4).
//   - MediaRecorder records audio/mp4 as Chromium does: Opus in MP4 (no
//     codec in the type means Opus for every container since Chromium's
//     8b97014da4), the clip encoder Slack's voice clips use here
//     (internal/clip: AudioTrackOpusEncoder at 48 kHz, 60 ms packets, the
//     bitrate asked for, VBR, into Chromium's Mp4Muxer). Clack has no WebM
//     muxer and no AAC encoder, so audio/webm and mp4a types aren't
//     supported (isTypeSupported says false; Slack's own recorder and the
//     plugins ask for audio/mp4 first).
package webaudio

import (
	"context"
	"errors"
	"math"
	"mime"
	"strings"

	"github.com/some-du6e/clack/internal/clip"
	"github.com/some-du6e/clack/internal/media/goaudio"
)

// Host is the page's audio host (taut.Options.Audio).
type Host struct{}

// Decode is decodeAudioData's decode: the file's channels at rate.
func (Host) Decode(ctx context.Context, data []byte, rate int) ([][]float32, error) {
	p, err := goaudio.Decode(ctx, data, "", rate)
	if err != nil {
		return nil, err
	}
	return p.Channels, nil
}

// Supports is MediaRecorder.isTypeSupported for an audio type: audio/mp4
// with no codecs or with opus.
func (Host) Supports(typ string) bool {
	t, params, err := mime.ParseMediaType(typ)
	if err != nil || t != "audio/mp4" {
		return false
	}
	if c := strings.TrimSpace(params["codecs"]); c != "" {
		for _, x := range strings.Split(c, ",") {
			if !strings.EqualFold(strings.TrimSpace(x), "opus") {
				return false
			}
		}
	}
	return true
}

// DefaultBitRate is MediaRecorder's audio bitrate when the page sets none
// (Chromium's kDefaultAudioBitRate, 128 kbps).
const DefaultBitRate = 128000

// Encode is a MediaRecorder recording's file: interleaved float PCM (ch
// channels at rate) as Opus in MP4 at bps. More than two channels keep
// the first two, as the Opus encoder takes at most two; another rate is
// converted to Opus's 48 kHz first, as Chromium's encoder converts its
// track. A recording too short for one packet is an empty file.
func (h Host) Encode(pcm []float32, ch, rate, bps int, typ string) ([]byte, error) {
	if !h.Supports(typ) {
		return nil, errors.New("webaudio: " + typ + " isn't a type MediaRecorder records here")
	}
	if ch < 1 || rate <= 0 {
		return nil, errors.New("webaudio: no audio to encode")
	}
	if bps <= 0 {
		bps = DefaultBitRate
	}
	out := min(ch, 2)
	if out != ch {
		frames := len(pcm) / ch
		two := make([]float32, frames*2)
		for f := 0; f < frames; f++ {
			two[2*f], two[2*f+1] = pcm[f*ch], pcm[f*ch+1]
		}
		pcm = two
	}
	if rate != clip.Rate {
		pcm = goaudio.Resample(pcm, out, rate, clip.Rate)
	}
	enc, err := clip.NewEncoder(out, bps)
	if err != nil {
		return nil, err
	}
	s := make([]int16, len(pcm))
	for i, v := range pcm {
		s[i] = int16(math.Round(float64(max(-1, min(1, v))) * math.MaxInt16))
	}
	if err := enc.Write(s); err != nil {
		enc.Close()
		return nil, err
	}
	b, err := enc.Finish()
	if err != nil {
		return nil, err
	}
	if b == nil {
		b = []byte{}
	}
	return b, nil
}
