package goaudio

import (
	"errors"
	"fmt"
	"io"
	"sort"

	"github.com/some-du6e/clack/internal/huddle"
)

// Opus in Ogg, WebM and MP4 (ogg.go, webm.go, mp4.go): each container
// gives the packets, the channels and the pre-skip (and, when it says,
// the length), and opusStream decodes them with the huddle's libopus at
// 48 kHz.

// opusPreRoll is how far before a seek target decoding starts, so the
// decoder has converged by then (RFC 7845's recommended 80 ms).
const opusPreRoll = 3840

type opusPacket struct {
	data []byte
	at   int64 // its first sample, from the stream's start (pre-skip included)
	n    int   // its samples
}

// opusPackets collects a container's packets, each placed by its TOC's
// sample count, at most maxPackets.
type opusPackets struct {
	pkts []opusPacket
	at   int64
}

// add appends packet p (a slice of the file, or a copy for one spread
// over several pages); a packet with no samples (empty, or a TOC asking
// for over 120 ms) is skipped.
func (o *opusPackets) add(p []byte) error {
	n := opusSamples(p)
	if n <= 0 {
		return nil
	}
	if len(o.pkts) >= maxPackets {
		return errTooBig
	}
	o.pkts = append(o.pkts, opusPacket{data: p, at: o.at, n: n})
	o.at += int64(n)
	return nil
}

type opusStream struct {
	codec   string
	pkts    []opusPacket
	ch      int
	preSkip int64
	total   int64 // playable frames (after the pre-skip, before any end trim)

	dec  *huddle.OpusDecoder
	next int     // the next packet to decode
	drop int64   // decoded samples to drop before output (pre-skip, a seek)
	pend []int16 // decoded, not read yet
	pos  int64   // the next frame Read returns
}

// newOpus makes the stream of o's packets; end, when positive, is the
// stream's last sample (pre-skip included: Ogg's last granule position,
// the sum of MP4's sample durations).
func newOpus(codec string, o *opusPackets, ch, preSkip int, end int64) (*opusStream, error) {
	if ch < 1 || ch > 2 {
		return nil, fmt.Errorf("%w: Opus with %d channels", errUnsupported, ch)
	}
	if len(o.pkts) == 0 {
		return nil, errors.New("goaudio: no Opus packets")
	}
	s := &opusStream{codec: codec, ch: ch, preSkip: int64(max(0, preSkip)), pkts: o.pkts}
	last := o.at
	if end > 0 && end < last {
		last = end
	}
	s.total = max(0, last-s.preSkip)
	return s, s.SeekFrame(0)
}

// opusSamples is a packet's samples at 48 kHz from its TOC (RFC 6716
// 3.1: the configuration's frame size times the frame count), 0 for an
// empty packet or one claiming over 120 ms.
func opusSamples(p []byte) int {
	if len(p) < 1 {
		return 0
	}
	cfg := p[0] >> 3
	var size int
	switch {
	case cfg < 12: // SILK: 10, 20, 40, 60 ms
		size = [4]int{480, 960, 1920, 2880}[cfg&3]
	case cfg < 16: // hybrid: 10, 20 ms
		size = [2]int{480, 960}[cfg&1]
	default: // CELT: 2.5, 5, 10, 20 ms
		size = [4]int{120, 240, 480, 960}[cfg&3]
	}
	frames := 1
	switch p[0] & 3 {
	case 1, 2:
		frames = 2
	case 3:
		if len(p) < 2 {
			return 0
		}
		frames = int(p[1] & 0x3F)
	}
	if n := frames * size; n <= 5760 { // at most 120 ms
		return n
	}
	return 0
}

func (s *opusStream) Rate() int     { return 48000 }
func (s *opusStream) Channels() int { return s.ch }
func (s *opusStream) Frames() int64 { return s.total }
func (s *opusStream) Codec() string { return s.codec }
func (s *opusStream) Close() {
	if s.dec != nil {
		s.dec.Close()
		s.dec = nil
	}
}

func (s *opusStream) SeekFrame(f int64) error {
	f = max(0, min(f, s.total))
	target := f + s.preSkip
	start := max(0, target-opusPreRoll)
	i := sort.Search(len(s.pkts), func(i int) bool { return s.pkts[i].at+int64(s.pkts[i].n) > start })
	if i == len(s.pkts) {
		i = len(s.pkts) - 1
	}
	if s.dec != nil {
		s.dec.Close()
		s.dec = nil
	}
	dec, err := huddle.NewOpusDecoder(s.ch)
	if err != nil {
		return err
	}
	s.dec, s.next, s.drop, s.pend, s.pos = dec, i, max(0, target-s.pkts[i].at), nil, f
	return nil
}

func (s *opusStream) Read(buf []float32) (int, error) {
	want := len(buf) / s.ch
	got := 0
	for got < want && s.pos < s.total {
		if len(s.pend) == 0 {
			if s.next >= len(s.pkts) || s.dec == nil {
				break
			}
			p := s.pkts[s.next]
			s.next++
			pcm, err := s.dec.Decode(p.data)
			if err != nil || len(pcm) > p.n*s.ch {
				// a packet libopus refused: its time in silence, as a lost
				// packet sounds
				pcm = make([]int16, p.n*s.ch)
			}
			s.pend = pcm
			if d := min(s.drop, int64(len(s.pend)/s.ch)); d > 0 {
				s.pend = s.pend[d*int64(s.ch):]
				s.drop -= d
			}
			continue
		}
		n := min(want-got, len(s.pend)/s.ch, int(s.total-s.pos))
		for i := 0; i < n*s.ch; i++ {
			buf[got*s.ch+i] = float32(s.pend[i]) / 32768
		}
		s.pend = s.pend[n*s.ch:]
		got += n
		s.pos += int64(n)
		if n == 0 {
			s.pend = nil // an odd sample left: drop it
		}
	}
	if got == 0 {
		return 0, io.EOF
	}
	return got, nil
}
