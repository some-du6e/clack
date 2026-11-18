package media

import (
	"encoding/binary"
	"math"
	"time"
)

// What a file's boxes say about it (ISO BMFF: MP4, QuickTime), read from
// the bytes as the source brings them: the ftyp brand and the video
// track's sample entry for the one log line a file gets (the answer to
// whether Slack's mp4 is H.264 that both OS decoders take), its duration
// and size for the fake backend.

// MP4Info is a file's probe.
type MP4Info struct {
	// Brand is ftyp's major brand ("isom", "mp42", "qt  "), "" without
	// an ftyp.
	Brand string
	// Entry is the video track's sample entry: "avc1", "hvc1", "hev1",
	// "av01", "vp09"…; "" until the moov is read, "none" for a moov with
	// no video track.
	Entry string
	// MoovAt is the moov box's offset, -1 unknown.
	MoovAt int64
	// NextAt is where the bytes the probe needs next are (a demuxer reads
	// there next), -1 once it knows all it will.
	NextAt int64
	// Duration is the movie's (mvhd), Size the video track's (tkhd).
	Duration time.Duration
	Width    int
	Height   int
}

// probeKeep is how much of the file's start and of its moov a probe keeps.
const probeKeep = 64 << 10

// probe reads a file's boxes from the bytes the source brings, in any
// order: the file's first probeKeep bytes, then the top-level boxes from
// there to the moov, and the moov's first probeKeep bytes.
type probe struct {
	head    []byte // the file's first bytes, contiguous
	walkAt  int64  // the next top-level box to read, past head
	hdr     []byte // the bytes at walkAt seen so far (a box header)
	moov    []byte // the moov's first bytes, from moovAt
	moovLen int64  // the moov box's size (0 for to the end)
	info    MP4Info
	parsed  bool // the moov is read (or there is none to read)
}

func newProbe() *probe { return &probe{walkAt: -1, info: MP4Info{MoovAt: -1}} }

// result is the probe's info, with where it reads next.
func (p *probe) result() MP4Info {
	info := p.info
	switch {
	case p.parsed:
		info.NextAt = -1
	case info.MoovAt >= 0:
		info.NextAt = info.MoovAt + int64(len(p.moov))
	case p.walkAt >= 0:
		info.NextAt = p.walkAt + int64(len(p.hdr))
	default:
		info.NextAt = int64(len(p.head))
	}
	return info
}

// ProbeMP4 is what b, a file's bytes from its start, says of it.
func ProbeMP4(b []byte) MP4Info {
	p := newProbe()
	for off := 0; off < len(b) && !p.done(); off += 32 << 10 {
		p.feed(int64(off), b[off:min(len(b), off+32<<10)])
	}
	p.finish()
	return p.result()
}

// done is whether the probe knows all it will.
func (p *probe) done() bool { return p.parsed }

// feed hands the probe b, the file's bytes at off.
func (p *probe) feed(off int64, b []byte) {
	if p.parsed || len(b) == 0 {
		return
	}
	if n := int64(len(p.head)); off <= n && n < probeKeep && off+int64(len(b)) > n {
		take := b[n-off:]
		if int64(len(take)) > probeKeep-n {
			take = take[:probeKeep-n]
		}
		p.head = append(p.head, take...)
		p.walkHead()
	}
	if p.walkAt >= 0 && p.info.MoovAt < 0 {
		p.feedHeader(off, b)
	}
	if p.info.MoovAt >= 0 && !p.parsed {
		p.feedMoov(off, b)
	}
}

// walkHead reads the top-level boxes in head: ftyp, a moov inside it, or
// where the box after head's last whole one starts.
func (p *probe) walkHead() {
	var at int64
	for {
		size, typ, hl, ok := boxHeader(p.head[at:], int64(len(p.head))-at)
		if !ok {
			if int64(len(p.head))-at >= 16 { // a whole header, and not a box
				p.parsed, p.info.Entry = true, "none"
				return
			}
			if int64(len(p.head)) >= probeKeep {
				p.walkAt = at // the box header straddles head's end
			}
			return
		}
		if typ == "ftyp" && p.info.Brand == "" && at+hl+4 <= int64(len(p.head)) {
			p.info.Brand = string(p.head[at+hl : at+hl+4])
		}
		if typ == "moov" {
			p.info.MoovAt, p.moovLen, p.walkAt = at, size, -1
			p.moov = append(p.moov[:0], p.head[at:min(int64(len(p.head)), at+probeKeep)]...)
			p.tryMoov()
			return
		}
		if size == 0 { // to the end of the file: nothing after it
			p.parsed = true
			p.info.Entry = "none"
			return
		}
		if size >= int64(len(p.head))-at {
			if size > math.MaxInt64-at { // no file is that long: not a box
				p.parsed, p.info.Entry = true, "none"
				return
			}
			p.walkAt = at + size // a box running past head: read on there
			p.hdr = p.hdr[:0]
			return
		}
		at += size
	}
}

// feedHeader collects the header at walkAt, then walks on from it.
func (p *probe) feedHeader(off int64, b []byte) {
	for p.walkAt >= 0 && p.info.MoovAt < 0 {
		have := p.walkAt + int64(len(p.hdr))
		if off > have || off+int64(len(b)) <= have {
			return
		}
		take := b[have-off:]
		if need := 16 - len(p.hdr); len(take) > need {
			take = take[:need]
		}
		p.hdr = append(p.hdr, take...)
		size, typ, _, ok := boxHeader(p.hdr, 1<<62)
		if !ok {
			if len(p.hdr) >= 16 { // not a box: nothing to play
				p.walkAt, p.parsed, p.info.Entry = -1, true, "none"
			}
			return
		}
		if typ == "moov" {
			p.info.MoovAt, p.moovLen = p.walkAt, size
			p.walkAt = -1
			return
		}
		if size == 0 {
			p.parsed, p.info.Entry, p.walkAt = true, "none", -1
			return
		}
		if size > math.MaxInt64-p.walkAt { // no file is that long: not a box
			p.walkAt, p.parsed, p.info.Entry = -1, true, "none"
			return
		}
		p.walkAt += size
		p.hdr = p.hdr[:0]
	}
}

// feedMoov collects the moov's first bytes and reads them once they're all
// there.
func (p *probe) feedMoov(off int64, b []byte) {
	want := int64(probeKeep)
	if p.moovLen > 0 && p.moovLen < want {
		want = p.moovLen
	}
	have := p.info.MoovAt + int64(len(p.moov))
	if int64(len(p.moov)) < want && off <= have && off+int64(len(b)) > have {
		take := b[have-off:]
		if rest := want - int64(len(p.moov)); int64(len(take)) > rest {
			take = take[:rest]
		}
		p.moov = append(p.moov, take...)
	}
	if int64(len(p.moov)) >= want {
		p.tryMoov()
	}
}

// finish reads what the probe has of the moov, at the end of the file
// (a moov shorter than probeKeep that the walk saw end the file).
func (p *probe) finish() {
	if !p.parsed && p.info.MoovAt >= 0 && len(p.moov) > 8 {
		p.tryMoov()
	}
}

// tryMoov reads the moov's boxes it has: mvhd's duration, and the first
// video track's tkhd size and stsd sample entry.
func (p *probe) tryMoov() {
	m := p.moov
	size, typ, hl, ok := boxHeader(m, int64(len(m)))
	if !ok || typ != "moov" {
		return
	}
	end := min(size, int64(len(m)))
	if size == 0 {
		end = int64(len(m))
	}
	if int64(len(m)) < min(size, probeKeep) && size != 0 {
		return // not all of it yet
	}
	p.parsed = true
	p.info.Entry = "none"
	for at := hl; at < end; {
		csize, ctyp, chl, ok := boxHeader(m[at:end], end-at)
		if !ok || csize == 0 {
			break
		}
		last := csize >= end-at // runs to (or past) what's kept
		cend := end
		if !last {
			cend = at + csize
		}
		body := m[at+chl : cend]
		switch ctyp {
		case "mvhd":
			if d, ok := mvhdDuration(body); ok {
				p.info.Duration = d
			}
		case "trak":
			if entry, w, h, ok := videoTrack(body); ok && p.info.Entry == "none" {
				p.info.Entry, p.info.Width, p.info.Height = entry, w, h
			}
		}
		if last {
			break
		}
		at += csize
	}
	if p.info.Entry == "none" && size != 0 && size > int64(len(m)) {
		// the moov runs past what's kept (a long video's sample tables):
		// a video track may be in what wasn't read
		p.info.Entry = "?"
	}
}

// boxHeader reads a box header from b, whose box may run to avail bytes
// from b's start: its size (0: to the end of the file), type and header
// length.
func boxHeader(b []byte, avail int64) (size int64, typ string, hl int64, ok bool) {
	if len(b) < 8 {
		return 0, "", 0, false
	}
	size = int64(binary.BigEndian.Uint32(b))
	typ = string(b[4:8])
	hl = 8
	if size == 1 {
		if len(b) < 16 {
			return 0, "", 0, false
		}
		size = int64(binary.BigEndian.Uint64(b[8:]))
		hl = 16
	}
	if size != 0 && size < hl {
		return 0, "", 0, false
	}
	for _, c := range []byte(typ) {
		if c < 0x20 || c > 0x7e {
			return 0, "", 0, false
		}
	}
	return size, typ, hl, true
}

// children calls f with each box inside b (a box's body).
func children(b []byte, f func(typ string, body []byte) bool) {
	for at := int64(0); at < int64(len(b)); {
		size, typ, hl, ok := boxHeader(b[at:], int64(len(b))-at)
		if !ok {
			return
		}
		if size == 0 || size > int64(len(b))-at {
			size = int64(len(b)) - at
		}
		if !f(typ, b[at+hl:at+size]) {
			return
		}
		at += size
	}
}

func mvhdDuration(b []byte) (time.Duration, bool) {
	var scale, dur uint64
	switch {
	case len(b) >= 20 && b[0] == 0:
		scale, dur = uint64(binary.BigEndian.Uint32(b[12:])), uint64(binary.BigEndian.Uint32(b[16:]))
	case len(b) >= 32 && b[0] == 1:
		scale, dur = uint64(binary.BigEndian.Uint32(b[20:])), binary.BigEndian.Uint64(b[24:])
	default:
		return 0, false
	}
	if scale == 0 {
		return 0, false
	}
	d := float64(dur) / float64(scale) * float64(time.Second)
	if d >= math.MaxInt64 {
		return 0, false
	}
	return time.Duration(d), true
}

// videoTrack reads a trak: whether its handler is video, its tkhd size and
// its first sample entry.
func videoTrack(trak []byte) (entry string, w, h int, ok bool) {
	var video bool
	children(trak, func(typ string, body []byte) bool {
		switch typ {
		case "tkhd":
			// width and height are the last 8 bytes, 16.16 fixed point
			if n := len(body); n > 0 && ((body[0] == 0 && n >= 84) || (body[0] == 1 && n >= 96)) {
				w = int(binary.BigEndian.Uint32(body[n-8:]) >> 16)
				h = int(binary.BigEndian.Uint32(body[n-4:]) >> 16)
			}
		case "mdia":
			children(body, func(typ string, body []byte) bool {
				switch typ {
				case "hdlr":
					video = len(body) >= 12 && string(body[8:12]) == "vide"
				case "minf":
					children(body, func(typ string, body []byte) bool {
						if typ != "stbl" {
							return true
						}
						children(body, func(typ string, body []byte) bool {
							// stsd: version and flags, an entry count, then
							// the entries, each a box named by its format
							if typ == "stsd" && len(body) >= 16 {
								entry = string(body[12:16])
								return false
							}
							return true
						})
						return false
					})
				}
				return true
			})
		}
		return true
	})
	return entry, w, h, video && entry != ""
}
