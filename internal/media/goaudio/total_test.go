package goaudio

// The readers are total (review of c38b91b8): a file's bytes come from
// whoever uploaded it, so no input may panic, hang or make the reader
// allocate far past the file. Each case is a file shaped to hit one of the
// gaps the review found: a 64-bit box size that wrapped the bounds check,
// chunk and fragment offsets past the end, boxes nested without end, a
// sample table walked once per chunk per entry, WebM children whose
// headers run past their parent, an OpusTags comment count opus-go
// allocated from, a WAV with 65535 channels, an MP3 whose frames change
// MPEG version (go-mp3's Seek indexed its frame table by the last frame's
// size) and an ID3 tag whose size go-mp3 allocated from. FuzzMP4,
// FuzzWebM, FuzzOgg, FuzzWAV and FuzzMP3 (fuzz_test.go) look for more.

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"
)

// parse opens b as the player does: the readers under test.
func parse(b []byte, mime string) (stream, error) { return open(context.Background(), b, mime) }

// totally runs f, failing the test if it panics, takes over limit, or
// allocates over 64 MB.
func totally(t *testing.T, limit time.Duration, f func() error) error {
	t.Helper()
	type result struct {
		err   error
		panic any
		alloc uint64
	}
	done := make(chan result, 1)
	go func() {
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		var r result
		func() {
			defer func() { r.panic = recover() }()
			r.err = f()
		}()
		runtime.ReadMemStats(&after)
		r.alloc = after.TotalAlloc - before.TotalAlloc
		done <- r
	}()
	select {
	case r := <-done:
		if r.panic != nil {
			t.Errorf("panicked: %v", r.panic)
		}
		if r.alloc > 64<<20 {
			t.Errorf("allocated %d MB for a file of a few KB", r.alloc>>20)
		}
		return r.err
	case <-time.After(limit):
		t.Errorf("took over %v", limit)
		return nil
	}
}

// exercise opens b and, if it opens, reads a little, seeks near its end
// and to the middle, and reads again.
func exercise(b []byte, mime string) error {
	s, err := parse(b, mime)
	if err != nil {
		return err
	}
	defer s.Close()
	if s.Channels() < 1 || s.Channels() > 32 {
		return fmt.Errorf("%d channels", s.Channels())
	}
	buf := make([]float32, 256*s.Channels())
	for i := 0; i < 4; i++ {
		if _, err := s.Read(buf); err != nil {
			break
		}
	}
	for _, f := range []int64{s.Frames() - 100, s.Frames() / 2, 0} {
		if err := s.SeekFrame(max(0, f)); err != nil {
			return err
		}
		if _, err := s.Read(buf); err != nil {
			break
		}
	}
	return nil
}

func be16(v uint16) []byte { return binary.BigEndian.AppendUint16(nil, v) }
func be32(v uint32) []byte { return binary.BigEndian.AppendUint32(nil, v) }
func be64(v uint64) []byte { return binary.BigEndian.AppendUint64(nil, v) }
func le16(v uint16) []byte { return binary.LittleEndian.AppendUint16(nil, v) }
func le32(v uint32) []byte { return binary.LittleEndian.AppendUint32(nil, v) }

// mp4Box is an MP4 box of typ around parts.
func mp4Box(typ string, parts ...[]byte) []byte {
	body := bytes.Join(parts, nil)
	return append(append(be32(uint32(8+len(body))), typ...), body...)
}

// mp4Full is a full box: version 0, flags.
func mp4Full(typ string, flags uint32, parts ...[]byte) []byte {
	return mp4Box(typ, append(be32(flags), bytes.Join(parts, nil)...))
}

var mp4Ftyp = mp4Box("ftyp", []byte("isom"), be32(0), []byte("isomopus"))

// mp4Moov is an Opus track's moov: n samples of size each, in one chunk
// per sample at chunk offsets offs (co64), and mvex's trex.
func mp4Moov(stbl ...[]byte) []byte {
	dOps := mp4Box("dOps", []byte{0, 1}, be16(312), be32(48000), be16(0), []byte{0})
	entry := mp4Box("Opus", make([]byte, 6), be16(1), make([]byte, 8), be16(1), be16(16), be16(0), be16(0), be32(48000<<16), dOps)
	stsd := mp4Full("stsd", 0, be32(1), entry)
	return mp4Box("moov",
		mp4Box("trak",
			mp4Full("tkhd", 0, make([]byte, 8), be32(1), make([]byte, 60)),
			mp4Box("mdia",
				mp4Full("mdhd", 0, make([]byte, 8), be32(48000), be32(0), make([]byte, 4)),
				mp4Full("hdlr", 0, be32(0), []byte("soun"), make([]byte, 12), []byte{0}),
				mp4Box("minf", mp4Box("stbl", append([][]byte{stsd}, stbl...)...)))),
		mp4Box("mvex", mp4Full("trex", 0, be32(1), be32(1), be32(0), be32(0), be32(0))))
}

// ebml is an EBML element: id's bytes, a size of n bytes (1 to 8) for
// len(body), the body.
func ebml(id uint32, n int, body []byte) []byte {
	var out []byte
	for s := 24; s >= 0; s -= 8 {
		if b := byte(id >> s); b != 0 || len(out) > 0 {
			out = append(out, b)
		}
	}
	size := uint64(len(body)) | 1<<(7*n) // the length marker
	for i := n - 1; i >= 0; i-- {
		out = append(out, byte(size>>(8*i)))
	}
	return append(out, body...)
}

// ebmlRaw is an element's id and size as given (sizeBytes: the size's
// vint), its body after.
func ebmlRaw(id []byte, sizeBytes []byte, body []byte) []byte {
	return append(append(append([]byte(nil), id...), sizeBytes...), body...)
}

var webmHeader = ebml(0x1A45DFA3, 1, ebml(0x4282, 1, []byte("webm")))

// oggCRC is Ogg's page checksum (polynomial 0x04C11DB7, MSB first).
func oggCRC(b []byte) uint32 {
	var crc uint32
	for _, c := range b {
		crc ^= uint32(c) << 24
		for i := 0; i < 8; i++ {
			if crc&0x80000000 != 0 {
				crc = crc<<1 ^ 0x04C11DB7
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

// oggPage is one Ogg page holding packet whole (flags: 2 begins the
// stream), its checksum filled in.
func oggPage(flags byte, granule uint64, seq uint32, packet []byte) []byte {
	var segs []byte
	for n := len(packet); ; n -= 255 {
		if n < 255 {
			segs = append(segs, byte(n))
			break
		}
		segs = append(segs, 255)
	}
	h := append([]byte("OggS"), 0, flags)
	h = binary.LittleEndian.AppendUint64(h, granule)
	h = append(h, le32(0x5eed)...)
	h = append(h, le32(seq)...)
	h = append(h, 0, 0, 0, 0) // the checksum
	h = append(h, byte(len(segs)))
	h = append(h, segs...)
	page := append(h, packet...)
	binary.LittleEndian.PutUint32(page[22:], oggCRC(page))
	return page
}

var opusHead = append([]byte("OpusHead"), 1, 1, 0x38, 0x01, 0x80, 0xBB, 0, 0, 0, 0, 0)

// wavFile is a RIFF WAVE file of fmt's fields and data.
func wavFile(format, ch uint16, rate uint32, align, bits uint16, data []byte) []byte {
	f := append(le16(format), le16(ch)...)
	f = append(f, le32(rate)...)
	f = append(f, le32(rate*uint32(align))...)
	f = append(f, le16(align)...)
	f = append(f, le16(bits)...)
	body := append([]byte("WAVE"), "fmt "...)
	body = append(body, le32(uint32(len(f)))...)
	body = append(body, f...)
	body = append(body, "data"...)
	body = append(body, le32(uint32(len(data)))...)
	body = append(body, data...)
	return append(append([]byte("RIFF"), le32(uint32(len(body)))...), body...)
}

// mp3Frames is n frames of MPEG audio header h, framesize bytes each
// (the header, then zeros: silence).
func mp3Frames(h uint32, framesize, n int) []byte {
	var out []byte
	for i := 0; i < n; i++ {
		f := make([]byte, framesize)
		binary.BigEndian.PutUint32(f, h)
		out = append(out, f...)
	}
	return out
}

func TestReadersAreTotal(t *testing.T) {
	type tc struct {
		name, mime string
		b          []byte
		// want, when set, must be in the error (the reader refused the file
		// for this reason)
		want string
		// limit is how long the reader may take (default 2 s)
		limit time.Duration
		// check, when set, checks the stream that opened
		check func(stream) error
	}
	var cases []tc

	// a 64-bit largesize whose sum with the box's offset wraps past the
	// bounds check
	large := append(append(append([]byte(nil), mp4Ftyp...), be32(1)...), "moov"...)
	large = append(large, be64(1<<63-uint64(len(mp4Ftyp)))...)
	cases = append(cases, tc{name: "mp4 largesize wraps", mime: "audio/mp4", b: append(large, make([]byte, 64)...)})

	// a co64 chunk offset past the end of the file
	mdat := mp4Box("mdat", bytes.Repeat([]byte{0xF8, 1, 2, 3}, 4))
	co64 := mp4Moov(mp4Full("stts", 0, be32(1), be32(1), be32(960)), mp4Full("stsc", 0, be32(1), be32(1), be32(1), be32(1)),
		mp4Full("stsz", 0, be32(0), be32(1), be32(4)), mp4Full("co64", 0, be32(1), be64(1<<63)))
	cases = append(cases, tc{name: "mp4 co64 offset past the end", mime: "audio/mp4", b: bytes.Join([][]byte{mp4Ftyp, co64, mdat}, nil)})

	// a fragment's base_data_offset at the top of int64: the sample's end
	// wraps past the bounds check
	frag := mp4Moov(mp4Full("stts", 0, be32(0)), mp4Full("stsc", 0, be32(0)), mp4Full("stsz", 0, be32(0), be32(0)), mp4Full("stco", 0, be32(0)))
	moof := mp4Box("moof", mp4Full("mfhd", 0, be32(1)),
		mp4Box("traf",
			mp4Full("tfhd", 0x1|0x8|0x10, be32(1), be64(1<<63-8), be32(960), be32(16)),
			mp4Full("trun", 0x1, be32(1), be32(0))))
	cases = append(cases, tc{name: "mp4 tfhd base offset wraps", mime: "audio/mp4", b: bytes.Join([][]byte{mp4Ftyp, frag, moof, mdat}, nil)})

	// boxes nested 100,000 deep: moov in moov in moov, a free box inside
	const levels = 100000
	deep := make([]byte, 8*(levels+1))
	for i := 0; i <= levels; i++ {
		binary.BigEndian.PutUint32(deep[8*i:], uint32(len(deep)-8*i))
		copy(deep[8*i+4:], "moov")
	}
	copy(deep[8*levels+4:], "free")
	cases = append(cases, tc{name: "mp4 boxes nested without end", mime: "audio/mp4", b: append(append([]byte(nil), mp4Ftyp...), deep...), want: "nested"})

	// 60,000 chunks and 60,000 stsc entries: once per chunk per entry is
	// 3.6 billion steps
	const n = 60000
	var stco, stsc, stsz []byte
	data := bytes.Repeat([]byte{0xF8}, n)
	moovAt := len(mp4Ftyp)
	stsc = append(be32(0), be32(n)...)
	stsz = append(be32(0), be32(0)...)
	stsz = append(stsz, be32(n)...)
	for i := 0; i < n; i++ {
		stsc = append(stsc, be32(uint32(i+1))...)
		stsc = append(stsc, be32(1)...)
		stsc = append(stsc, be32(1)...)
		stsz = append(stsz, be32(1)...)
	}
	stco = append(be32(0), be32(n)...)
	tables := func(base int) []byte {
		out := append([]byte(nil), stco...)
		for i := 0; i < n; i++ {
			out = append(out, be32(uint32(base+i))...)
		}
		return out
	}
	build := func(base int) []byte {
		return mp4Moov(mp4Full("stts", 0, be32(1), be32(n), be32(960)), mp4Box("stsc", stsc), mp4Box("stsz", stsz), mp4Box("stco", tables(base)))
	}
	m0 := build(0)
	quad := bytes.Join([][]byte{mp4Ftyp, build(moovAt + len(m0) + 8), mp4Box("mdat", data)}, nil)
	cases = append(cases, tc{name: "mp4 sample table walked linearly", mime: "audio/mp4", b: quad, limit: time.Second})

	// WebM: a Tracks element whose size runs past its Segment's end
	tracks := ebmlRaw([]byte{0x16, 0x54, 0xAE, 0x6B}, []byte{0x01, 0, 0, 0, 0, 0, 0, 0x10}, make([]byte, 16))
	seg := ebmlRaw([]byte{0x18, 0x53, 0x80, 0x67}, []byte{0x86}, tracks)
	cases = append(cases, tc{name: "webm child past its segment", mime: "audio/webm", b: append(append([]byte(nil), webmHeader...), seg...)})

	// WebM: a Block whose size runs past its BlockGroup's end
	group := ebmlRaw([]byte{0xA0}, []byte{0x83}, []byte{0xA1, 0x01, 0})
	cluster := ebmlRaw([]byte{0x1F, 0x43, 0xB6, 0x75}, []byte{0x85}, group)
	open := ebmlRaw([]byte{0x18, 0x53, 0x80, 0x67}, []byte{0x01, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}, append(cluster, make([]byte, 16)...))
	cases = append(cases, tc{name: "webm block past its group", mime: "audio/webm", b: append(append([]byte(nil), webmHeader...), open...)})

	// Ogg: OpusTags claiming 8 million comments
	tags := append([]byte("OpusTags"), le32(0)...)
	tags = append(tags, le32(1<<23)...)
	tags = append(tags, le32(4)...)
	ogg := append(oggPage(2, 0, 0, opusHead), oggPage(0, 0, 1, tags)...)
	ogg = append(ogg, oggPage(4, 960, 2, []byte{0xF8})...)
	cases = append(cases, tc{name: "ogg tags count", mime: "audio/ogg", b: ogg})

	// WAV: 65535 channels of 8 bits, one frame
	cases = append(cases, tc{name: "wav channels capped", mime: "audio/wav", b: wavFile(1, 65535, 8000, 65535, 8, make([]byte, 65535)),
		check: func(s stream) error {
			if s.Channels() > 32 {
				return fmt.Errorf("%d channels (Chromium plays at most 32)", s.Channels())
			}
			return nil
		}})

	// MP3: 20 MPEG-1 frames, then 20 MPEG-2 ones, and a seek near the end
	mixed := append(mp3Frames(0xFFFB9000, 417, 20), mp3Frames(0xFFF38000, 208, 20)...)
	cases = append(cases, tc{name: "mp3 frames change version", mime: "audio/mpeg", b: mixed})

	// MP3: an ID3 tag whose (not syncsafe) size says 535 MB
	id3 := append([]byte("ID3"), 4, 0, 0, 0xFF, 0xFF, 0xFF, 0xFF)
	cases = append(cases, tc{name: "mp3 id3 size", mime: "audio/mpeg", b: append(id3, mp3Frames(0xFFFB9000, 417, 4)...)})

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			limit := c.limit
			if limit == 0 {
				limit = 2 * time.Second
			}
			err := totally(t, limit, func() error {
				if c.check != nil {
					s, err := parse(c.b, c.mime)
					if err != nil {
						return err
					}
					defer s.Close()
					if err := c.check(s); err != nil {
						return fmt.Errorf("opened, but %w", err)
					}
				}
				return exercise(c.b, c.mime)
			})
			if err != nil && strings.Contains(err.Error(), "opened, but") {
				t.Error(err)
			}
			if c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)) {
				t.Errorf("error %v, want one saying %q", err, c.want)
			}
			t.Logf("%s: %v", c.name, err)
		})
	}
}
