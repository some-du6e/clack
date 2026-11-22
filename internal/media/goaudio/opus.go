package goaudio

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sort"

	"github.com/kazzmir/opus-go/ogg"

	"github.com/some-du6e/clack/internal/huddle"
)

// Opus in Ogg, WebM and MP4: each container gives the packets, the
// channels and the pre-skip (and, when it says, the length), and
// opusStream decodes them with the huddle's libopus at 48 kHz.

// opusPreRoll is how far before a seek target decoding starts, so the
// decoder has converged by then (RFC 7845's recommended 80 ms).
const opusPreRoll = 3840

type opusPacket struct {
	data []byte
	at   int64 // its first sample, from the stream's start (pre-skip included)
	n    int   // its samples
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

// newOpus makes the stream; end, when positive, is the stream's last
// sample (pre-skip included: Ogg's last granule position).
func newOpus(codec string, packets [][]byte, ch, preSkip int, end int64) (*opusStream, error) {
	if ch < 1 || ch > 2 {
		return nil, fmt.Errorf("%w: Opus with %d channels", errUnsupported, ch)
	}
	s := &opusStream{codec: codec, ch: ch, preSkip: int64(preSkip)}
	var at int64
	for _, p := range packets {
		n := opusSamples(p)
		if n <= 0 {
			continue
		}
		s.pkts = append(s.pkts, opusPacket{data: p, at: at, n: n})
		at += int64(n)
	}
	if len(s.pkts) == 0 {
		return nil, errors.New("goaudio: no Opus packets")
	}
	last := at
	if end > 0 && end < at {
		last = end
	}
	s.total = max(0, last-s.preSkip)
	return s, s.SeekFrame(0)
}

// opusSamples is a packet's samples at 48 kHz from its TOC (RFC 6716
// 3.1: the configuration's frame size times the frame count).
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
	}
	dec, err := huddle.NewOpusDecoder(s.ch)
	if err != nil {
		return err
	}
	s.dec, s.next, s.drop, s.pend, s.pos = dec, i, target-s.pkts[i].at, nil, f
	return nil
}

func (s *opusStream) Read(buf []float32) (int, error) {
	want := len(buf) / s.ch
	got := 0
	for got < want && s.pos < s.total {
		if len(s.pend) == 0 {
			if s.next >= len(s.pkts) {
				break
			}
			pcm, err := s.dec.Decode(s.pkts[s.next].data)
			s.next++
			if err != nil {
				// a packet libopus refused: its time in silence, as a
				// lost packet sounds
				pcm = make([]int16, s.pkts[s.next-1].n*s.ch)
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
	}
	if got == 0 {
		return 0, io.EOF
	}
	return got, nil
}

// ---- Ogg ----

func openOgg(b []byte) (stream, error) {
	if bytes.Contains(b[:min(len(b), 512)], []byte("\x01vorbis")) {
		return nil, fmt.Errorf("%w: Ogg Vorbis", errUnsupported)
	}
	r, err := ogg.NewOpusReader(bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("%w: Ogg: %v", errUnsupported, err)
	}
	if r.Head.ChannelMappingFamily != 0 {
		return nil, fmt.Errorf("%w: Ogg Opus mapping family %d", errUnsupported, r.Head.ChannelMappingFamily)
	}
	var pkts [][]byte
	var end int64
	for {
		p, err := r.ReadAudioPacket()
		if err == io.EOF {
			break
		}
		if err != nil {
			if len(pkts) > 0 {
				break // a truncated tail: play what's there
			}
			return nil, fmt.Errorf("goaudio: Ogg: %w", err)
		}
		pkts = append(pkts, p.Data)
		if p.GranuleValid {
			end = int64(p.GranulePos)
		}
	}
	return newOpus("ogg-opus", pkts, int(r.Head.Channels), int(r.Head.PreSkip), end)
}

// ---- WebM / Matroska ----

const (
	ebmlHeader     = 0x1A45DFA3
	mkvSegment     = 0x18538067
	mkvInfo        = 0x1549A966
	mkvTracks      = 0x1654AE6B
	mkvTrackEntry  = 0xAE
	mkvTrackNumber = 0xD7
	mkvCodecID     = 0x86
	mkvCodecPriv   = 0x63A2
	mkvCodecDelay  = 0x56AA
	mkvAudio       = 0xE1
	mkvChannels    = 0x9F
	mkvCluster     = 0x1F43B675
	mkvTimecode    = 0xE7
	mkvSimpleBlock = 0xA3
	mkvBlockGroup  = 0xA0
	mkvBlock       = 0xA1
	mkvDiscardPad  = 0x75A2
)

// ebmlID reads an element ID (its length marker kept) at b[off:].
func ebmlID(b []byte, off int) (uint32, int, bool) {
	if off >= len(b) {
		return 0, 0, false
	}
	n := 1
	for m := byte(0x80); n <= 4 && b[off]&m == 0; m >>= 1 {
		n++
	}
	if n > 4 || off+n > len(b) {
		return 0, 0, false
	}
	var id uint32
	for i := 0; i < n; i++ {
		id = id<<8 | uint32(b[off+i])
	}
	return id, n, true
}

// ebmlSize reads an element size (or a block's track number) at b[off:];
// unknown is the all-ones size of a live stream's open element.
func ebmlSize(b []byte, off int) (size int64, n int, unknown, ok bool) {
	if off >= len(b) {
		return 0, 0, false, false
	}
	n = 1
	m := byte(0x80)
	for n <= 8 && b[off]&m == 0 {
		n++
		m >>= 1
	}
	if n > 8 || off+n > len(b) {
		return 0, 0, false, false
	}
	v := int64(b[off] & (m - 1))
	ones := v == int64(m-1)
	for i := 1; i < n; i++ {
		v = v<<8 | int64(b[off+i])
		ones = ones && b[off+i] == 0xFF
	}
	return v, n, ones, true
}

func ebmlUint(b []byte) int64 {
	var v int64
	for _, c := range b {
		v = v<<8 | int64(c)
	}
	return v
}

type webmTrack struct {
	num      int64
	codec    string
	priv     []byte
	delayNS  int64
	channels int64
}

// clusterChild reports whether id can be inside a Cluster (so an open,
// unknown-size Cluster ends at the first element that can't).
func clusterChild(id uint32) bool {
	switch id {
	case mkvTimecode, mkvSimpleBlock, mkvBlockGroup, 0xA7, 0xAB, 0x5854, 0xAF, 0xEC, 0xBF:
		return true
	}
	return false
}

func openWebM(b []byte) (stream, error) {
	// the EBML header, then the Segment
	id, n, ok := ebmlID(b, 0)
	if !ok || id != ebmlHeader {
		return nil, errUnsupported
	}
	sz, sn, _, ok := ebmlSize(b, n)
	if !ok {
		return nil, errUnsupported
	}
	off := n + sn + int(sz)
	id, n, ok = ebmlID(b, off)
	if !ok || id != mkvSegment {
		return nil, errors.New("goaudio: WebM without a Segment")
	}
	sz, sn, unk, ok := ebmlSize(b, off+n)
	if !ok {
		return nil, errors.New("goaudio: WebM: bad Segment size")
	}
	pos, end := off+n+sn, len(b)
	if !unk && pos+int(sz) < end {
		end = pos + int(sz)
	}
	var tracks []webmTrack
	var audio *webmTrack
	var pkts [][]byte
	var discard int64 // ns
	block := func(body []byte) {
		tn, tl, _, ok := ebmlSize(body, 0)
		if !ok || audio == nil || tn != audio.num || len(body) < tl+3 {
			return
		}
		flags := body[tl+2]
		data := body[tl+3:]
		switch (flags >> 1) & 3 {
		case 0:
			pkts = append(pkts, data)
		default:
			pkts = append(pkts, unlace(data, (flags>>1)&3)...)
		}
	}
	for pos < end {
		id, n, ok := ebmlID(b, pos)
		if !ok {
			break
		}
		sz, sn, unk, ok := ebmlSize(b, pos+n)
		if !ok {
			break
		}
		body := pos + n + sn
		bodyEnd := end
		if !unk {
			bodyEnd = min(end, body+int(sz))
		}
		switch id {
		case mkvTracks:
			tracks = parseTracks(b[body:bodyEnd])
			for i := range tracks {
				if tracks[i].codec == "A_OPUS" {
					audio = &tracks[i]
					break
				}
			}
			if audio == nil {
				for _, t := range tracks {
					if t.codec == "A_VORBIS" {
						return nil, fmt.Errorf("%w: WebM Vorbis", errUnsupported)
					}
				}
				return nil, fmt.Errorf("%w: WebM without an Opus track", errUnsupported)
			}
		case mkvCluster:
			p := body
			for p < bodyEnd {
				cid, cn, ok := ebmlID(b, p)
				if !ok || (unk && !clusterChild(cid)) {
					break
				}
				csz, csn, cunk, ok := ebmlSize(b, p+cn)
				if !ok || cunk {
					break
				}
				cb := p + cn + csn
				ce := min(bodyEnd, cb+int(csz))
				switch cid {
				case mkvSimpleBlock:
					block(b[cb:ce])
				case mkvBlockGroup:
					for q := cb; q < ce; {
						gid, gn, ok := ebmlID(b, q)
						gsz, gsn, _, ok2 := ebmlSize(b, q+gn)
						if !ok || !ok2 {
							break
						}
						gb, ge := q+gn+gsn, min(ce, q+gn+gsn+int(gsz))
						switch gid {
						case mkvBlock:
							block(b[gb:ge])
						case mkvDiscardPad:
							discard += ebmlUint(b[gb:ge])
						}
						q = ge
					}
				}
				p = ce
			}
			if unk {
				bodyEnd = p
			}
		default:
			if unk {
				bodyEnd = end // an open element this reader doesn't know: the rest is its
			}
		}
		pos = bodyEnd
	}
	if audio == nil {
		return nil, fmt.Errorf("%w: WebM without tracks", errUnsupported)
	}
	ch, pre := int(audio.channels), int(audio.delayNS*48000/1e9)
	if len(audio.priv) >= 19 && string(audio.priv[:8]) == "OpusHead" {
		ch = int(audio.priv[9])
		pre = int(binary.LittleEndian.Uint16(audio.priv[10:]))
	}
	s, err := newOpus("webm-opus", pkts, ch, pre, 0)
	if err != nil {
		return nil, err
	}
	if trim := discard * 48000 / 1e9; trim > 0 && trim < s.total {
		s.total -= trim
	}
	return s, nil
}

func parseTracks(b []byte) []webmTrack {
	var out []webmTrack
	for p := 0; p < len(b); {
		id, n, ok := ebmlID(b, p)
		sz, sn, _, ok2 := ebmlSize(b, p+n)
		if !ok || !ok2 {
			break
		}
		body, end := p+n+sn, min(len(b), p+n+sn+int(sz))
		if id == mkvTrackEntry {
			var t webmTrack
			for q := body; q < end; {
				tid, tn, ok := ebmlID(b, q)
				tsz, tsn, _, ok2 := ebmlSize(b, q+tn)
				if !ok || !ok2 {
					break
				}
				tb, te := q+tn+tsn, min(end, q+tn+tsn+int(tsz))
				switch tid {
				case mkvTrackNumber:
					t.num = ebmlUint(b[tb:te])
				case mkvCodecID:
					t.codec = string(bytes.TrimRight(b[tb:te], "\x00"))
				case mkvCodecPriv:
					t.priv = b[tb:te]
				case mkvCodecDelay:
					t.delayNS = ebmlUint(b[tb:te])
				case mkvAudio:
					for r := tb; r < te; {
						aid, an, ok := ebmlID(b, r)
						asz, asn, _, ok2 := ebmlSize(b, r+an)
						if !ok || !ok2 {
							break
						}
						ab, ae := r+an+asn, min(te, r+an+asn+int(asz))
						if aid == mkvChannels {
							t.channels = ebmlUint(b[ab:ae])
						}
						r = ae
					}
				}
				q = te
			}
			out = append(out, t)
		}
		p = end
	}
	return out
}

// unlace splits a laced block's frames: 1 Xiph, 2 fixed, 3 EBML.
func unlace(d []byte, kind byte) [][]byte {
	if len(d) < 1 {
		return nil
	}
	count := int(d[0]) + 1
	p := 1
	sizes := make([]int, count)
	switch kind {
	case 1: // Xiph: each size but the last as 255s and a remainder
		for i := 0; i < count-1; i++ {
			for p < len(d) {
				sizes[i] += int(d[p])
				p++
				if d[p-1] != 255 {
					break
				}
			}
		}
	case 3: // EBML: the first size, then signed differences
		v, n, _, ok := ebmlSize(d, p)
		if !ok {
			return nil
		}
		sizes[0], p = int(v), p+n
		for i := 1; i < count-1; i++ {
			v, n, _, ok := ebmlSize(d, p)
			if !ok {
				return nil
			}
			bias := int64(1)<<(7*n-1) - 1
			sizes[i] = sizes[i-1] + int(v-bias)
			p += n
		}
	case 2: // fixed: equal parts
		each := (len(d) - p) / count
		for i := range sizes {
			sizes[i] = each
		}
	}
	used := 0
	for _, s := range sizes[:count-1] {
		used += s
	}
	if kind != 2 {
		sizes[count-1] = len(d) - p - used
	}
	var out [][]byte
	for _, s := range sizes {
		if s < 0 || p+s > len(d) {
			return out
		}
		out = append(out, d[p:p+s])
		p += s
	}
	return out
}

// ---- MP4 ----

func openMP4(b []byte) (stream, error) {
	m := &mp4File{b: b}
	if err := m.walk(0, len(b), 0); err != nil {
		return nil, err
	}
	switch {
	case m.codec == "mp4a":
		return nil, fmt.Errorf("%w: AAC", errUnsupported)
	case m.codec != "Opus":
		return nil, fmt.Errorf("%w: MP4 audio %q", errUnsupported, m.codec)
	}
	pkts := m.samples()
	if len(pkts) == 0 {
		return nil, errors.New("goaudio: MP4 without samples")
	}
	// the samples' durations end the stream (the last packet's is cut to
	// what was recorded: Chromium's Mp4Muxer's last one is a few ms)
	var end int64
	if d := m.sttsDur + m.fragDur; d > 0 && m.scale > 0 {
		end = d * 48000 / int64(m.scale)
	}
	return newOpus("mp4-opus", pkts, m.channels, m.preSkip, end)
}

// mp4File reads the first audio track of an MP4: its sample entry, its
// sample table (stsz, stsc, stco/co64, stts) and its fragments' runs.
type mp4File struct {
	b        []byte
	codec    string
	channels int
	preSkip  int
	track    uint32 // the audio track's id (tkhd)
	curTrack uint32
	curScale uint32 // the trak's mdhd timescale, until its hdlr says what it is
	scale    uint32 // the audio track's timescale
	inAudio  bool
	sizes    []uint32
	chunks   []int64
	stsc     [][3]uint32 // first chunk, samples per chunk, description
	frags    [][]byte    // fragment samples, in order
	// the samples' durations in scale: the sample table's (stts) and the
	// fragments' runs' (trun, tfhd, the trex defaults)
	sttsDur, fragDur  int64
	trexDur, trexSize uint32
}

func (m *mp4File) walk(off, end int, depth int) error {
	for off+8 <= end {
		size := int(binary.BigEndian.Uint32(m.b[off:]))
		typ := string(m.b[off+4 : off+8])
		body := off + 8
		switch size {
		case 0:
			size = end - off
		case 1:
			if off+16 > end {
				return errors.New("goaudio: short MP4 box")
			}
			size, body = int(binary.BigEndian.Uint64(m.b[off+8:])), off+16
		}
		if size < body-off || off+size > end {
			return fmt.Errorf("goaudio: an MP4 %q box doesn't fit", typ)
		}
		bEnd := off + size
		bb := m.b[body:bEnd]
		switch typ {
		case "moov", "mdia", "minf", "stbl", "mvex", "edts":
			if err := m.walk(body, bEnd, depth+1); err != nil {
				return err
			}
		case "trak":
			m.inAudio, m.curTrack, m.curScale = false, 0, 0
			if err := m.walk(body, bEnd, depth+1); err != nil {
				return err
			}
		case "tkhd":
			if len(bb) >= 24 {
				if bb[0] == 1 {
					m.curTrack = binary.BigEndian.Uint32(bb[20:])
				} else {
					m.curTrack = binary.BigEndian.Uint32(bb[12:])
				}
			}
		case "mdhd":
			switch {
			case len(bb) >= 24 && bb[0] == 1:
				m.curScale = binary.BigEndian.Uint32(bb[20:])
			case len(bb) >= 16:
				m.curScale = binary.BigEndian.Uint32(bb[12:])
			}
		case "hdlr":
			if len(bb) >= 12 && string(bb[8:12]) == "soun" && m.codec == "" {
				m.inAudio, m.track, m.scale = true, m.curTrack, m.curScale
			}
		case "stts":
			if m.inAudio && len(bb) >= 8 {
				n := int(binary.BigEndian.Uint32(bb[4:]))
				for i := 0; i < n && 8+8*i+8 <= len(bb); i++ {
					m.sttsDur += int64(binary.BigEndian.Uint32(bb[8+8*i:])) * int64(binary.BigEndian.Uint32(bb[12+8*i:]))
				}
			}
		case "trex":
			if len(bb) >= 20 && m.track != 0 && binary.BigEndian.Uint32(bb[4:]) == m.track {
				m.trexDur, m.trexSize = binary.BigEndian.Uint32(bb[12:]), binary.BigEndian.Uint32(bb[16:])
			}
		case "stsd":
			if m.inAudio && len(bb) >= 16 {
				m.sampleEntry(bb[8:])
			}
		case "stsz":
			if m.inAudio && len(bb) >= 12 {
				one, n := binary.BigEndian.Uint32(bb[4:]), int(binary.BigEndian.Uint32(bb[8:]))
				if n > len(bb) { // a sample takes at least a byte
					return errors.New("goaudio: bad stsz")
				}
				for i := 0; i < n; i++ {
					if one != 0 {
						m.sizes = append(m.sizes, one)
					} else if 12+4*i+4 <= len(bb) {
						m.sizes = append(m.sizes, binary.BigEndian.Uint32(bb[12+4*i:]))
					}
				}
			}
		case "stsc":
			if m.inAudio && len(bb) >= 8 {
				n := int(binary.BigEndian.Uint32(bb[4:]))
				for i := 0; i < n && 8+12*i+12 <= len(bb); i++ {
					e := bb[8+12*i:]
					m.stsc = append(m.stsc, [3]uint32{binary.BigEndian.Uint32(e), binary.BigEndian.Uint32(e[4:]), binary.BigEndian.Uint32(e[8:])})
				}
			}
		case "stco", "co64":
			if m.inAudio && len(bb) >= 8 {
				n, w := int(binary.BigEndian.Uint32(bb[4:])), 4
				if typ == "co64" {
					w = 8
				}
				for i := 0; i < n && 8+w*i+w <= len(bb); i++ {
					if w == 4 {
						m.chunks = append(m.chunks, int64(binary.BigEndian.Uint32(bb[8+4*i:])))
					} else {
						m.chunks = append(m.chunks, int64(binary.BigEndian.Uint64(bb[8+8*i:])))
					}
				}
			}
		case "moof":
			if err := m.moof(off, body, bEnd); err != nil {
				return err
			}
		}
		off = bEnd
	}
	return nil
}

// sampleEntry reads the stsd's first entry: its type, and an Opus one's
// dOps (channels, pre-skip).
func (m *mp4File) sampleEntry(b []byte) {
	if len(b) < 8 {
		return
	}
	n := int(binary.BigEndian.Uint32(b))
	if n > len(b) || n < 36 {
		return
	}
	m.codec = string(b[4:8])
	m.channels = int(binary.BigEndian.Uint16(b[24:]))
	for p := 36; p+8 <= n; {
		sz := int(binary.BigEndian.Uint32(b[p:]))
		if sz < 8 || p+sz > n {
			break
		}
		if string(b[p+4:p+8]) == "dOps" && sz >= 8+11 {
			d := b[p+8:]
			m.channels = int(d[1])
			m.preSkip = int(binary.BigEndian.Uint16(d[2:]))
		}
		p += sz
	}
}

// moof reads a fragment's runs of the audio track.
func (m *mp4File) moof(moofOff, body, end int) error {
	for off := body; off+8 <= end; {
		size := int(binary.BigEndian.Uint32(m.b[off:]))
		if size < 8 || off+size > end {
			return errors.New("goaudio: bad moof")
		}
		if string(m.b[off+4:off+8]) == "traf" {
			m.traf(moofOff, off+8, off+size)
		}
		off += size
	}
	return nil
}

func (m *mp4File) traf(moofOff, body, end int) {
	base := int64(moofOff)
	defDur, defSize := m.trexDur, m.trexSize
	var track uint32
	for off := body; off+8 <= end; {
		size := int(binary.BigEndian.Uint32(m.b[off:]))
		if size < 12 || off+size > end {
			return
		}
		bb := m.b[off+8 : off+size]
		flags := binary.BigEndian.Uint32(bb) & 0xffffff
		switch string(m.b[off+4 : off+8]) {
		case "tfhd":
			if len(bb) < 8 {
				return
			}
			track = binary.BigEndian.Uint32(bb[4:])
			p := 8
			if flags&0x1 != 0 && p+8 <= len(bb) {
				base = int64(binary.BigEndian.Uint64(bb[p:]))
				p += 8
			}
			if flags&0x2 != 0 {
				p += 4
			}
			if flags&0x8 != 0 && p+4 <= len(bb) {
				defDur = binary.BigEndian.Uint32(bb[p:])
				p += 4
			}
			if flags&0x10 != 0 && p+4 <= len(bb) {
				defSize = binary.BigEndian.Uint32(bb[p:])
			}
		case "trun":
			if m.track != 0 && track != 0 && track != m.track {
				break
			}
			if len(bb) < 8 {
				return
			}
			n := int(binary.BigEndian.Uint32(bb[4:]))
			p := 8
			pos := base
			if flags&0x1 != 0 && p+4 <= len(bb) {
				pos = base + int64(int32(binary.BigEndian.Uint32(bb[p:])))
				p += 4
			}
			if flags&0x4 != 0 {
				p += 4
			}
			if n > len(bb) {
				return
			}
			for i := 0; i < n; i++ {
				sz, dur := defSize, defDur
				if flags&0x100 != 0 && p+4 <= len(bb) {
					dur = binary.BigEndian.Uint32(bb[p:])
					p += 4
				}
				if flags&0x200 != 0 && p+4 <= len(bb) {
					sz = binary.BigEndian.Uint32(bb[p:])
					p += 4
				}
				if flags&0x400 != 0 {
					p += 4
				}
				if flags&0x800 != 0 {
					p += 4
				}
				if pos < 0 || pos+int64(sz) > int64(len(m.b)) {
					return
				}
				m.frags = append(m.frags, m.b[pos:pos+int64(sz)])
				m.fragDur += int64(dur)
				pos += int64(sz)
			}
		}
		off += size
	}
}

// samples is the track's samples: the sample table's, then the
// fragments'.
func (m *mp4File) samples() [][]byte {
	var out [][]byte
	if len(m.sizes) > 0 && len(m.chunks) > 0 && len(m.stsc) > 0 {
		s := 0
		for ci, off := range m.chunks {
			per := uint32(0)
			for _, e := range m.stsc { // the last entry starting at or before this chunk (1-based)
				if int(e[0]) <= ci+1 {
					per = e[1]
				}
			}
			for k := uint32(0); k < per && s < len(m.sizes); k++ {
				sz := int64(m.sizes[s])
				if off+sz > int64(len(m.b)) {
					return out
				}
				out = append(out, m.b[off:off+sz])
				off += sz
				s++
			}
		}
	}
	return append(out, m.frags...)
}
