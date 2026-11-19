//go:build js

package opus

// Stand-ins for the browser demo (opus.go): the names and methods package
// huddle uses, none of which opens a codec.

import "errors"

var errNoAudio = errors.New("huddle audio isn't part of the browser demo")

// libopus's values
const (
	ApplicationVoIP  = 2048
	ApplicationAudio = 2049
)

type Encoder struct{}

type Decoder struct{}

func NewEncoder(sampleRate, channels, application int) (*Encoder, error) { return nil, errNoAudio }

func NewDecoder(sampleRate, channels int) (*Decoder, error) { return nil, errNoAudio }

func (*Encoder) SetBitrate(int) error                     { return errNoAudio }
func (*Encoder) SetComplexity(int) error                  { return errNoAudio }
func (*Encoder) Encode([]int16, int, []byte) (int, error) { return 0, errNoAudio }

func (*Decoder) Decode([]byte, []int16, int, bool) (int, error) { return 0, errNoAudio }
