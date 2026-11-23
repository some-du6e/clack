package goaudio

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
)

// WebM / Matroska: the EBML header, then the Segment's Tracks (the first
// A_OPUS track: OpusHead from its CodecPrivate, else its Channels and
// CodecDelay) and Clusters (SimpleBlocks and BlockGroups, laced or not;
// DiscardPadding trims the end). Every element is read inside its parent:
// its header must fit there, and its body ends at its size or the
// parent's end, whichever comes first. Only the Segment and a Cluster may
// have an unknown size (a live recording's); such a Cluster ends at the
// first element that can't be inside one.

const (
	ebmlHeader     = 0x1A45DFA3
	mkvSegment     = 0x18538067
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
	if off < 0 || off >= len(b) {
		return 0, 0, false
	}
	n := 1
	for m := byte(0x80); n <= 4 && b[off]&m == 0; m >>= 1 {
		n++
	}
	if n > 4 || n > len(b)-off {
		return 0, 0, false
	}
	var id uint32
	for i := 0; i < n; i++ {
		id = id<<8 | uint32(b[off+i])
	}
	return id, n, true
}

// ebmlSize reads a vint at b[off:] (an element's size, a block's track
// number); unknown: all ones, a live stream's open element.
func ebmlSize(b []byte, off int) (size uint64, n int, unknown, ok bool) {
	if off < 0 || off >= len(b) {
		return 0, 0, false, false
	}
	n = 1
	m := byte(0x80)
	for n <= 8 && b[off]&m == 0 {
		n++
		m >>= 1
	}
	if n > 8 || n > len(b)-off {
		return 0, 0, false, false
	}
	v := uint64(b[off] & (m - 1))
	ones := v == uint64(m-1)
	for i := 1; i < n; i++ {
		v = v<<8 | uint64(b[off+i])
		ones = ones && b[off+i] == 0xFF
	}
	return v, n, ones, true
}

// ebmlUint is an unsigned integer element's value (its last 8 bytes).
func ebmlUint(b []byte) uint64 {
	if len(b) > 8 {
		b = b[len(b)-8:]
	}
	var v uint64
	for _, c := range b {
		v = v<<8 | uint64(c)
	}
	return v
}

// ebmlInt is a signed integer element's value.
func ebmlInt(b []byte) int64 {
	if len(b) == 0 {
		return 0
	}
	if len(b) > 8 {
		b = b[len(b)-8:]
	}
	v := int64(int8(b[0]))
	for _, c := range b[1:] {
		v = v<<8 | int64(c)
	}
	return v
}

// elem is an element inside its parent: its id, and its body [body, end)
// (end: its size's end, or the parent's).
type elem struct {
	id        uint32
	body, end int
	unknown   bool
}

// readElem reads the element at off of a parent ending at end.
func readElem(b []byte, off, end int) (elem, bool) {
	end = min(end, len(b))
	if off < 0 || off >= end {
		return elem{}, false
	}
	p := b[:end]
	id, n, ok := ebmlID(p, off)
	if !ok {
		return elem{}, false
	}
	sz, sn, unk, ok := ebmlSize(p, off+n)
	if !ok {
		return elem{}, false
	}
	e := elem{id: id, body: off + n + sn, end: end, unknown: unk}
	if !unk && sz < uint64(end-e.body) {
		e.end = e.body + int(sz)
	}
	return e, true
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

type webmTrack struct {
	num      uint64
	codec    string
	priv     []byte
	delayNS  uint64
	channels uint64
}

// webmReader is the walk's state: the Opus track, its packets, the
// discard padding.
type webmReader struct {
	audio   *webmTrack
	vorbis  bool
	pk      opusPackets
	discard int64 // ns, the blocks' DiscardPadding
	err     error
}

func openWebM(ctx context.Context, b []byte) (stream, error) {
	hdr, ok := readElem(b, 0, len(b))
	if !ok || hdr.id != ebmlHeader || hdr.unknown {
		return nil, errUnsupported
	}
	var seg elem
	for off := hdr.end; ; {
		e, ok := readElem(b, off, len(b))
		if !ok {
			return nil, fmt.Errorf("%w: WebM without a Segment", errUnsupported)
		}
		if e.id == mkvSegment {
			seg = e
			break
		}
		if e.unknown {
			return nil, fmt.Errorf("%w: WebM without a Segment", errUnsupported)
		}
		off = e.end
	}
	var w webmReader
	for pos, step := seg.body, 0; pos < seg.end; step++ {
		if done(ctx, step) {
			return nil, ctx.Err()
		}
		e, ok := readElem(b, pos, seg.end)
		if !ok {
			break
		}
		switch e.id {
		case mkvTracks:
			w.tracks(b, e)
		case mkvCluster:
			e.end = w.cluster(b, e)
			if w.err != nil {
				return nil, w.err
			}
		default:
			if e.unknown {
				e.end = seg.end // an open element this reader doesn't know: the rest is its
			}
		}
		pos = e.end
	}
	if w.audio == nil {
		if w.vorbis {
			return nil, fmt.Errorf("%w: WebM Vorbis", errUnsupported)
		}
		return nil, fmt.Errorf("%w: WebM without an Opus track", errUnsupported)
	}
	ch, pre := int(min(w.audio.channels, 255)), 0
	if d := w.audio.delayNS; d > 0 && d < 1e9 {
		pre = int(d * 48000 / 1e9)
	}
	if p := w.audio.priv; len(p) >= 19 && string(p[:8]) == "OpusHead" {
		if p[18] != 0 {
			return nil, fmt.Errorf("%w: WebM Opus mapping family %d", errUnsupported, p[18])
		}
		ch, pre = int(p[9]), int(binary.LittleEndian.Uint16(p[10:]))
	}
	s, err := newOpus("webm-opus", &w.pk, ch, pre, 0)
	if err != nil {
		return nil, err
	}
	if trim := w.discard * 48000 / 1e9; trim > 0 && trim < s.total {
		s.total -= trim
	}
	return s, nil
}

// tracks reads the Tracks element: the first Opus track.
func (w *webmReader) tracks(b []byte, t elem) {
	for p := t.body; p < t.end; {
		e, ok := readElem(b, p, t.end)
		if !ok || e.unknown {
			return
		}
		if e.id == mkvTrackEntry {
			tr := readTrack(b, e)
			switch {
			case tr.codec == "A_OPUS" && w.audio == nil:
				w.audio = &tr
			case tr.codec == "A_VORBIS":
				w.vorbis = true
			}
		}
		p = e.end
	}
}

func readTrack(b []byte, t elem) webmTrack {
	var tr webmTrack
	for q := t.body; q < t.end; {
		e, ok := readElem(b, q, t.end)
		if !ok || e.unknown {
			break
		}
		v := b[e.body:e.end]
		switch e.id {
		case mkvTrackNumber:
			tr.num = ebmlUint(v)
		case mkvCodecID:
			if len(v) <= 64 {
				tr.codec = string(bytes.TrimRight(v, "\x00"))
			}
		case mkvCodecPriv:
			tr.priv = v
		case mkvCodecDelay:
			tr.delayNS = ebmlUint(v)
		case mkvAudio:
			for r := e.body; r < e.end; {
				a, ok := readElem(b, r, e.end)
				if !ok || a.unknown {
					break
				}
				if a.id == mkvChannels {
					tr.channels = ebmlUint(b[a.body:a.end])
				}
				r = a.end
			}
		}
		q = e.end
	}
	return tr
}

// cluster reads a Cluster's blocks, and returns where it ended (an open
// one: at the first element that can't be inside it).
func (w *webmReader) cluster(b []byte, c elem) int {
	p := c.body
	for p < c.end && w.err == nil {
		e, ok := readElem(b, p, c.end)
		if !ok || c.unknown && !clusterChild(e.id) || e.unknown {
			break
		}
		switch e.id {
		case mkvSimpleBlock:
			w.block(b[e.body:e.end])
		case mkvBlockGroup:
			w.group(b, e)
		}
		p = e.end
	}
	if c.unknown {
		return p
	}
	return c.end
}

// group reads a BlockGroup: its Block and DiscardPadding.
func (w *webmReader) group(b []byte, g elem) {
	for q := g.body; q < g.end && w.err == nil; {
		e, ok := readElem(b, q, g.end)
		if !ok || e.unknown {
			return
		}
		switch e.id {
		case mkvBlock:
			w.block(b[e.body:e.end])
		case mkvDiscardPad:
			// at most a second a block, an hour in all (a longer pad is no
			// Opus stream's)
			if d := ebmlInt(b[e.body:e.end]); d > 0 && d <= 1e9 && w.discard < 3600e9 {
				w.discard += d
			}
		}
		q = e.end
	}
}

// block takes a SimpleBlock's or a Block's frames of the Opus track.
func (w *webmReader) block(body []byte) {
	if w.audio == nil || w.err != nil {
		return
	}
	tn, tl, _, ok := ebmlSize(body, 0)
	if !ok || tn != w.audio.num || len(body)-tl < 3 {
		return
	}
	flags := body[tl+2]
	data := body[tl+3:]
	lacing := (flags >> 1) & 3
	if lacing == 0 {
		w.err = w.pk.add(data)
		return
	}
	for _, p := range unlace(data, lacing) {
		if w.err = w.pk.add(p); w.err != nil {
			return
		}
	}
}

// unlace splits a laced block's frames (1 Xiph, 2 fixed, 3 EBML); nil when
// the lacing doesn't fit the block.
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
			for {
				if p >= len(d) {
					return nil
				}
				v := int(d[p])
				p++
				sizes[i] += v
				if v != 255 {
					break
				}
			}
		}
	case 3: // EBML: the first size, then signed differences
		v, n, _, ok := ebmlSize(d, p)
		if !ok || v > uint64(len(d)) {
			return nil
		}
		sizes[0], p = int(v), p+n
		for i := 1; i < count-1; i++ {
			v, n, _, ok := ebmlSize(d, p)
			if !ok {
				return nil
			}
			s := int64(sizes[i-1]) + int64(v) - (int64(1)<<(7*n-1) - 1)
			if s < 0 || s > int64(len(d)) {
				return nil
			}
			sizes[i], p = int(s), p+n
		}
	case 2: // fixed: equal parts
		rest := len(d) - p
		if rest%count != 0 {
			return nil
		}
		for i := range sizes {
			sizes[i] = rest / count
		}
	}
	if kind != 2 {
		used := 0
		for _, s := range sizes[:count-1] {
			used += s
			if used > len(d)-p {
				return nil
			}
		}
		sizes[count-1] = len(d) - p - used
	}
	out := make([][]byte, 0, count)
	for _, s := range sizes {
		if s < 0 || s > len(d)-p {
			return out
		}
		out = append(out, d[p:p+s])
		p += s
	}
	return out
}
