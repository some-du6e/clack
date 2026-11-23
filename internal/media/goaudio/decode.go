package goaudio

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"

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
	// Close lets go of its decoder.
	Close()
}

// masked is a stream that says where its channels go (a WAV's channel
// mask): the downmix's layout (render.go).
type masked interface {
	ChannelMask() uint32
}

// osOpen, where the OS has a decoder for what Go can't decode (Media
// Foundation's source reader on Windows: AAC), opens b with it; nil
// elsewhere. mime is the file's type, a hint.
var osOpen func(b []byte, mime string) (stream, error)

// DecodesAAC reports whether this build decodes AAC (Media Foundation's,
// on Windows with cgo): elsewhere an aac rendition is for the OS backend
// or url_private.
func DecodesAAC() bool { return osOpen != nil }

// errUnsupported is a file this package can't decode: the element's
// MEDIA_ERR_SRC_NOT_SUPPORTED.
var errUnsupported = errors.New("goaudio: unsupported audio")

// The readers are total (no input panics them, hangs them or makes them
// allocate far past the file; FuzzMP4 and the others look for one): every
// size and count a file states is checked against the bytes left before
// anything is made from it, and these cap the rest.
const (
	// maxPackets is the most packets (or MP3 frames) a file may hold: 2^21,
	// 11.6 hours of 20 ms Opus packets (a voice clip has under 5,000).
	maxPackets = 1 << 21
	// maxChannels is Chromium's media::limits::kMaxChannels: a file with
	// more doesn't play there either. More than two are downmixed before
	// the resampler (render.go).
	maxChannels = 32
	// maxDepth is how deep MP4 boxes may nest (moov, trak, mdia, minf,
	// stbl: 5).
	maxDepth = 12
	// checkEvery is how many steps a reader takes between looks at its
	// context: Close cancels it, and the reader ends at the next look.
	checkEvery = 1024
)

var errTooBig = fmt.Errorf("%w: more than %d packets", errUnsupported, maxPackets)

// done reports whether ctx ended, looking every checkEvery steps.
func done(ctx context.Context, step int) bool {
	return step%checkEvery == 0 && ctx.Err() != nil
}

// open sniffs the file and returns its stream: Go's decoders, then the
// OS's for what they don't support.
func open(ctx context.Context, b []byte, mime string) (stream, error) {
	s, err := openGo(ctx, b)
	if err != nil && errors.Is(err, errUnsupported) && osOpen != nil && ctx.Err() == nil {
		if ds, oerr := osOpen(b, mime); oerr == nil {
			return ds, nil
		}
	}
	if err != nil {
		return nil, err
	}
	return s, nil
}

func openGo(ctx context.Context, b []byte) (stream, error) {
	switch {
	case len(b) >= 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WAVE":
		return openWAV(b)
	case len(b) >= 4 && string(b[:4]) == "OggS":
		return openOgg(ctx, b)
	case len(b) >= 4 && binary.BigEndian.Uint32(b) == ebmlHeader:
		return openWebM(ctx, b)
	case len(b) >= 8 && string(b[4:8]) == "ftyp":
		return openMP4(ctx, b)
	case isMP3(b):
		return openMP3(ctx, b)
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
	frame    int    // bytes per frame
	mask     uint32 // WAVE_FORMAT_EXTENSIBLE's channel mask, 0 none
	pos      int64
}

// openWAV reads a RIFF WAVE file's chunks: each chunk's size must fit
// what's left (data's may run to the end: a stream that didn't fill its
// size in, or a cut file), at most maxChannels channels.
func openWAV(b []byte) (stream, error) {
	w := &wavStream{}
	var fmtOK bool
	for off := 12; len(b)-off >= 8; {
		id := string(b[off : off+4])
		n := uint64(binary.LittleEndian.Uint32(b[off+4:]))
		body := off + 8
		left := uint64(len(b) - body)
		if id == "data" && (n == 0 || n > left) {
			n = left
		}
		if n > left {
			break // a chunk past the end: what came before is all there is
		}
		end := body + int(n)
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
			if w.format == 0xFFFE && n >= 40 { // WAVE_FORMAT_EXTENSIBLE: the mask, the subformat's first two bytes
				w.mask = binary.LittleEndian.Uint32(f[20:])
				w.format = int(binary.LittleEndian.Uint16(f[24:]))
			}
			fmtOK = true
		case "data":
			if w.data == nil {
				w.data = b[body:end]
			}
		}
		next := uint64(end) + n&1 // chunks are word-aligned
		if next > uint64(len(b)) {
			break
		}
		off = int(next)
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
	if w.ch < 1 || w.ch > maxChannels {
		return nil, fmt.Errorf("%w: WAV with %d channels", errUnsupported, w.ch)
	}
	if w.rate < 1000 || w.rate > 384000 || w.frame != w.ch*w.bits/8 {
		return nil, fmt.Errorf("%w: WAV of %d channels at %d Hz", errUnsupported, w.ch, w.rate)
	}
	return w, nil
}

func (w *wavStream) Rate() int           { return w.rate }
func (w *wavStream) Channels() int       { return w.ch }
func (w *wavStream) Frames() int64       { return int64(len(w.data) / w.frame) }
func (w *wavStream) Codec() string       { return "pcm" }
func (w *wavStream) Close()              {}
func (w *wavStream) ChannelMask() uint32 { return w.mask }
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
		if v != v || v > 1e6 || v < -1e6 { // a float WAV's NaN or wild value: silence
			v = 0
		}
		buf[i] = v
	}
	w.pos += n
	return int(n), nil
}

// ---- MP3 ----

// MP3 is go-mp3's to decode, from frames this reader finds itself: go-mp3
// v0.3.4's own Seek takes one frame size for all (the last frame's) and
// indexed past its frame table when a file's frames change MPEG version,
// and its ID3 skip allocated whatever size the tag said. So the tags are
// skipped here (a syncsafe size that fits), the frames are indexed here
// with go-mp3's own header rules (frameheader's IsValid, FrameSize and the
// Layer III, MPEG-1/2, bitrate and frame-size checks frame.Read and
// maindata make), stopping where go-mp3 would stop or where the sample
// rate changes, and a seek starts a new go-mp3 decoder a few frames
// before the target over a reader that can't seek (so it scans nothing),
// dropping the samples before it.

// mp3Frame is a frame: where it starts in the file, its first sample.
type mp3Frame struct {
	off int64
	at  int64
}

type mp3Stream struct {
	b      []byte // the frames' bytes (no ID3v1 tag at the end)
	frames []mp3Frame
	total  int64 // the frames' samples
	rate   int
	d      *mp3.Decoder
	pos    int64 // the next sample Read returns
	drop   int64 // decoded samples to drop (a seek starts frames early)
	raw    []byte
}

// readerOnly hides a reader's Seek: go-mp3 then scans no frame table.
type readerOnly struct{ io.Reader }

func openMP3(ctx context.Context, b []byte) (stream, error) {
	end := len(b)
	if end >= 128 && string(b[end-128:end-125]) == "TAG" { // ID3v1 at the end
		end -= 128
	}
	b = b[:end]
	frames, total, rate, err := indexMP3(ctx, b, skipID3(b))
	if err != nil {
		return nil, err
	}
	if len(frames) == 0 || rate == 0 {
		return nil, fmt.Errorf("%w: no MP3 frames go-mp3 decodes", errUnsupported)
	}
	m := &mp3Stream{b: b, frames: frames, total: total, rate: rate}
	if err := m.SeekFrame(0); err != nil {
		return nil, err
	}
	return m, nil
}

// skipID3 is where the ID3v2 tags at b's start end (several may follow
// each other): each a 10-byte header whose size is syncsafe (every byte
// under 0x80) and fits the file, plus a footer when it says so. What isn't
// such a tag is where frames are looked for.
func skipID3(b []byte) int {
	off := 0
	for i := 0; i < 16 && len(b)-off >= 10 && string(b[off:off+3]) == "ID3"; i++ {
		h := b[off:]
		if h[3] == 0xFF || h[4] == 0xFF || h[6]|h[7]|h[8]|h[9] >= 0x80 {
			break
		}
		size := 10 + (int(h[6])<<21 | int(h[7])<<14 | int(h[8])<<7 | int(h[9]))
		if h[5]&0x10 != 0 { // a footer
			size += 10
		}
		if size > len(b)-off {
			break
		}
		off += size
	}
	return off
}

// mp3Layer3Bitrates are go-mp3's Layer III bitrates: MPEG-1's, MPEG-2's.
var mp3Layer3Bitrates = [2][15]int{
	{0, 32000, 40000, 48000, 56000, 64000, 80000, 96000, 112000, 128000, 160000, 192000, 224000, 256000, 320000},
	{0, 8000, 16000, 24000, 32000, 40000, 48000, 56000, 64000, 80000, 96000, 112000, 128000, 144000, 160000},
}

// mp3Header reads the frame header at off as go-mp3 does: valid is
// frameheader.IsValid (go-mp3 resyncs past what isn't), decodable what
// frame.Read and maindata.Read take (MPEG-1 or MPEG-2, Layer III, a
// bitrate, a frame of at most 2000 bytes whose main data, after its side
// info and header, is 0 to 1500 bytes). size is FrameSize, samples its
// samples per channel, rate its sample rate.
func mp3Header(b []byte, off int) (size, samples, rate int, valid, decodable bool) {
	if len(b)-off < 4 {
		return 0, 0, 0, false, false
	}
	w := binary.BigEndian.Uint32(b[off:])
	ver := (w >> 19) & 3   // 3 MPEG-1, 2 MPEG-2, 0 MPEG-2.5, 1 reserved
	layer := (w >> 17) & 3 // 1 Layer III, 0 reserved
	br := (w >> 12) & 15
	sr := (w >> 10) & 3
	if w&0xffe00000 != 0xffe00000 || ver == 1 || br == 15 || sr == 3 || layer == 0 || w&3 == 2 {
		return 0, 0, 0, false, false
	}
	if ver == 0 || layer != 1 || br == 0 {
		return 0, 0, 0, true, false
	}
	lsf := 0
	if ver != 3 {
		lsf = 1
	}
	rate = [3]int{44100, 48000, 32000}[sr] >> lsf
	size = ((144*mp3Layer3Bitrates[lsf][br])/rate + int(w>>9&1)) >> lsf
	side := 32 // MPEG-1 stereo; 17 mono
	mono := (w>>6)&3 == 3
	switch {
	case lsf == 0 && mono:
		side = 17
	case lsf == 1 && mono:
		side = 9
	case lsf == 1:
		side = 17
	}
	main := size - side - 4
	if w>>16&1 == 0 { // a CRC
		main -= 2
	}
	if size > 2000 || main < 0 || main > 1500 {
		return 0, 0, 0, true, false
	}
	return size, 576 * (2 >> lsf), rate, true, true
}

// indexMP3 finds the frames from start: a header go-mp3 takes as valid
// and decodes, then the next where it ends, skipping bytes that aren't
// one as go-mp3 resyncs; it stops at a frame go-mp3 wouldn't decode, one
// cut off by the end, or one at another sample rate.
func indexMP3(ctx context.Context, b []byte, start int) ([]mp3Frame, int64, int, error) {
	var frames []mp3Frame
	var total int64
	rate := 0
	for off, step := start, 0; off < len(b); step++ {
		if done(ctx, step) {
			return nil, 0, 0, ctx.Err()
		}
		size, samples, r, valid, ok := mp3Header(b, off)
		if !valid {
			off++
			continue
		}
		if !ok || rate != 0 && r != rate || size > len(b)-off {
			break
		}
		if len(frames) >= maxPackets {
			return nil, 0, 0, errTooBig
		}
		rate = r
		frames = append(frames, mp3Frame{off: int64(off), at: total})
		total += int64(samples)
		off += size
	}
	return frames, total, rate, nil
}

func (m *mp3Stream) Rate() int     { return m.rate }
func (m *mp3Stream) Channels() int { return 2 } // go-mp3 decodes to stereo
func (m *mp3Stream) Frames() int64 { return m.total }
func (m *mp3Stream) Codec() string { return "mp3" }
func (m *mp3Stream) Close()        { m.d = nil }

// SeekFrame starts the next Read at sample f: a go-mp3 decoder from up to
// 16 frames (2 KB) before f's frame (the bit reservoir reaches 511 bytes
// back), the samples before f dropped.
func (m *mp3Stream) SeekFrame(f int64) error {
	f = max(0, min(f, m.total))
	k := sort.Search(len(m.frames), func(i int) bool { return m.frames[i].at > f }) - 1
	k = max(k, 0)
	j := k
	for j > 0 && k-j < 16 && m.frames[k].off-m.frames[j].off < 2048 {
		j--
	}
	// a decoder starts with no reservoir, so its first frame is read from
	// the wrong bits: dropped, and should go-mp3 refuse it, the next frame
	// starts it instead
	var err error
	for ; j <= k; j++ {
		var d *mp3.Decoder
		if d, err = mp3.NewDecoder(readerOnly{bytes.NewReader(m.b[m.frames[j].off:])}); err == nil {
			m.d, m.pos, m.drop = d, f, f-m.frames[j].at
			return nil
		}
	}
	return fmt.Errorf("goaudio: MP3 at frame %d: %w", k, err)
}

func (m *mp3Stream) Read(buf []float32) (int, error) {
	want := min(int64(len(buf)/2), m.total-m.pos)
	if want <= 0 || m.d == nil {
		return 0, io.EOF
	}
	got := 0
	for got < int(want) {
		need := int(want) - got
		if m.drop > 0 {
			need = int(min(m.drop, 4096))
		}
		if cap(m.raw) < need*4 {
			m.raw = make([]byte, need*4)
		}
		raw := m.raw[:need*4]
		n, err := io.ReadFull(m.d, raw)
		frames := n / 4
		if m.drop > 0 {
			m.drop -= int64(frames)
		} else {
			for i := 0; i < frames*2; i++ {
				buf[got*2+i] = float32(int16(binary.LittleEndian.Uint16(raw[2*i:]))) / 32768
			}
			got += frames
			m.pos += int64(frames)
		}
		if err != nil || frames == 0 {
			break // the end of what go-mp3 decodes
		}
	}
	if got == 0 {
		return 0, io.EOF
	}
	return got, nil
}
