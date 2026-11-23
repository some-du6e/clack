package clip

import (
	"encoding/binary"
	"errors"
	"fmt"
	"time"
)

// Info is what Probe reads from an audio MP4: enough to check a clip's
// container, codec and length without trusting the writer.
type Info struct {
	MajorBrand   string
	MinorVersion uint32
	Brands       []string
	// Top is the top-level boxes in file order ("ftyp", "moov", "moof",
	// "mdat", …).
	Top []string
	// Codec is the sample entry's type ("Opus", "mp4a").
	Codec string
	// Channels and SampleRate are the sample entry's.
	Channels, SampleRate int
	// PreSkip and InputRate are dOps's (Opus only).
	PreSkip, InputRate int
	// Handler is hdlr's handler_type ("soun").
	Handler string
	// Timescale is mdhd's.
	Timescale int
	// The duration fields as written (Chromium writes milliseconds in all
	// three) and mvhd's next_track_id.
	MovieDuration, TrackDuration, MediaDuration uint64
	MovieTimescale                              int
	NextTrackID                                 uint32
	// Fragments are the moof/mdat pairs in order.
	Fragments []Fragment
}

// Fragment is one moof and its samples.
type Fragment struct {
	Seq       uint32
	BaseTime  uint64   // tfdt, at Timescale
	Durations []uint32 // trun's, at Timescale
	Packets   [][]byte // the samples, slices of the probed file
}

// Packets returns every sample in order.
func (i *Info) Packets() [][]byte {
	var out [][]byte
	for _, f := range i.Fragments {
		out = append(out, f.Packets...)
	}
	return out
}

// Samples is the timeline's end: the last fragment's base time plus its
// sample durations (at Timescale).
func (i *Info) Samples() uint64 {
	if len(i.Fragments) == 0 {
		return 0
	}
	f := i.Fragments[len(i.Fragments)-1]
	n := f.BaseTime
	for _, d := range f.Durations {
		n += uint64(d)
	}
	return n
}

// Duration is Samples as a time.
func (i *Info) Duration() time.Duration {
	if i.Timescale <= 0 {
		return 0
	}
	return time.Duration(i.Samples()) * time.Second / time.Duration(i.Timescale)
}

type mp4Box struct {
	typ      string
	off, end int // the whole box in the file
	body     int // where its payload starts
}

// boxes splits b[off:end] into boxes.
func boxes(b []byte, off, end int) ([]mp4Box, error) {
	var out []mp4Box
	for off < end {
		if end-off < 8 {
			return nil, fmt.Errorf("clip: %d stray bytes at %d", end-off, off)
		}
		size := int(binary.BigEndian.Uint32(b[off:]))
		typ := string(b[off+4 : off+8])
		body := off + 8
		switch size {
		case 0:
			size = end - off
		case 1:
			if end-off < 16 {
				return nil, errors.New("clip: short large box")
			}
			large := binary.BigEndian.Uint64(b[off+8:])
			if large > uint64(end-off) {
				return nil, fmt.Errorf("clip: %s box past the end", typ)
			}
			size, body = int(large), off+16
		}
		if size < body-off || off+size > end {
			return nil, fmt.Errorf("clip: %s box of %d bytes at %d doesn't fit", typ, size, off)
		}
		out = append(out, mp4Box{typ: typ, off: off, end: off + size, body: body})
		off += size
	}
	return out, nil
}

// rd reads big-endian fields from a box payload, remembering the first
// overrun.
type rd struct {
	b   []byte
	p   int
	bad bool
}

func (r *rd) need(n int) bool {
	if r.p+n > len(r.b) {
		r.bad = true
		return false
	}
	return true
}
func (r *rd) u8() uint8 {
	if !r.need(1) {
		return 0
	}
	r.p++
	return r.b[r.p-1]
}
func (r *rd) u16() uint16 {
	if !r.need(2) {
		return 0
	}
	r.p += 2
	return binary.BigEndian.Uint16(r.b[r.p-2:])
}
func (r *rd) u32() uint32 {
	if !r.need(4) {
		return 0
	}
	r.p += 4
	return binary.BigEndian.Uint32(r.b[r.p-4:])
}
func (r *rd) u64() uint64 {
	if !r.need(8) {
		return 0
	}
	r.p += 8
	return binary.BigEndian.Uint64(r.b[r.p-8:])
}
func (r *rd) str4() string {
	if !r.need(4) {
		return ""
	}
	r.p += 4
	return string(r.b[r.p-4 : r.p])
}
func (r *rd) skip(n int) {
	if r.need(n) {
		r.p += n
	}
}

// full reads a full box's version and flags.
func (r *rd) full() (uint8, uint32) {
	v := r.u32()
	return uint8(v >> 24), v & 0xffffff
}

// Probe reads an audio-only MP4: ftyp, the first audio track's sample
// entry, the movie's durations and every fragment's samples (a fragmented
// file, as MediaRecorder writes; samples in a non-fragmented file's stbl
// aren't read).
func Probe(b []byte) (*Info, error) {
	top, err := boxes(b, 0, len(b))
	if err != nil {
		return nil, err
	}
	info := &Info{}
	for _, bx := range top {
		info.Top = append(info.Top, bx.typ)
		r := &rd{b: b[bx.body:bx.end]}
		switch bx.typ {
		case "ftyp":
			info.MajorBrand, info.MinorVersion = r.str4(), r.u32()
			for r.p+4 <= len(r.b) {
				info.Brands = append(info.Brands, r.str4())
			}
		case "moov":
			if err := info.moov(b, bx); err != nil {
				return nil, err
			}
		case "moof":
			f, err := info.moof(b, bx)
			if err != nil {
				return nil, err
			}
			info.Fragments = append(info.Fragments, f)
		}
		if r.bad {
			return nil, fmt.Errorf("clip: short %s", bx.typ)
		}
	}
	if info.Codec == "" {
		return nil, errors.New("clip: no audio sample entry")
	}
	return info, nil
}

func (info *Info) moov(b []byte, moov mp4Box) error {
	var walk func(p mp4Box) error
	walk = func(p mp4Box) error {
		kids, err := boxes(b, p.body, p.end)
		if err != nil {
			return err
		}
		for _, k := range kids {
			r := &rd{b: b[k.body:k.end]}
			switch k.typ {
			case "trak", "mdia", "minf", "stbl", "dinf", "mvex":
				if err := walk(k); err != nil {
					return err
				}
			case "mvhd":
				v, _ := r.full()
				if v == 1 {
					r.skip(16)
					info.MovieTimescale = int(r.u32())
					info.MovieDuration = r.u64()
				} else {
					r.skip(8)
					info.MovieTimescale = int(r.u32())
					info.MovieDuration = uint64(r.u32())
				}
				if len(r.b) >= 4 {
					info.NextTrackID = binary.BigEndian.Uint32(r.b[len(r.b)-4:])
				}
			case "tkhd":
				v, _ := r.full()
				if v == 1 {
					r.skip(16 + 8)
					info.TrackDuration = r.u64()
				} else {
					r.skip(8 + 8)
					info.TrackDuration = uint64(r.u32())
				}
			case "mdhd":
				v, _ := r.full()
				if v == 1 {
					r.skip(16)
					info.Timescale = int(r.u32())
					info.MediaDuration = r.u64()
				} else {
					r.skip(8)
					info.Timescale = int(r.u32())
					info.MediaDuration = uint64(r.u32())
				}
			case "hdlr":
				r.full()
				r.skip(4)
				info.Handler = r.str4()
			case "stsd":
				r.full()
				r.u32() // entry_count
				if err := info.sampleEntry(b, k.body+8, k.end); err != nil {
					return err
				}
			}
			if r.bad {
				return fmt.Errorf("clip: short %s", k.typ)
			}
		}
		return nil
	}
	return walk(moov)
}

// sampleEntry reads the first audio sample entry in b[off:end].
func (info *Info) sampleEntry(b []byte, off, end int) error {
	ents, err := boxes(b, off, end)
	if err != nil || len(ents) == 0 {
		return errors.New("clip: empty stsd")
	}
	e := ents[0]
	r := &rd{b: b[e.body:e.end]}
	r.skip(6)
	r.u16() // data_reference_index
	r.skip(8)
	info.Codec = e.typ
	info.Channels = int(r.u16())
	r.u16() // sample size
	r.skip(4)
	info.SampleRate = int(r.u32() >> 16)
	if r.bad {
		return errors.New("clip: short sample entry")
	}
	kids, err := boxes(b, e.body+r.p, e.end)
	if err != nil {
		return err
	}
	for _, k := range kids {
		if k.typ != "dOps" {
			continue
		}
		d := &rd{b: b[k.body:k.end]}
		d.u8() // version
		d.u8() // channels
		info.PreSkip = int(d.u16())
		info.InputRate = int(d.u32())
		if d.bad {
			return errors.New("clip: short dOps")
		}
	}
	return nil
}

func (info *Info) moof(b []byte, moof mp4Box) (Fragment, error) {
	var f Fragment
	kids, err := boxes(b, moof.body, moof.end)
	if err != nil {
		return f, err
	}
	for _, k := range kids {
		switch k.typ {
		case "mfhd":
			r := &rd{b: b[k.body:k.end]}
			r.full()
			f.Seq = r.u32()
		case "traf":
			if err := f.traf(b, k, moof.off); err != nil {
				return f, err
			}
		}
	}
	return f, nil
}

func (f *Fragment) traf(b []byte, traf mp4Box, moofOff int) error {
	kids, err := boxes(b, traf.body, traf.end)
	if err != nil {
		return err
	}
	var defSize, defDur uint32
	base := moofOff
	for _, k := range kids {
		r := &rd{b: b[k.body:k.end]}
		switch k.typ {
		case "tfhd":
			_, fl := r.full()
			r.u32() // track_id
			if fl&0x1 != 0 {
				base = int(r.u64())
			}
			if fl&0x2 != 0 {
				r.u32()
			}
			if fl&0x8 != 0 {
				defDur = r.u32()
			}
			if fl&0x10 != 0 {
				defSize = r.u32()
			}
		case "tfdt":
			v, _ := r.full()
			if v == 1 {
				f.BaseTime = r.u64()
			} else {
				f.BaseTime = uint64(r.u32())
			}
		case "trun":
			_, fl := r.full()
			n := int(r.u32())
			pos := base
			if fl&0x1 != 0 {
				pos = base + int(int32(r.u32()))
			}
			if fl&0x4 != 0 {
				r.u32()
			}
			if n > len(r.b) { // every sample takes at least a byte to describe
				return fmt.Errorf("clip: trun of %d samples", n)
			}
			for i := 0; i < n && !r.bad; i++ {
				dur, size := defDur, defSize
				if fl&0x100 != 0 {
					dur = r.u32()
				}
				if fl&0x200 != 0 {
					size = r.u32()
				}
				if fl&0x400 != 0 {
					r.u32()
				}
				if fl&0x800 != 0 {
					r.u32()
				}
				if pos < 0 || pos+int(size) > len(b) {
					return errors.New("clip: sample past the end of the file")
				}
				f.Durations = append(f.Durations, dur)
				f.Packets = append(f.Packets, b[pos:pos+int(size)])
				pos += int(size)
			}
		}
		if r.bad {
			return fmt.Errorf("clip: short %s", k.typ)
		}
	}
	return nil
}
