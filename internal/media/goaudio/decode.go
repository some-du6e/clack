package goaudio

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"

	mp3 "github.com/hajimehoshi/go-mp3"
)

// stream is a decoded audio file: float PCM, interleaved, at its own rate.
type stream interface {
	Rate() int
	Channels() int
	// Frames is the file's length in frames.
	Frames() int64
	// Read decodes up to len(buf)/Channels frames into buf and returns how
	// many it decoded; io.EOF once there are no more.
	Read(buf []float32) (int, error)
	// SeekFrame makes the next Read start at frame f.
	SeekFrame(f int64) error
	// Codec names what it decodes (logs, tests).
	Codec() string
}

// errUnsupported is a file this package can't decode: the element's
// MEDIA_ERR_SRC_NOT_SUPPORTED.
var errUnsupported = errors.New("goaudio: unsupported audio")

// open sniffs the file and returns its stream.
func open(b []byte) (stream, error) {
	switch {
	case len(b) >= 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WAVE":
		return openWAV(b)
	case len(b) >= 4 && string(b[:4]) == "OggS":
		return openOgg(b)
	case len(b) >= 4 && binary.BigEndian.Uint32(b) == ebmlHeader:
		return openWebM(b)
	case len(b) >= 8 && string(b[4:8]) == "ftyp":
		return openMP4(b)
	case isMP3(b):
		return openMP3(b)
	}
	return nil, errUnsupported
}

// isMP3 is an ID3 tag or an MPEG audio frame sync at the start.
func isMP3(b []byte) bool {
	if len(b) >= 3 && string(b[:3]) == "ID3" {
		return true
	}
	return len(b) >= 2 && b[0] == 0xFF && b[1]&0xE0 == 0xE0 && b[1]&0x06 != 0 // sync, not layer "reserved"
}

// ---- WAV ----

type wavStream struct {
	data     []byte
	rate, ch int
	format   int // 1 PCM, 3 float
	bits     int
	frame    int // bytes per frame
	pos      int64
}

func openWAV(b []byte) (stream, error) {
	w := &wavStream{}
	var fmtOK bool
	for off := 12; off+8 <= len(b); {
		id := string(b[off : off+4])
		n := int(binary.LittleEndian.Uint32(b[off+4:]))
		body := off + 8
		end := body + n
		if id == "data" && (n == 0 || end > len(b) || n < 0) {
			end, n = len(b), len(b)-body // a stream that didn't fill in the size
		} else if end > len(b) || n < 0 {
			return nil, errors.New("goaudio: a WAV chunk runs past the end")
		}
		switch id {
		case "fmt ":
			if n < 16 {
				return nil, errors.New("goaudio: short WAV fmt")
			}
			f := b[body:end]
			w.format = int(binary.LittleEndian.Uint16(f))
			w.ch = int(binary.LittleEndian.Uint16(f[2:]))
			w.rate = int(binary.LittleEndian.Uint32(f[4:]))
			w.frame = int(binary.LittleEndian.Uint16(f[12:]))
			w.bits = int(binary.LittleEndian.Uint16(f[14:]))
			if w.format == 0xFFFE && n >= 40 { // WAVE_FORMAT_EXTENSIBLE: the subformat's first two bytes
				w.format = int(binary.LittleEndian.Uint16(f[24:]))
			}
			fmtOK = true
		case "data":
			w.data = b[body:end]
		}
		off = body + n + n&1 // chunks are word-aligned
	}
	if !fmtOK || w.data == nil {
		return nil, errors.New("goaudio: a WAV without fmt or data")
	}
	switch {
	case w.format == 1 && (w.bits == 8 || w.bits == 16 || w.bits == 24 || w.bits == 32):
	case w.format == 3 && (w.bits == 32 || w.bits == 64):
	default:
		return nil, fmt.Errorf("%w: WAV format %d, %d bits", errUnsupported, w.format, w.bits)
	}
	if w.ch < 1 || w.rate < 1000 || w.rate > 384000 || w.frame != w.ch*w.bits/8 {
		return nil, fmt.Errorf("%w: WAV of %d channels at %d Hz", errUnsupported, w.ch, w.rate)
	}
	return w, nil
}

func (w *wavStream) Rate() int     { return w.rate }
func (w *wavStream) Channels() int { return w.ch }
func (w *wavStream) Frames() int64 { return int64(len(w.data) / w.frame) }
func (w *wavStream) Codec() string { return "pcm" }
func (w *wavStream) SeekFrame(f int64) error {
	w.pos = max(0, min(f, w.Frames()))
	return nil
}

func (w *wavStream) Read(buf []float32) (int, error) {
	n := min(int64(len(buf)/w.ch), w.Frames()-w.pos)
	if n <= 0 {
		return 0, io.EOF
	}
	src := w.data[w.pos*int64(w.frame):]
	bps := w.bits / 8
	for i := 0; i < int(n)*w.ch; i++ {
		s := src[i*bps:]
		var v float32
		switch {
		case w.format == 3 && w.bits == 32:
			v = math.Float32frombits(binary.LittleEndian.Uint32(s))
		case w.format == 3:
			v = float32(math.Float64frombits(binary.LittleEndian.Uint64(s)))
		case w.bits == 8:
			v = (float32(s[0]) - 128) / 128
		case w.bits == 16:
			v = float32(int16(binary.LittleEndian.Uint16(s))) / 32768
		case w.bits == 24:
			v = float32(int32(uint32(s[0])<<8|uint32(s[1])<<16|uint32(s[2])<<24)>>8) / 8388608
		default:
			v = float32(int32(binary.LittleEndian.Uint32(s))) / 2147483648
		}
		buf[i] = v
	}
	w.pos += n
	return int(n), nil
}

// ---- MP3 ----

type mp3Stream struct {
	d      *mp3.Decoder
	frames int64
	raw    []byte
}

func openMP3(b []byte) (stream, error) {
	d, err := mp3.NewDecoder(bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errUnsupported, err)
	}
	return &mp3Stream{d: d, frames: d.Length() / 4}, nil
}

func (m *mp3Stream) Rate() int     { return m.d.SampleRate() }
func (m *mp3Stream) Channels() int { return 2 } // go-mp3 always decodes to stereo
func (m *mp3Stream) Frames() int64 { return m.frames }
func (m *mp3Stream) Codec() string { return "mp3" }
func (m *mp3Stream) SeekFrame(f int64) error {
	_, err := m.d.Seek(max(0, min(f, m.frames))*4, io.SeekStart)
	return err
}

func (m *mp3Stream) Read(buf []float32) (int, error) {
	frames := len(buf) / 2
	if cap(m.raw) < frames*4 {
		m.raw = make([]byte, frames*4)
	}
	raw := m.raw[:frames*4]
	n, err := io.ReadFull(m.d, raw)
	n -= n % 4
	for i := 0; i < n/2; i++ {
		buf[i] = float32(int16(binary.LittleEndian.Uint16(raw[2*i:]))) / 32768
	}
	if n > 0 {
		return n / 4, nil
	}
	if err == io.ErrUnexpectedEOF {
		err = io.EOF
	}
	return 0, err
}
