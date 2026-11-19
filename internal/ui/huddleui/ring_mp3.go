//go:build !js

package huddleui

import (
	"bytes"
	"encoding/binary"
	"io"

	"github.com/hajimehoshi/go-mp3"

	"github.com/some-du6e/clack/internal/huddle"
)

// decodeMP3 decodes to 48 kHz interleaved stereo.
func decodeMP3(b []byte) ([]int16, error) {
	d, err := mp3.NewDecoder(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(d) // 16-bit little-endian stereo
	if err != nil && len(raw) == 0 {
		return nil, err
	}
	pcm := make([]int16, len(raw)/2)
	for i := range pcm {
		pcm[i] = int16(binary.LittleEndian.Uint16(raw[2*i:]))
	}
	return huddle.ResampleStereo(pcm, d.SampleRate()), nil
}
