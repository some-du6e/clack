package clip

import (
	"encoding/binary"
	"time"
)

// Muxer writes Opus packets into an MP4 the way Chromium's MediaRecorder
// does for "audio/mp4" (media/muxers: Mp4Muxer, Mp4MuxerDelegate,
// Mp4MuxerDelegateFragment, Mp4MovieBoxWriter, Mp4FragmentBoxWriter), audio
// only:
//
//   - ftyp (isom, minor 512; isom iso6 iso2 avc1 mp41: BuildFileTypeBox adds
//     a video brand even without video), at the first packet;
//   - moov at the first flush: mvhd, one trak (tkhd, mdia: mdhd, hdlr
//     "SoundHandler", minf: smhd, dinf/dref/"url ", stbl with empty stsc,
//     stts, stsz, stco and stsd's Opus entry with dOps), mvex/trex;
//   - then a moof (mfhd, traf: tfhd, tfdt, trun) and mdat per fragment.
//
// Mp4Muxer flushes a fragment once a packet's timestamp is the timeslice
// past the last flush (MaybeForceFragmentFlush). That puts the delegate in
// live mode: the moov written then carries the durations up to that
// packet, mvhd's duration and next_track_id stay 0, and nothing else
// changes later. A recording too short for one is flushed once at stop,
// with the whole duration and next_track_id 2. A fragment also ends at
// 1000 packets (kAudioFragmentCount). Each fragment's last packet lasts
// 1000/(rate/1024) whole milliseconds (21 ms at 48 kHz: AddAudioLastTimestamp
// assumes AAC frames). Durations in mvhd, tkhd and mdhd are written in
// milliseconds whatever the box's timescale (the delegate's TODO), and
// dOps's pre-skip is 80 ms. The audio track has no edit list and a zero
// matrix; audio gets no mfra.
type Muxer struct {
	// Channels and Rate are the Opus stream's (AudioParameters).
	Channels, Rate int
	// Interval is Mp4Muxer's max_data_output_interval (the timeslice, at
	// least 100 ms).
	Interval time.Duration
	// Now stamps the moov's creation_time (base::Time::Now() in
	// BuildMovieBox); nil is time.Now.
	Now func() time.Time

	out        []byte
	ftyp, moov bool
	live       bool
	seq        uint32 // the next fragment's sequence_number_
	started    bool
	start      time.Duration // start_audio_time_
	last       time.Duration // last_audio_time_
	origin     time.Duration // Mp4Muxer's flush_origin_
	haveOrigin bool
	frags      []*mp4Fragment
}

type mp4Fragment struct {
	seq   uint32
	sizes []uint32
	times []time.Duration // one per packet, plus the end once finalized
	data  []byte
}

// Chromium's constants (mp4_muxer_delegate.cc, .h; muxer.h).
const (
	audioSamplesPerFrame  = 1024 // kAudioSamplesPerFrame
	audioFragmentCount    = 1000 // kAudioFragmentCount
	minForcedOutput       = 100 * time.Millisecond
	opusSkipDuration      = 80 * time.Millisecond // kOpusSkipDuration
	undefinedLanguageCode = 0x55C4                // "und"
	soundHandlerName      = "SoundHandler"
)

// mp4Epoch is kMP4Epoch, 1904-01-01 UTC.
var mp4Epoch = time.Date(1904, 1, 1, 0, 0, 0, 0, time.UTC)

// NewMuxer makes the muxer for a 48 kHz clip with channels channels and
// MediaRecorder's timeslice.
func NewMuxer(channels int) *Muxer {
	return &Muxer{Channels: channels, Rate: Rate, Interval: Timeslice}
}

func (m *Muxer) interval() time.Duration { return max(m.Interval, minForcedOutput) }

// lastSampleDuration is AddAudioLastTimestamp's duration.
func (m *Muxer) lastSampleDuration() time.Duration {
	return time.Duration(1000/(m.Rate/audioSamplesPerFrame)) * time.Millisecond
}

// Add muxes one packet whose first sample was captured at ts (from the
// recording's start).
func (m *Muxer) Add(pkt []byte, ts time.Duration) {
	if m.seq == 0 {
		m.seq = 1 // sequence_number_ starts at 1
	}
	// Mp4MuxerDelegate::AddAudioFrame
	if !m.started {
		m.started = true
		m.start = ts
	}
	m.last = ts
	// AddDataToAudioFragment: CreateFragmentIfNeeded, then the packet
	if n := len(m.frags); n == 0 || len(m.frags[n-1].sizes) >= audioFragmentCount {
		m.frags = append(m.frags, &mp4Fragment{seq: m.seq})
		m.seq++
	}
	f := m.frags[len(m.frags)-1]
	f.sizes = append(f.sizes, uint32(len(pkt)))
	f.times = append(f.times, ts)
	f.data = append(f.data, pkt...)
	m.writeFtyp()
	// Mp4Muxer::PutFrame: no video, so MaybeForceFragmentFlush
	if !m.haveOrigin {
		m.origin, m.haveOrigin = ts, true
	} else if ts-m.origin >= m.interval() {
		m.live = true // FlushFragment
		m.flush()
		m.origin = ts
	}
}

// Finish flushes what's left (MediaRecorder's stop calls Mp4Muxer::Flush)
// and returns the file; nil if no packet came.
func (m *Muxer) Finish() []byte {
	m.flush()
	return m.out
}

// Live reports whether a timeslice flush happened (the delegate's
// live_mode_).
func (m *Muxer) Live() bool { return m.live }

func (m *Muxer) flush() {
	if !m.started {
		return
	}
	m.writeFtyp()
	m.writeMoov()
	for _, f := range m.frags { // MaybeFlushMoofAndMfraBoxes
		if len(f.times) == len(f.sizes) && len(f.times) > 0 {
			f.times = append(f.times, f.times[len(f.times)-1]+m.lastSampleDuration())
		}
	}
	for _, f := range m.frags {
		m.writeFragment(f)
	}
	m.frags = nil
}

func (m *Muxer) writeFtyp() {
	if m.ftyp {
		return
	}
	m.ftyp = true
	var w boxWriter
	w.start("ftyp")
	w.fourcc("isom")
	w.u32(512)
	for _, b := range []string{"isom", "iso6", "iso2", "avc1", "mp41"} {
		w.fourcc(b)
	}
	w.end()
	m.out = append(m.out, w.b...)
}

var identityMatrix = [9]uint32{0x00010000, 0, 0, 0, 0x00010000, 0, 0, 0, 0x40000000}

func (m *Muxer) writeMoov() {
	if m.moov {
		return
	}
	m.moov = true
	now := time.Now
	if m.Now != nil {
		now = m.Now
	}
	created := isoTime(now())
	trackMS := uint64((m.last + m.lastSampleDuration() - m.start).Milliseconds())
	var movieMS uint64
	var nextTrack uint32
	if !m.live {
		movieMS, nextTrack = trackMS, 2
	}
	var w boxWriter
	w.start("moov")

	w.startFull("mvhd", 0, 1)
	w.u64(created)
	w.u64(created)
	w.u32(1000) // kMillisecondsTimeScale
	w.u64(movieMS)
	w.u32(0x00010000) // rate
	w.u16(0x0100)     // volume
	w.u16(0)
	w.u32(0)
	w.u32(0)
	for _, v := range identityMatrix {
		w.u32(v)
	}
	for i := 0; i < 6; i++ {
		w.u32(0) // pre_defined
	}
	w.u32(nextTrack)
	w.end()

	w.start("trak")
	w.startFull("tkhd", 0x3, 1) // kTrackEnabled | kTrackInMovie
	w.u64(created)
	w.u64(created)
	w.u32(1) // track_id
	w.u32(0)
	w.u64(trackMS)
	w.u32(0)
	w.u32(0)
	w.u16(0)      // layer
	w.u16(0)      // alternate_group
	w.u16(0x0100) // volume: audio
	w.u16(0)
	for i := 0; i < 9; i++ {
		w.u32(0) // an audio track's matrix is left zero
	}
	w.u32(0) // natural_size width (WriteLowHigh)
	w.u32(0) // height
	w.end()

	w.start("mdia")
	w.startFull("mdhd", 0, 1)
	w.u64(created)
	w.u64(created)
	w.u32(uint32(m.Rate))
	w.u64(trackMS) // milliseconds, as the delegate writes it
	w.u16(undefinedLanguageCode)
	w.u16(0)
	w.end()
	w.startFull("hdlr", 0, 0)
	w.u32(0)
	w.fourcc("soun")
	w.u32(0)
	w.u32(0)
	w.u32(0)
	w.str(soundHandlerName)
	w.end()
	w.start("minf")
	w.startFull("smhd", 0, 0)
	w.u16(0) // balance
	w.u16(0)
	w.end()
	w.start("dinf")
	w.startFull("dref", 0, 0)
	w.u32(1)
	w.startFull("url ", 1, 0)
	w.str("") // an empty location (one NUL)
	w.end()
	w.end()
	w.end()
	w.start("stbl")
	w.startFull("stsc", 0, 0)
	w.u32(0)
	w.end()
	w.startFull("stts", 0, 0)
	w.u32(0)
	w.end()
	w.startFull("stsz", 0, 0)
	w.u32(0)
	w.u32(0)
	w.end()
	w.startFull("stco", 0, 0)
	w.u32(0)
	w.end()
	w.startFull("stsd", 0, 0)
	w.u32(1)
	w.start("Opus")
	for i := 0; i < 6; i++ {
		w.u8(0)
	}
	w.u16(1) // data_reference_index
	w.u32(0)
	w.u32(0)
	w.u16(uint16(m.Channels))
	w.u16(16) // sample size
	w.u16(0)
	w.u16(0)
	w.u16(uint16(m.Rate & 0xFFFF)) // WriteLowHigh: 16.16
	w.u16(uint16(m.Rate >> 16))
	w.start("dOps")
	w.u8(0) // version
	w.u8(uint8(m.Channels))
	w.u16(uint16(timeToFrames(opusSkipDuration, m.Rate)))
	w.u32(uint32(m.Rate))
	w.u16(0) // output gain
	w.u8(0)  // channel mapping family
	w.end()
	w.end() // Opus
	w.end() // stsd
	w.end() // stbl
	w.end() // minf
	w.end() // mdia
	w.end() // trak

	w.start("mvex")
	w.startFull("trex", 0, 0)
	w.u32(1) // track_id
	w.u32(1) // default_sample_description_index
	w.u32(0) // default_sample_duration (ms)
	w.u32(0) // default_sample_size
	w.u32(0) // default_sample_flags
	w.end()
	w.end()

	w.end() // moov
	m.out = append(m.out, w.b...)
}

// writeFragment writes f's moof and mdat; f.times has its end entry.
func (m *Muxer) writeFragment(f *mp4Fragment) {
	var w boxWriter
	w.start("moof")
	w.startFull("mfhd", 0, 0)
	w.u32(f.seq)
	w.end()
	w.start("traf")
	w.startFull("tfhd", 0x020020, 0) // kDefaultBaseIsMoof | kDefaultSampleFlagsPresent
	w.u32(1)                         // track_id
	w.u32(0x02000000)                // kSampleFlagDependsNo
	w.end()
	w.startFull("tfdt", 0, 1)
	w.u64(toTimescale(f.times[0]-m.start, m.Rate))
	w.end()
	w.startFull("trun", 0x000301, 1) // data offset, sample durations, sizes
	w.u32(uint32(len(f.sizes)))
	off := len(w.b)
	w.u32(0) // data_offset, set below
	for i, sz := range f.sizes {
		w.u32(uint32(toTimescale(f.times[i+1]-f.times[i], m.Rate)))
		w.u32(sz)
	}
	w.end() // trun
	w.end() // traf
	w.end() // moof
	w.start("mdat")
	binary.BigEndian.PutUint32(w.b[off:], uint32(len(w.b))) // FlushCurrentOffset: from the moof's start
	w.b = append(w.b, f.data...)
	w.end()
	m.out = append(m.out, w.b...)
}

// toTimescale is ConvertToTimescale: TimeDelta's InSecondsF (whole
// microseconds) times the timescale, truncated.
func toTimescale(d time.Duration, timescale int) uint64 {
	v := float64(d.Microseconds()) / 1e6 * float64(timescale)
	if v <= 0 {
		return 0
	}
	return uint64(v)
}

// timeToFrames is AudioTimestampHelper::TimeToFrames (rounded).
func timeToFrames(d time.Duration, rate int) int64 {
	v := float64(d.Microseconds()) / 1e6 * float64(rate)
	return int64(v + 0.5)
}

// isoTime is WriteIsoTime: whole seconds since 1904.
func isoTime(t time.Time) uint64 {
	s := t.Sub(mp4Epoch).Seconds()
	if s <= 0 {
		return 0
	}
	return uint64(s)
}

// boxWriter is BoxByteStream: big-endian fields, boxes sized at EndBox.
type boxWriter struct {
	b     []byte
	sizes []int
}

func (w *boxWriter) u8(v uint8)      { w.b = append(w.b, v) }
func (w *boxWriter) u16(v uint16)    { w.b = binary.BigEndian.AppendUint16(w.b, v) }
func (w *boxWriter) u32(v uint32)    { w.b = binary.BigEndian.AppendUint32(w.b, v) }
func (w *boxWriter) u64(v uint64)    { w.b = binary.BigEndian.AppendUint64(w.b, v) }
func (w *boxWriter) fourcc(c string) { w.b = append(w.b, c[:4]...) }
func (w *boxWriter) start(c string)  { w.sizes = append(w.sizes, len(w.b)); w.u32(0); w.fourcc(c) }
func (w *boxWriter) startFull(c string, flags uint32, version uint8) {
	w.start(c)
	w.u32(uint32(version)<<24 | flags&0xffffff)
}

// str is WriteString: the bytes and a NUL (one NUL for "").
func (w *boxWriter) str(s string) {
	w.b = append(w.b, s...)
	if s == "" || s[len(s)-1] != 0 {
		w.u8(0)
	}
}

func (w *boxWriter) end() {
	s := w.sizes[len(w.sizes)-1]
	w.sizes = w.sizes[:len(w.sizes)-1]
	binary.BigEndian.PutUint32(w.b[s:], uint32(len(w.b)-s))
}
