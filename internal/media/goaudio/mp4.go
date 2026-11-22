package goaudio

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
)

// MP4 (ISO BMFF) with Opus (the voice clips Chromium's MediaRecorder
// writes, plain or fragmented): the first audio track's sample entry
// (Opus's dOps: the channels and pre-skip), its sample table (stsz, stsc,
// stco or co64, stts) and the fragments' runs (moof, traf, tfhd, trun, the
// mvex trex defaults). Every box must fit its parent (a 64-bit size too),
// boxes nest at most maxDepth deep, every count is checked against the
// bytes its box holds before anything is made from it, every sample's
// offset and size against the file, and the sample table is walked once
// (chunk by chunk, stsc's entries in step). An AAC track is the OS's
// (Media Foundation on Windows, AVFoundation on macOS).

// rd reads big-endian fields from a box's body; a read past its end sets
// bad and reads zero.
type rd struct {
	b   []byte
	p   int
	bad bool
}

func (r *rd) skip(n int) {
	if n < 0 || n > len(r.b)-r.p {
		r.bad, r.p = true, len(r.b)
		return
	}
	r.p += n
}

func (r *rd) u8() uint8 {
	if len(r.b)-r.p < 1 {
		r.bad = true
		return 0
	}
	r.p++
	return r.b[r.p-1]
}

func (r *rd) u16() uint16 {
	if len(r.b)-r.p < 2 {
		r.bad = true
		return 0
	}
	r.p += 2
	return binary.BigEndian.Uint16(r.b[r.p-2:])
}

func (r *rd) u32() uint32 {
	if len(r.b)-r.p < 4 {
		r.bad = true
		return 0
	}
	r.p += 4
	return binary.BigEndian.Uint32(r.b[r.p-4:])
}

func (r *rd) u64() uint64 {
	if len(r.b)-r.p < 8 {
		r.bad = true
		return 0
	}
	r.p += 8
	return binary.BigEndian.Uint64(r.b[r.p-8:])
}

// left is the bytes not read yet.
func (r *rd) left() int { return len(r.b) - r.p }

// isoBox is a box: its type, where it starts, and its body [body, end)
// in the file.
type isoBox struct {
	typ              string
	start, body, end int
}

// nextBox reads the box at off of a parent ending at end: false if its
// header or its size doesn't fit.
func nextBox(b []byte, off, end int) (isoBox, bool) {
	end = min(end, len(b))
	if off < 0 || end-off < 8 {
		return isoBox{}, false
	}
	size := uint64(binary.BigEndian.Uint32(b[off:]))
	typ := string(b[off+4 : off+8])
	hdr := 8
	switch size {
	case 0: // to the end of its parent (the file's last box)
		size = uint64(end - off)
	case 1: // a 64-bit size follows
		if end-off < 16 {
			return isoBox{}, false
		}
		size, hdr = binary.BigEndian.Uint64(b[off+8:]), 16
	}
	if size < uint64(hdr) || size > uint64(end-off) {
		return isoBox{}, false
	}
	return isoBox{typ: typ, start: off, body: off + hdr, end: off + int(size)}, true
}

var errTooDeep = fmt.Errorf("%w: MP4 boxes nested past %d", errUnsupported, maxDepth)

type stscEntry struct{ first, per uint32 }

// mp4File is the walk's state: the first audio track's.
type mp4File struct {
	ctx      context.Context
	b        []byte
	steps    int
	codec    string
	channels int
	family   int
	preSkip  int
	track    uint32 // the audio track's id (tkhd)
	curTrack uint32
	curScale uint32 // the trak's mdhd timescale, until its hdlr says what it is
	scale    uint32 // the audio track's timescale
	inAudio  bool
	sizes    []uint32
	chunks   []int64
	stsc     []stscEntry
	// the samples' durations in scale (capped: maxDur): the sample
	// table's (stts) and the fragments' runs' (trun, tfhd, trex)
	sttsDur, fragDur  int64
	trexDur, trexSize uint32
	// frags are the fragments' samples, in order: they follow the sample
	// table's
	frags [][]byte
	pk    opusPackets
}

// maxDur caps the summed sample durations (2^40 ticks: 254 days at
// 48 kHz), so scaling them can't overflow.
const maxDur = 1 << 40

func addDur(sum *int64, n, d uint64) {
	if n != 0 && d > maxDur/n {
		*sum = maxDur
		return
	}
	*sum = min(maxDur, *sum+int64(n*d))
}

func openMP4(ctx context.Context, b []byte) (stream, error) {
	m := &mp4File{ctx: ctx, b: b, family: -1}
	if err := m.walk(0, len(b), 0); err != nil {
		return nil, err
	}
	switch {
	case m.codec == "mp4a":
		return nil, fmt.Errorf("%w: AAC", errUnsupported)
	case m.codec != "Opus":
		return nil, fmt.Errorf("%w: MP4 audio %q", errUnsupported, m.codec)
	case m.family > 0:
		return nil, fmt.Errorf("%w: MP4 Opus mapping family %d", errUnsupported, m.family)
	}
	if err := m.tableSamples(); err != nil {
		return nil, err
	}
	for _, p := range m.frags {
		if err := m.pk.add(p); err != nil {
			return nil, err
		}
	}
	if len(m.pk.pkts) == 0 {
		return nil, errors.New("goaudio: MP4 without samples")
	}
	// the samples' durations end the stream (the last packet's is cut to
	// what was recorded: Chromium's Mp4Muxer's last one is a few ms)
	var end int64
	if d := m.sttsDur + m.fragDur; d > 0 && m.scale > 0 {
		end = d * 48000 / int64(m.scale)
	}
	s, err := newOpus("mp4-opus", &m.pk, m.channels, m.preSkip, end)
	if err != nil {
		return nil, err
	}
	return s, nil
}

// step counts one step of the walk, false once ctx ended.
func (m *mp4File) step() bool {
	m.steps++
	return !done(m.ctx, m.steps)
}

func (m *mp4File) walk(off, end, depth int) error {
	if depth > maxDepth {
		return errTooDeep
	}
	for off < end {
		if !m.step() {
			return m.ctx.Err()
		}
		bx, ok := nextBox(m.b, off, end)
		if !ok {
			return nil // what doesn't fit is the end of what's read
		}
		r := &rd{b: m.b[bx.body:bx.end]}
		switch bx.typ {
		case "moov", "mdia", "minf", "stbl", "mvex", "edts":
			if err := m.walk(bx.body, bx.end, depth+1); err != nil {
				return err
			}
		case "trak":
			m.inAudio, m.curTrack, m.curScale = false, 0, 0
			if err := m.walk(bx.body, bx.end, depth+1); err != nil {
				return err
			}
		case "tkhd":
			if v := r.u8(); v == 1 {
				r.skip(3 + 16)
			} else {
				r.skip(3 + 8)
			}
			if id := r.u32(); !r.bad {
				m.curTrack = id
			}
		case "mdhd":
			if v := r.u8(); v == 1 {
				r.skip(3 + 16)
			} else {
				r.skip(3 + 8)
			}
			if s := r.u32(); !r.bad {
				m.curScale = s
			}
		case "hdlr":
			r.skip(8)
			if h := r.u32(); !r.bad && h == 0x736f756e && m.codec == "" { // "soun"
				m.inAudio, m.track, m.scale = true, m.curTrack, m.curScale
			}
		case "stsd":
			if m.inAudio && m.codec == "" {
				r.skip(8) // version, flags, entry count
				m.sampleEntry(bx.body+r.p, bx.end)
			}
		case "stsz":
			if m.inAudio && m.sizes == nil {
				if err := m.readStsz(r); err != nil {
					return err
				}
			}
		case "stsc":
			if m.inAudio && m.stsc == nil {
				if err := m.readStsc(r); err != nil {
					return err
				}
			}
		case "stco", "co64":
			if m.inAudio && m.chunks == nil {
				if err := m.readChunks(r, bx.typ == "co64"); err != nil {
					return err
				}
			}
		case "stts":
			if m.inAudio {
				m.readStts(r)
			}
		case "trex":
			r.skip(4)
			if id := r.u32(); m.track != 0 && id == m.track {
				r.skip(4)
				m.trexDur, m.trexSize = r.u32(), r.u32()
				if r.bad {
					m.trexDur, m.trexSize = 0, 0
				}
			}
		case "moof":
			if err := m.moof(bx, depth+1); err != nil {
				return err
			}
		}
		off = bx.end
	}
	return nil
}

// sampleEntry reads stsd's first entry, at off in a box ending at end:
// its type, and an Opus one's dOps (channels, pre-skip, mapping family).
func (m *mp4File) sampleEntry(off, end int) {
	e, ok := nextBox(m.b, off, end)
	if !ok {
		return
	}
	m.codec = e.typ
	r := &rd{b: m.b[e.body:e.end]}
	r.skip(16) // reserved, data_reference_index, reserved
	m.channels = int(r.u16())
	r.skip(10) // samplesize, pre_defined, reserved, samplerate
	if r.bad {
		return
	}
	for p := e.body + r.p; p < e.end; {
		c, ok := nextBox(m.b, p, e.end)
		if !ok {
			return
		}
		if c.typ == "dOps" {
			d := &rd{b: m.b[c.body:c.end]}
			d.skip(1) // version
			ch, pre := d.u8(), d.u16()
			d.skip(6) // input sample rate, output gain
			fam := d.u8()
			if !d.bad {
				m.channels, m.preSkip, m.family = int(ch), int(pre), int(fam)
			}
		}
		p = c.end
	}
}

// readStsz reads the sample sizes: a count of 4-byte entries must fit the
// box, every one at most maxPackets.
func (m *mp4File) readStsz(r *rd) error {
	r.skip(4)
	one, n := r.u32(), r.u32()
	if r.bad {
		return nil
	}
	if n > maxPackets {
		return errTooBig
	}
	if one == 0 && uint64(n)*4 > uint64(r.left()) {
		return fmt.Errorf("%w: stsz's %d samples don't fit it", errUnsupported, n)
	}
	m.sizes = make([]uint32, n)
	for i := range m.sizes {
		if one != 0 {
			m.sizes[i] = one
		} else {
			m.sizes[i] = r.u32()
		}
	}
	return nil
}

func (m *mp4File) readStsc(r *rd) error {
	r.skip(4)
	n := r.u32()
	if r.bad || uint64(n)*12 > uint64(r.left()) {
		return fmt.Errorf("%w: stsc's entries don't fit it", errUnsupported)
	}
	m.stsc = make([]stscEntry, n)
	for i := range m.stsc {
		m.stsc[i] = stscEntry{first: r.u32(), per: r.u32()}
		r.skip(4) // sample_description_index
	}
	return nil
}

func (m *mp4File) readChunks(r *rd, wide bool) error {
	r.skip(4)
	n := r.u32()
	w := uint64(4)
	if wide {
		w = 8
	}
	if r.bad || n > maxPackets || uint64(n)*w > uint64(r.left()) {
		return fmt.Errorf("%w: the chunk offsets don't fit their box", errUnsupported)
	}
	m.chunks = make([]int64, n)
	for i := range m.chunks {
		var off uint64
		if wide {
			off = r.u64()
		} else {
			off = uint64(r.u32())
		}
		if off > uint64(len(m.b)) {
			off = uint64(len(m.b)) // past the end: no sample of it fits
		}
		m.chunks[i] = int64(off)
	}
	return nil
}

func (m *mp4File) readStts(r *rd) {
	r.skip(4)
	n := r.u32()
	for i := uint32(0); i < n && r.left() >= 8; i++ {
		count, delta := r.u32(), r.u32()
		addDur(&m.sttsDur, uint64(count), uint64(delta))
	}
}

// tableSamples takes the sample table's samples, chunk by chunk: chunk i
// (from 1) holds the samples per chunk of the last stsc entry whose first
// chunk is i or before, its samples in a row from its offset.
func (m *mp4File) tableSamples() error {
	if len(m.sizes) == 0 || len(m.chunks) == 0 || len(m.stsc) == 0 {
		return nil
	}
	s, j := 0, -1
	for ci, off := range m.chunks {
		if !m.step() {
			return m.ctx.Err()
		}
		for j+1 < len(m.stsc) && uint64(m.stsc[j+1].first) <= uint64(ci+1) {
			j++
		}
		if j < 0 {
			continue
		}
		for k := uint32(0); k < m.stsc[j].per && s < len(m.sizes); k++ {
			sz := int64(m.sizes[s])
			if sz > int64(len(m.b))-off {
				return nil // a sample past the end: the file's cut there
			}
			if err := m.pk.add(m.b[off : off+sz]); err != nil {
				return err
			}
			off += sz
			s++
		}
		if s == len(m.sizes) {
			break
		}
	}
	return nil
}

// moof reads a fragment's runs of the audio track.
func (m *mp4File) moof(moof isoBox, depth int) error {
	if depth > maxDepth {
		return errTooDeep
	}
	for off := moof.body; off < moof.end; {
		if !m.step() {
			return m.ctx.Err()
		}
		bx, ok := nextBox(m.b, off, moof.end)
		if !ok {
			return nil
		}
		if bx.typ == "traf" {
			if err := m.traf(int64(moof.start), bx); err != nil {
				return err
			}
		}
		off = bx.end
	}
	return nil
}

func (m *mp4File) traf(moofStart int64, traf isoBox) error {
	base := moofStart // default-base-is-moof, and ffmpeg's and Chromium's base
	defDur, defSize := m.trexDur, m.trexSize
	var track uint32
	for off := traf.body; off < traf.end; {
		if !m.step() {
			return m.ctx.Err()
		}
		bx, ok := nextBox(m.b, off, traf.end)
		if !ok {
			return nil
		}
		r := &rd{b: m.b[bx.body:bx.end]}
		switch bx.typ {
		case "tfhd":
			flags := r.u32() & 0xffffff
			track = r.u32()
			if flags&0x1 != 0 {
				b := r.u64()
				if b > uint64(len(m.b)) {
					return nil // a base past the end: no sample of the run fits
				}
				base = int64(b)
			}
			if flags&0x2 != 0 {
				r.skip(4)
			}
			if flags&0x8 != 0 {
				defDur = r.u32()
			}
			if flags&0x10 != 0 {
				defSize = r.u32()
			}
			if r.bad {
				return nil
			}
		case "trun":
			if m.track != 0 && track != 0 && track != m.track {
				break
			}
			if err := m.trun(r, base, defDur, defSize); err != nil {
				return err
			}
		}
		off = bx.end
	}
	return nil
}

// trun reads a run: its sample count checked against the bytes its
// per-sample fields take (or, with none, against the file's bytes), each
// sample's place checked against the file.
func (m *mp4File) trun(r *rd, base int64, defDur, defSize uint32) error {
	flags := r.u32() & 0xffffff
	n := r.u32()
	pos := base
	if flags&0x1 != 0 {
		pos = base + int64(int32(r.u32()))
	}
	if flags&0x4 != 0 {
		r.skip(4)
	}
	if r.bad || pos < 0 || pos > int64(len(m.b)) {
		return nil
	}
	rec := 0
	for _, f := range []uint32{0x100, 0x200, 0x400, 0x800} {
		if flags&f != 0 {
			rec += 4
		}
	}
	if n > maxPackets || rec > 0 && uint64(n)*uint64(rec) > uint64(r.left()) || rec == 0 && uint64(n) > uint64(len(m.b)) {
		return fmt.Errorf("%w: a run of %d samples doesn't fit", errUnsupported, n)
	}
	for i := uint32(0); i < n; i++ {
		if i%checkEvery == 0 && m.ctx.Err() != nil {
			return m.ctx.Err()
		}
		sz, dur := defSize, defDur
		if flags&0x100 != 0 {
			dur = r.u32()
		}
		if flags&0x200 != 0 {
			sz = r.u32()
		}
		if flags&0x400 != 0 {
			r.skip(4)
		}
		if flags&0x800 != 0 {
			r.skip(4)
		}
		if r.bad || sz == 0 || int64(sz) > int64(len(m.b))-pos {
			return nil // a sample past the end: the fragment's cut there
		}
		if len(m.frags) >= maxPackets {
			return errTooBig
		}
		m.frags = append(m.frags, m.b[pos:pos+int64(sz)])
		addDur(&m.fragDur, 1, uint64(dur))
		pos += int64(sz)
	}
	return nil
}
