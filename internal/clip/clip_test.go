package clip

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/huddle"
)

// box finds the first box of type typ anywhere in b (a scan: the fixtures
// here hold each type once) and returns it whole, header included.
func box(t *testing.T, b []byte, typ string) []byte {
	t.Helper()
	i := bytes.Index(b, []byte(typ))
	if i < 4 {
		t.Fatalf("no %q box", typ)
	}
	n := int(binary.BigEndian.Uint32(b[i-4:]))
	if i-4+n > len(b) {
		t.Fatalf("%q box of %d bytes runs past the file", typ, n)
	}
	return b[i-4 : i-4+n]
}

func hexOf(b []byte) string {
	var s strings.Builder
	for i, c := range b {
		if i > 0 {
			s.WriteByte(' ')
		}
		s.WriteString(strconv.FormatUint(uint64(c)>>4, 16))
		s.WriteString(strconv.FormatUint(uint64(c)&15, 16))
	}
	return s.String()
}

// A recording shorter than the timeslice is flushed once, at stop, the way
// Chromium's Mp4MuxerDelegate writes it: ftyp, a moov with the whole
// duration (in milliseconds, in mvhd, tkhd and mdhd alike) and
// next_track_id 2, one moof + mdat, every packet 60 ms but the last, which
// AddAudioLastTimestamp makes 21 ms. Box for box as its writers emit them.
func TestMuxerMatchesChromiumLayout(t *testing.T) {
	created := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	m := NewMuxer(1)
	m.Now = func() time.Time { return created }
	var pkts [][]byte
	for i := 0; i < 10; i++ {
		p := bytes.Repeat([]byte{byte(i + 1)}, 100+i)
		pkts = append(pkts, p)
		m.Add(p, time.Duration(i)*60*time.Millisecond)
	}
	if m.Live() {
		t.Fatal("a 0.54 s recording went live")
	}
	b := m.Finish()
	info, err := Probe(b)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(info.Top, " "); got != "ftyp moov moof mdat" {
		t.Errorf("boxes %q", got)
	}
	if info.MajorBrand != "isom" || info.MinorVersion != 512 || strings.Join(info.Brands, " ") != "isom iso6 iso2 avc1 mp41" {
		t.Errorf("ftyp %s %d %v", info.MajorBrand, info.MinorVersion, info.Brands)
	}
	if info.Codec != "Opus" || info.Channels != 1 || info.SampleRate != 48000 || info.PreSkip != 3840 || info.InputRate != 48000 {
		t.Errorf("sample entry %s ch %d rate %d dOps pre-skip %d input rate %d", info.Codec, info.Channels, info.SampleRate, info.PreSkip, info.InputRate)
	}
	if info.Handler != "soun" || info.Timescale != 48000 || info.MovieTimescale != 1000 {
		t.Errorf("handler %q timescale %d movie timescale %d", info.Handler, info.Timescale, info.MovieTimescale)
	}
	const wantMS = 9*60 + 21
	if info.MovieDuration != wantMS || info.TrackDuration != wantMS || info.MediaDuration != wantMS || info.NextTrackID != 2 {
		t.Errorf("durations mvhd %d tkhd %d mdhd %d (want %d ms in each), next_track_id %d", info.MovieDuration, info.TrackDuration,
			info.MediaDuration, wantMS, info.NextTrackID)
	}
	if len(info.Fragments) != 1 {
		t.Fatalf("%d fragments", len(info.Fragments))
	}
	f := info.Fragments[0]
	want := append(slices.Repeat([]uint32{2880}, 9), 1008)
	if f.Seq != 1 || f.BaseTime != 0 || !slices.Equal(f.Durations, want) {
		t.Errorf("fragment seq %d base %d durations %v", f.Seq, f.BaseTime, f.Durations)
	}
	if len(f.Packets) != len(pkts) {
		t.Fatalf("%d packets", len(f.Packets))
	}
	for i := range pkts {
		if !bytes.Equal(f.Packets[i], pkts[i]) {
			t.Errorf("packet %d differs", i)
		}
	}
	if s := info.Samples(); s != 9*2880+1008 {
		t.Errorf("timeline %d samples", s)
	}

	// the writers' bytes
	for _, c := range []struct{ typ, hex string }{
		{"ftyp", "00 00 00 24 66 74 79 70 69 73 6f 6d 00 00 02 00 69 73 6f 6d 69 73 6f 36 69 73 6f 32 61 76 63 31 6d 70 34 31"},
		// Mp4MovieDataUrlEntryBoxWriter: flags 1 and WriteString("") (one NUL)
		{"url ", "00 00 00 0d 75 72 6c 20 00 00 00 01 00"},
		// version 0, 1 channel, pre-skip 3840 (80 ms), 48000, gain 0, family 0
		{"dOps", "00 00 00 13 64 4f 70 73 00 01 0f 00 00 00 bb 80 00 00 00"},
		{"trex", "00 00 00 20 74 72 65 78 00 00 00 00 00 00 00 01 00 00 00 01 00 00 00 00 00 00 00 00 00 00 00 00"},
		// kDefaultBaseIsMoof | kDefaultSampleFlagsPresent, track 1, kSampleFlagDependsNo
		{"tfhd", "00 00 00 14 74 66 68 64 00 02 00 20 00 00 00 01 02 00 00 00"},
		{"tfdt", "00 00 00 14 74 66 64 74 01 00 00 00 00 00 00 00 00 00 00 00"},
		{"mfhd", "00 00 00 10 6d 66 68 64 00 00 00 00 00 00 00 01"},
		{"smhd", "00 00 00 10 73 6d 68 64 00 00 00 00 00 00 00 00"},
		{"stsz", "00 00 00 14 73 74 73 7a 00 00 00 00 00 00 00 00 00 00 00 00"},
	} {
		if got := hexOf(box(t, b, c.typ)); got != c.hex {
			t.Errorf("%s:\n got %s\nwant %s", c.typ, got, c.hex)
		}
	}
	// the Opus sample entry: 6 reserved, data_reference_index 1, 8
	// reserved, channels 1, sample size 16, 4 reserved, 48000 as 16.16
	if got := hexOf(box(t, b, "Opus")[8:36]); got != "00 00 00 00 00 00 00 01 00 00 00 00 00 00 00 00 00 01 00 10 00 00 00 00 bb 80 00 00" {
		t.Errorf("Opus entry %s", got)
	}
	// hdlr: pre_defined, soun, 3 reserved, "SoundHandler\0"
	if h := box(t, b, "hdlr"); string(h[32:]) != "SoundHandler\x00" || string(h[16:20]) != "soun" {
		t.Errorf("hdlr %q", h)
	}
	// stbl's children in the delegate's order, every table empty
	stbl := box(t, b, "stbl")
	var kids []string
	for off := 8; off+8 <= len(stbl); off += int(binary.BigEndian.Uint32(stbl[off:])) {
		kids = append(kids, string(stbl[off+4:off+8]))
	}
	if strings.Join(kids, " ") != "stsc stts stsz stco stsd" {
		t.Errorf("stbl holds %v", kids)
	}
	// mvhd v1: creation and modification times since 1904, 1000, duration
	mvhd := box(t, b, "mvhd")
	secs := uint64(created.Sub(time.Date(1904, 1, 1, 0, 0, 0, 0, time.UTC)) / time.Second)
	if mvhd[8] != 1 || binary.BigEndian.Uint64(mvhd[12:]) != secs || binary.BigEndian.Uint64(mvhd[20:]) != secs {
		t.Errorf("mvhd %s", hexOf(mvhd[:36]))
	}
	// tkhd: flags 3, version 1; an audio track's matrix left zero, volume 1
	tkhd := box(t, b, "tkhd")
	if len(tkhd) != 104 || hexOf(tkhd[8:12]) != "01 00 00 03" || binary.BigEndian.Uint32(tkhd[28:]) != 1 ||
		binary.BigEndian.Uint64(tkhd[36:]) != wantMS || binary.BigEndian.Uint16(tkhd[56:]) != 0x0100 {
		t.Errorf("tkhd %s", hexOf(tkhd))
	}
	for i := 60; i < 104; i++ { // the matrix and the natural size
		if tkhd[i] != 0 {
			t.Fatalf("tkhd matrix and size %s", hexOf(tkhd[60:]))
		}
	}
	// trun v1 flags 0x301: the data offset points at the mdat's first byte
	moof := box(t, b, "moof")
	trun := box(t, b, "trun")
	if hexOf(trun[8:12]) != "01 00 03 01" || int(binary.BigEndian.Uint32(trun[16:])) != len(moof)+8 {
		t.Errorf("trun %s (moof %d bytes)", hexOf(trun[:20]), len(moof))
	}
}

// Past the timeslice Mp4Muxer flushes a fragment after the first packet
// 5 s past the last flush (MaybeForceFragmentFlush): the delegate goes live,
// so the moov written then carries the durations so far, mvhd's duration
// and next_track_id stay 0, and later fragments follow on with their own
// tfdt. Each fragment's last packet is 21 ms.
func TestMuxerTimesliceGoesLive(t *testing.T) {
	m := NewMuxer(1)
	for i := 0; i < 100; i++ { // 6 s
		m.Add([]byte{byte(i), 0xfc}, time.Duration(i)*60*time.Millisecond)
	}
	if !m.Live() {
		t.Fatal("no timeslice flush in 6 s")
	}
	info, err := Probe(m.Finish())
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(info.Top, " "); got != "ftyp moov moof mdat moof mdat" {
		t.Errorf("boxes %q", got)
	}
	// the flush came after the packet at 5.04 s (84): 5.04 s + 21 ms
	if info.MovieDuration != 0 || info.NextTrackID != 0 || info.TrackDuration != 5061 || info.MediaDuration != 5061 {
		t.Errorf("live moov: mvhd %d next %d tkhd %d mdhd %d", info.MovieDuration, info.NextTrackID, info.TrackDuration, info.MediaDuration)
	}
	if len(info.Fragments) != 2 {
		t.Fatalf("%d fragments", len(info.Fragments))
	}
	f0, f1 := info.Fragments[0], info.Fragments[1]
	// tfdt is ConvertToTimescale's truncated InSecondsF() × 48000: 5.1 s
	// comes out one sample short, as Chromium's does
	base := toTimescale(85*60*time.Millisecond, Rate)
	if base != 244799 {
		t.Errorf("ConvertToTimescale(5.1 s) = %d", base)
	}
	if len(f0.Packets) != 85 || len(f1.Packets) != 15 || f0.Seq != 1 || f1.Seq != 2 || f1.BaseTime != base {
		t.Errorf("fragments %d+%d packets, seq %d %d, second at %d", len(f0.Packets), len(f1.Packets), f0.Seq, f1.Seq, f1.BaseTime)
	}
	for _, f := range info.Fragments {
		if last := f.Durations[len(f.Durations)-1]; last != 1008 {
			t.Errorf("fragment %d ends with a %d-sample packet", f.Seq, last)
		}
	}
	if info.Samples() != base+14*2880+1008 {
		t.Errorf("timeline %d", info.Samples())
	}

	// a fragment also ends at 1000 packets (kAudioFragmentCount); the final
	// flush shortens both fragments' last packets
	m = NewMuxer(1)
	m.Interval = time.Hour
	for i := 0; i < 1001; i++ {
		m.Add([]byte{1}, time.Duration(i)*60*time.Millisecond)
	}
	info, err = Probe(m.Finish())
	if err != nil {
		t.Fatal(err)
	}
	if m.Live() || len(info.Fragments) != 2 || len(info.Fragments[0].Packets) != 1000 || info.Fragments[1].BaseTime != 1000*2880 ||
		info.Fragments[0].Durations[999] != 1008 || info.NextTrackID != 2 {
		t.Errorf("1001 packets: %d fragments, live %v", len(info.Fragments), m.Live())
	}
}

// sine is amp×sin at hz for d, 48 kHz mono.
func sine(hz, amp float64, d time.Duration) []int16 {
	n := int(d * Rate / time.Second)
	out := make([]int16, n)
	for i := range out {
		out[i] = int16(amp * 32767 * math.Sin(2*math.Pi*hz*float64(i)/Rate))
	}
	return out
}

// goertzel is the signal's power at hz.
func goertzel(x []int16, hz float64) float64 {
	k := 2 * math.Cos(2*math.Pi*hz/Rate)
	var s1, s2 float64
	for _, v := range x {
		s := float64(v) + k*s1 - s2
		s2, s1 = s1, s
	}
	return s1*s1 + s2*s2 - k*s1*s2
}

func rms(x []int16) float64 {
	s := 0.0
	for _, v := range x {
		s += float64(v) * float64(v)
	}
	return math.Sqrt(s / float64(max(1, len(x))))
}

// encodeSine runs d of a 440 Hz tone through the real encoder in the
// microphone's 10 ms chunks.
func encodeSine(t *testing.T, d time.Duration) ([]byte, []int16) {
	t.Helper()
	enc, err := NewEncoder(1, BitRate)
	if err != nil {
		t.Fatal(err)
	}
	pcm := sine(440, 0.25, d)
	for off := 0; off < len(pcm); off += 480 {
		if err := enc.Write(pcm[off:min(len(pcm), off+480)]); err != nil {
			t.Fatal(err)
		}
	}
	b, err := enc.Finish()
	if err != nil {
		t.Fatal(err)
	}
	return b, pcm
}

// A synthetic tone through the real encoder: Opus in MP4 with Chromium's
// AudioTrackOpusEncoder settings, 60 ms packets (what's left under 60 ms
// at stop isn't encoded), which libopus decodes back to the tone.
func TestEncodeSineIsOpusMP4(t *testing.T) {
	b, in := encodeSine(t, 3200*time.Millisecond)
	info, err := Probe(b)
	if err != nil {
		t.Fatal(err)
	}
	if info.Codec != "Opus" || info.Channels != 1 || info.SampleRate != 48000 || info.PreSkip != 3840 || info.Timescale != 48000 {
		t.Fatalf("codec %s ch %d rate %d pre-skip %d", info.Codec, info.Channels, info.SampleRate, info.PreSkip)
	}
	pkts := info.Packets()
	if len(pkts) != 53 { // floor(3.2 / 0.06)
		t.Fatalf("%d packets, want 53", len(pkts))
	}
	if s := info.Samples(); s != 52*2880+1008 {
		t.Errorf("timeline %d samples", s)
	}
	if d := info.Duration(); d < 3100*time.Millisecond || d > 3200*time.Millisecond {
		t.Errorf("duration %v", d)
	}
	total := 0
	for _, p := range pkts {
		total += len(p)
	}
	if kbps := float64(total) * 8 / 3.18 / 1000; kbps < 24 || kbps > 200 {
		t.Errorf("%.0f kbps for a 128 kbps VBR stream", kbps)
	}
	dec, err := huddle.NewOpusDecoder(1)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()
	var out []int16
	for i, p := range pkts {
		pcm, err := dec.Decode(p)
		if err != nil {
			t.Fatalf("packet %d: %v", i, err)
		}
		if len(pcm) != FrameSamples {
			t.Fatalf("packet %d decodes to %d samples, want 2880 (60 ms)", i, len(pcm))
		}
		out = append(out, pcm...)
	}
	body := out[info.PreSkip:] // what a player plays
	if r, want := rms(body), rms(in); r < 0.6*want || r > 1.4*want {
		t.Errorf("decoded RMS %.0f, the tone's %.0f", r, want)
	}
	tone, off := goertzel(body, 440), goertzel(body, 1000)
	if tone < 100*off {
		t.Errorf("440 Hz power %.3g vs 1 kHz %.3g: the tone didn't survive", tone, off)
	}
}

// ffprobe and ffmpeg read the clip as an independent MP4 demuxer and Opus
// decoder would (CLACK_FFPROBE=1: huddle.yml installs ffmpeg), in both of
// its shapes: a recording under the 5 s timeslice, flushed once at stop,
// and one past it, which most clips are: live mode, the moov written at
// the first timeslice flush (durations up to it, mvhd's 0), then a
// fragment every 5 s and the rest at stop. Every packet the encoder made
// is read, and ffmpeg decodes all of them without an error. The files
// are a synthetic tone, written to the test's temp dir.
func TestFFProbeReadsTheClip(t *testing.T) {
	if os.Getenv("CLACK_FFPROBE") == "" {
		t.Skip("CLACK_FFPROBE not set")
	}
	for _, c := range []struct {
		name  string
		d     time.Duration
		frags int
	}{
		{"one flush at stop", 3200 * time.Millisecond, 1},
		{"fragments every 5 s", 12600 * time.Millisecond, 3},
	} {
		t.Run(c.name, func(t *testing.T) {
			b, pcm := encodeSine(t, c.d)
			packets := len(pcm) / FrameSamples // what's left under 60 ms isn't encoded
			info, err := Probe(b)
			if err != nil {
				t.Fatal(err)
			}
			if len(info.Fragments) != c.frags {
				t.Fatalf("%d fragments, want %d (top %v)", len(info.Fragments), c.frags, info.Top)
			}
			path := filepath.Join(t.TempDir(), FileName)
			if err := os.WriteFile(path, b, 0o600); err != nil {
				t.Fatal(err)
			}
			out, err := exec.Command("ffprobe", "-v", "error", "-count_packets", "-show_streams", "-show_format", "-of", "json", path).Output()
			if err != nil {
				t.Fatalf("ffprobe: %v", err)
			}
			var r struct {
				Streams []struct {
					CodecName     string `json:"codec_name"`
					CodecType     string `json:"codec_type"`
					SampleRate    string `json:"sample_rate"`
					Channels      int    `json:"channels"`
					NbReadPackets string `json:"nb_read_packets"`
				} `json:"streams"`
				Format struct {
					FormatName string `json:"format_name"`
					Duration   string `json:"duration"`
				} `json:"format"`
			}
			if err := json.Unmarshal(out, &r); err != nil {
				t.Fatal(err)
			}
			t.Logf("ffprobe: %s", out)
			if len(r.Streams) != 1 || r.Streams[0].CodecName != "opus" || r.Streams[0].CodecType != "audio" ||
				r.Streams[0].SampleRate != "48000" || r.Streams[0].Channels != 1 {
				t.Fatalf("streams %+v", r.Streams)
			}
			if got := r.Streams[0].NbReadPackets; got != strconv.Itoa(packets) {
				t.Errorf("ffprobe read %s packets, the encoder made %d", got, packets)
			}
			if !strings.Contains(r.Format.FormatName, "mp4") {
				t.Errorf("format %q", r.Format.FormatName)
			}
			want := float64(packets) * 0.06
			// the last packet's end, from the fragments' timestamps (tfdt,
			// trun): a live-mode moov's mvhd says 0, so the format's
			// duration is FFmpeg's estimate at open, checked only for the
			// single flush
			pk, err := exec.Command("ffprobe", "-v", "error", "-select_streams", "a:0", "-show_entries", "packet=pts_time,duration_time", "-of", "csv=p=0", path).Output()
			if err != nil {
				t.Fatalf("ffprobe packets: %v", err)
			}
			lines := strings.Fields(strings.TrimSpace(string(pk)))
			if len(lines) != packets {
				t.Errorf("%d packet entries, want %d", len(lines), packets)
			}
			if n := len(lines); n > 0 {
				f := strings.Split(lines[n-1], ",")
				pts, _ := strconv.ParseFloat(f[0], 64)
				dur := 0.0
				if len(f) > 1 {
					dur, _ = strconv.ParseFloat(f[1], 64)
				}
				if end := pts + dur; math.Abs(end-want) > 0.3 {
					t.Errorf("the last packet ends at %.3f s (%s), want about %.2f", end, lines[n-1], want)
				}
			}
			if c.frags == 1 {
				if d, _ := strconv.ParseFloat(r.Format.Duration, 64); math.Abs(d-want) > 0.3 {
					t.Errorf("duration %q, want about %.2f", r.Format.Duration, want)
				}
			}
			cmd := exec.Command("ffmpeg", "-v", "error", "-i", path, "-f", "null", "-")
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			if err := cmd.Run(); err != nil || stderr.Len() > 0 {
				t.Errorf("ffmpeg decode: %v %s", err, stderr.String())
			}
		})
	}
}

// The analyser is Chromium's RealtimeAnalyser at fftSize 32: a steady
// level c (32/32768) windowed by the Blackman window puts c·a0, c·a1/2 and
// c·a2/2 (.42, .25, .04) in bins 0-2 and nothing above; the first read's
// smoothing keeps .8 of them, and 20·log10 over -100..-30 dB maps them to
// 110, 94 and 36.
func TestAnalyserDCBins(t *testing.T) {
	w := NewWaveform()
	pcm := make([]int16, analyserFFT)
	for i := range pcm {
		pcm[i] = 32
	}
	w.Write(pcm)
	var bins [analyserBins]uint8
	w.byteFrequencyData(&bins)
	want := [analyserBins]uint8{110, 94, 36}
	if bins != want {
		t.Errorf("bins %v, want %v", bins, want)
	}
	// silence reads as nothing
	q := NewWaveform()
	q.Write(make([]int16, analyserFFT))
	q.byteFrequencyData(&bins)
	if bins != ([analyserBins]uint8{}) {
		t.Errorf("silence: %v", bins)
	}
}

// The strip starts as 24 empty dots (-1); every fifth 20 ms read adds the
// loudest of the five, capped at 1, and audio_wave_samples is the strip
// × 100.
func TestWaveformStrip(t *testing.T) {
	w := NewWaveform()
	for i, d := range w.Dots() {
		if d != -1 {
			t.Fatalf("dot %d starts at %v", i, d)
		}
	}
	loud := sine(1500, 0.9, 20*time.Millisecond)
	for i := 0; i < 4; i++ {
		w.Write(loud)
		if w.Read() {
			t.Fatalf("read %d added a dot", i+1)
		}
	}
	w.Write(loud)
	if !w.Read() {
		t.Fatal("the fifth read added no dot")
	}
	d := w.Dots()
	if d[23] <= 0.4 || d[23] > 1 || d[22] != -1 {
		t.Errorf("strip ends %v %v", d[22], d[23])
	}
	s := w.Samples()
	if s[0] != -100 || s[23] != d[23]*100 {
		t.Errorf("samples %v … %v", s[0], s[23])
	}
	// quiet now: the smoothing (k .2) carries the tone into the next dot,
	// the one after is nearly flat
	for i := 0; i < 10; i++ {
		w.Write(make([]int16, 960))
		w.Read()
	}
	if d := w.Dots(); d[21] <= 0.4 || d[23] >= 0.05 || d[23] < 0 {
		t.Errorf("after silence the strip ends %v %v %v", d[21], d[22], d[23])
	}
	if h := DotHeight(-1, 3); h != 3 {
		t.Errorf("an empty dot is %v px", h)
	}
	if h := DotHeight(1, 3); h != 34 {
		t.Errorf("a full dot is %v px", h)
	}
}

// AudioWaveform's resampling and the Scrubber's dot count (d0gJ, o/6F).
func TestResampleLikeAudioWaveform(t *testing.T) {
	if got := Resample([]float64{0, 1}, 3); !slices.Equal(got, []float64{0, 0.5, 1}) {
		t.Errorf("interpolated %v", got)
	}
	if got := Resample([]float64{1, 1, 0, 0}, 2); !slices.Equal(got, []float64{1, 0}) {
		t.Errorf("folded %v", got)
	}
	if got := Resample([]float64{3, 4}, 1); math.Abs(got[0]-math.Sqrt(12.5)) > 1e-9 {
		t.Errorf("folded to one %v", got)
	}
	for _, c := range []struct{ w, dots, px int }{{100, 17, 99}, {96, 16, 93}, {3, 1, 3}, {2, 0, 0}} {
		if n, px := ScrubberDots(c.w); n != c.dots || px != c.px {
			t.Errorf("ScrubberDots(%d) = %d, %d; want %d, %d", c.w, n, px, c.dots, c.px)
		}
	}
}

// failDevice is a microphone that won't open.
type failDevice struct{}

func (failDevice) Start(func([]int16), func([]int16)) error { return errors.New("no microphone") }
func (failDevice) Close()                                   {}

func waitState(t *testing.T, r *Recorder, want State) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for r.State() != want {
		if time.Now().After(deadline) {
			t.Fatalf("state %s, want %s", r.State(), want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// The recorder on a synthetic microphone (the huddle's tone device, through
// OpenClipMic's mic chain): microphonestart plays the start sound, 300 ms
// later recording starts and the timer runs; Stop makes Slack's File: the
// name, type and subtype, the MP4, the timer's duration and the strip's
// samples.
func TestRecorderOnAToneDevice(t *testing.T) {
	var plays, changes atomic.Int32
	r := Start(Options{AGC: true, Device: huddle.NewToneDevice(440), PlayStart: func() { plays.Add(1) }, OnChange: func() { changes.Add(1) }})
	waitState(t, r, Recording)
	if plays.Load() != 1 {
		t.Errorf("start sound played %d times", plays.Load())
	}
	time.Sleep(1300 * time.Millisecond)
	if e := r.Elapsed(); e < time.Second || e > 2*time.Second {
		t.Errorf("timer at %v after 1.3 s", e)
	}
	drawn := 0
	for _, d := range r.Dots() {
		if d >= 0 {
			drawn++
		}
	}
	if drawn < 8 {
		t.Errorf("%d dots drawn after 1.3 s (one per 100 ms)", drawn)
	}
	if r.Warning() {
		t.Error("warning with 5 minutes left")
	}
	r.Stop()
	if r.State() != Stopped {
		t.Fatalf("state %s after Stop", r.State())
	}
	f, ok := r.File()
	if !ok || f.Name != "audio_message.m4a" || f.Mime != "audio/mp4" || f.Subtype != "slack_audio" || len(f.WaveSamples) != 24 {
		t.Fatalf("file %q %q %q, %d samples", f.Name, f.Mime, f.Subtype, len(f.WaveSamples))
	}
	info, err := Probe(f.Data)
	if err != nil {
		t.Fatal(err)
	}
	if info.Codec != "Opus" || info.Channels != 1 {
		t.Errorf("recorded %s × %d", info.Codec, info.Channels)
	}
	if got := info.Duration().Seconds(); math.Abs(got-f.Duration) > 0.25 {
		t.Errorf("MP4 %.2f s, timer %.2f s", got, f.Duration)
	}
	if changes.Load() < 10 {
		t.Errorf("%d redraws asked for", changes.Load())
	}
	r.Cancel() // after the file: nothing changes
	if r.State() != Stopped {
		t.Errorf("Cancel after Stop: %s", r.State())
	}
}

// Cancel (the × or Escape) stops the microphone and keeps nothing; a
// microphone that fails to open is microphoneerror; past the time limit the
// recording stops by itself and attaches.
func TestRecorderCancelFailAndLimit(t *testing.T) {
	r := Start(Options{Device: huddle.NewToneDevice(440)})
	waitState(t, r, Recording)
	r.Cancel()
	if r.State() != Cancelled {
		t.Fatalf("state %s after Cancel", r.State())
	}
	if _, ok := r.File(); ok {
		t.Error("a cancelled recording has a file")
	}
	r.Stop() // nothing to stop
	if r.State() != Cancelled {
		t.Errorf("Stop after Cancel: %s", r.State())
	}

	r = Start(Options{Device: failDevice{}})
	waitState(t, r, Failed)
	if st, err := r.Mic(); st != huddle.MicFailed || err == nil {
		t.Errorf("mic %s %v", st, err)
	}

	r = Start(Options{Device: huddle.NewToneDevice(440), TimeLimit: 1500 * time.Millisecond})
	waitState(t, r, Recording)
	if !r.Warning() { // 1.5 s left is inside the last 10 s
		t.Error("no warning near the limit")
	}
	waitState(t, r, Stopped)
	f, _ := r.File()
	if f.Duration < 1.5 || f.Duration > 1.8 || len(f.Data) == 0 {
		t.Errorf("limit stop: %.2f s, %d bytes", f.Duration, len(f.Data))
	}
}

// slowCloseDevice is a tone microphone whose Close takes a while, as a
// sound card's can.
type slowCloseDevice struct {
	huddle.Device
	delay  time.Duration
	once   atomic.Bool
	closed chan struct{}
}

func (d *slowCloseDevice) Close() {
	time.Sleep(d.delay)
	d.Device.Close()
	if d.once.CompareAndSwap(false, true) {
		close(d.closed)
	}
}

// Cancel returns at once even when the microphone is slow to close (the
// popover's onClose calls it on the UI goroutine), and the microphone and
// the encoder still close after it.
func TestRecorderCancelDoesNotWaitForTheMicrophone(t *testing.T) {
	dev := &slowCloseDevice{Device: huddle.NewToneDevice(440), delay: 2 * time.Second, closed: make(chan struct{})}
	r := Start(Options{Device: dev})
	waitState(t, r, Recording)
	t0 := time.Now()
	r.Cancel()
	if d := time.Since(t0); d > 500*time.Millisecond {
		t.Errorf("Cancel took %v; the microphone's Close takes %v", d.Round(time.Millisecond), dev.delay)
	}
	if r.State() != Cancelled {
		t.Fatalf("state %s after Cancel", r.State())
	}
	select {
	case <-dev.closed:
	case <-time.After(10 * time.Second):
		t.Fatal("the microphone never closed")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		r.encMu.Lock()
		enc := r.enc
		r.encMu.Unlock()
		if enc == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the encoder was never freed")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// quietMic is a microphone that opens and never delivers a frame (one
// whose driver hands back nothing); hold, when set, keeps its Start from
// returning until it's closed. It counts its Closes.
type quietMic struct {
	hold   chan struct{}
	closes atomic.Int32
}

func (d *quietMic) Start(func([]int16), func([]int16)) error {
	if d.hold != nil {
		<-d.hold
	}
	return nil
}

func (d *quietMic) Close() { d.closes.Add(1) }

// noPanic runs f, failing the test with what panicked instead of ending
// the test binary.
func noPanic(t *testing.T, what string, f func()) {
	t.Helper()
	defer func() {
		if p := recover(); p != nil {
			t.Errorf("%s panicked: %v", what, p)
		}
	}()
	f()
}

// A stop before any audio reached the encoder (✓, Return or Space as the
// timer starts; a microphone that delivers nothing, even at the 5:00
// stop) still makes the File, as Slack's recordingend does: an empty
// audio_message.m4a with the timer's duration and the strip's samples,
// attached like any other. The popover's close after it (×, Escape, the
// composer going: Cancel) finds nothing left to close: it closed the
// ticker's channel a second time and panicked.
func TestRecorderStopBeforeAnyAudio(t *testing.T) {
	dev := &quietMic{}
	r := Start(Options{Device: dev})
	waitState(t, r, Recording)
	noPanic(t, "Stop", r.Stop)
	if r.State() != Stopped {
		t.Fatalf("state %s after a stop with nothing recorded, want stopped (Slack attaches the empty file)", r.State())
	}
	f, ok := r.File()
	if !ok || f.Name != FileName || f.Mime != Mime || f.Subtype != Subtype || len(f.Data) != 0 || len(f.WaveSamples) != WaveDots {
		t.Errorf("file %q %q %q, %d bytes, %d samples", f.Name, f.Mime, f.Subtype, len(f.Data), len(f.WaveSamples))
	}
	noPanic(t, "Cancel after a stop that recorded nothing", r.Cancel)
	noPanic(t, "a second Cancel", r.Cancel)
	noPanic(t, "a second Stop", r.Stop)
	if r.State() != Stopped {
		t.Errorf("state %s after the popover closed", r.State())
	}
	if n := dev.closes.Load(); n != 1 {
		t.Errorf("the microphone closed %d times, want once", n)
	}
}

// Stop and Cancel in every state, in either order and twice over: nothing
// panics, the microphone closes exactly once, and the state each leaves is
// the first one's (a Cancel during Opening or Starting cancels, a Stop
// then does nothing; after Stopped or Failed both do nothing).
func TestRecorderStopAndCancelInEveryState(t *testing.T) {
	type step func(r *Recorder)
	stop := func(r *Recorder) { r.Stop() }
	cancel := func(r *Recorder) { r.Cancel() }
	for _, c := range []struct {
		name  string
		at    State
		steps []step
		want  State
	}{
		{"opening: cancel, cancel, stop", Opening, []step{cancel, cancel, stop}, Cancelled},
		{"starting: cancel, stop, cancel", Starting, []step{cancel, stop, cancel}, Cancelled},
		{"recording: stop, stop, cancel", Recording, []step{stop, stop, cancel}, Stopped},
		{"recording: cancel, stop, cancel", Recording, []step{cancel, stop, cancel}, Cancelled},
		{"failed: cancel, stop, cancel", Failed, []step{cancel, stop, cancel}, Failed},
	} {
		t.Run(c.name, func(t *testing.T) {
			dev := &quietMic{}
			var d huddle.Device = dev
			if c.at == Opening {
				dev.hold = make(chan struct{})
			}
			if c.at == Failed {
				d = failDevice{}
			}
			r := Start(Options{Device: d})
			switch c.at {
			case Opening:
				time.Sleep(20 * time.Millisecond) // in the device's Start
				if r.State() != Opening {
					t.Fatalf("state %s, want opening", r.State())
				}
			default:
				waitState(t, r, c.at)
			}
			for i, s := range c.steps {
				noPanic(t, "step "+strconv.Itoa(i), func() { s(r) })
			}
			if dev.hold != nil {
				close(dev.hold) // the device's Start returns: open finds the recording cancelled
			}
			if r.State() != c.want {
				t.Errorf("state %s, want %s", r.State(), c.want)
			}
			if c.at == Failed {
				return
			}
			deadline := time.Now().Add(5 * time.Second)
			for dev.closes.Load() == 0 && time.Now().Before(deadline) {
				time.Sleep(5 * time.Millisecond)
			}
			time.Sleep(50 * time.Millisecond) // a second Close would come by now
			if n := dev.closes.Load(); n != 1 {
				t.Errorf("the microphone closed %d times, want once", n)
			}
		})
	}
}
