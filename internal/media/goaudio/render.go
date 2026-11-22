package goaudio

import "math"

// The output is 48 kHz stereo (the huddle's sound card format). A file at
// another rate goes through resampler, a playbackRate other than 1 through
// stretcher, which keeps the pitch as Chromium's AudioRendererAlgorithm
// does (WSOLA).

const outRate = 48000

// resampler converts interleaved float frames from inRate to 48 kHz with
// a Blackman-windowed sinc (8 zero crossings a side; the cutoff follows
// the lower of the two rates). The kernel is made once, at kernPhases
// offsets between input frames (each normalized to unity gain at DC):
// the device callback interpolates between the two nearest and takes a
// dot product per channel.
type resampler struct {
	ratio float64 // input frames per output frame
	ch    int
	half  int       // taps each side of the frame before the output's position
	taps  int       // 2*half+2
	kern  []float32 // kernPhases+1 rows of taps
	row   []float32 // the interpolated row
	hist  []float32 // input frames from frame base on
	base  int64
	t     float64 // the next output frame's position, in input frames
}

const (
	sincZeros  = 8
	kernPhases = 256
)

func newResampler(inRate, ch int) *resampler {
	r := &resampler{ratio: float64(inRate) / outRate, ch: ch}
	if r.passthrough() {
		return r
	}
	cutoff := math.Min(1, outRate/float64(inRate))
	w := float64(sincZeros) / cutoff // the kernel's half width, in input frames
	r.half = int(math.Ceil(w))
	r.taps = 2*r.half + 2
	r.kern = make([]float32, (kernPhases+1)*r.taps)
	r.row = make([]float32, r.taps)
	vals := make([]float64, r.taps)
	for p := 0; p <= kernPhases; p++ {
		frac := float64(p) / kernPhases
		var norm float64
		for i := range vals {
			x := frac - float64(i-r.half) // the output's position less tap i's frame
			vals[i] = 0
			if x > -w && x < w {
				vals[i] = sinc(x*cutoff) * cutoff * blackman(x/w)
			}
			norm += vals[i]
		}
		row := r.kern[p*r.taps : (p+1)*r.taps]
		for i, v := range vals {
			if norm != 0 {
				row[i] = float32(v / norm) // unity gain at DC
			}
		}
	}
	return r
}

func (r *resampler) passthrough() bool { return r.ratio == 1 }

// push adds input frames.
func (r *resampler) push(in []float32) { r.hist = append(r.hist, in...) }

// reset drops what's buffered (a seek).
func (r *resampler) reset() { r.hist, r.base, r.t = r.hist[:0], 0, 0 }

// pull writes up to len(out)/ch output frames; it stops where it would
// need input it doesn't have yet (flush: the input ended, so frames past
// it count as silence).
func (r *resampler) pull(out []float32, flush bool) int {
	if r.passthrough() {
		n := min(len(out), len(r.hist)) / r.ch
		copy(out, r.hist[:n*r.ch])
		r.hist = append(r.hist[:0], r.hist[n*r.ch:]...)
		return n
	}
	have := int64(len(r.hist) / r.ch)
	n := 0
	for n < len(out)/r.ch {
		c := int64(math.Floor(r.t))
		if c+int64(r.half)+1 >= r.base+have && !flush {
			break
		}
		if flush && c >= r.base+have {
			break
		}
		pf := (r.t - float64(c)) * kernPhases
		p0 := min(int(pf), kernPhases-1)
		a := float32(pf - float64(p0))
		row0 := r.kern[p0*r.taps : (p0+1)*r.taps]
		row1 := r.kern[(p0+1)*r.taps : (p0+2)*r.taps]
		for i := range r.row {
			r.row[i] = row0[i] + a*(row1[i]-row0[i])
		}
		first := c - int64(r.half) - r.base // tap 0's frame in hist
		for k := 0; k < r.ch; k++ {
			var acc float32
			for i, kv := range r.row {
				if j := first + int64(i); j >= 0 && j < have {
					acc += kv * r.hist[int(j)*r.ch+k]
				}
			}
			out[n*r.ch+k] = acc
		}
		n++
		r.t += r.ratio
	}
	// drop input no output will need again
	if keep := int64(math.Floor(r.t)) - int64(r.half) - 1 - r.base; keep > 0 {
		keep = min(keep, have)
		r.hist = append(r.hist[:0], r.hist[keep*int64(r.ch):]...)
		r.base += keep
	}
	return n
}

func sinc(x float64) float64 {
	if x == 0 {
		return 1
	}
	return math.Sin(math.Pi*x) / (math.Pi * x)
}

// blackman is the window over x in (-1, 1).
func blackman(x float64) float64 {
	t := (x + 1) / 2
	return 0.42 - 0.5*math.Cos(2*math.Pi*t) + 0.08*math.Cos(4*math.Pi*t)
}

// mixer folds a stream's channels into at most two before the resampler
// (Chromium's audio path mixes to the output's layout first too): mono
// and stereo pass; more fold to stereo by where each channel is (a WAV's
// channel mask, or the usual layout for the count: L R C, quad, 5.0, 5.1,
// 6.1, 7.1): the front left and right as they are, the centre and every
// other left or right channel at -3 dB, the LFE dropped, and each side's
// weights scaled to sum to at most 1, so nothing clips.
type mixer struct {
	in, out int
	w       [][2]float32 // each input channel's weight into L and R
}

// Speaker positions (WAVEFORMATEXTENSIBLE's dwChannelMask bits).
const (
	spFL  = 0x1
	spFR  = 0x2
	spFC  = 0x4
	spLFE = 0x8
	spBL  = 0x10
	spBR  = 0x20
	spFLC = 0x40
	spFRC = 0x80
	spBC  = 0x100
	spSL  = 0x200
	spSR  = 0x400
	spTC  = 0x800
	spTFL = 0x1000
	spTFC = 0x2000
	spTFR = 0x4000
	spTBL = 0x8000
	spTBC = 0x10000
	spTBR = 0x20000
)

// layouts are the usual masks by channel count (Chromium's
// GuessChannelLayout, WAVE_FORMAT_EXTENSIBLE's order).
var layouts = map[int]uint32{
	3: spFL | spFR | spFC,
	4: spFL | spFR | spBL | spBR,
	5: spFL | spFR | spFC | spBL | spBR,
	6: spFL | spFR | spFC | spLFE | spBL | spBR,
	7: spFL | spFR | spFC | spLFE | spBC | spSL | spSR,
	8: spFL | spFR | spFC | spLFE | spBL | spBR | spSL | spSR,
}

func newMixer(ch int, mask uint32) *mixer {
	m := &mixer{in: ch, out: min(ch, 2)}
	if ch <= 2 {
		return m
	}
	if bitsSet(mask) != ch {
		mask = layouts[ch]
	}
	const h = float32(0.70710678) // -3 dB
	m.w = make([][2]float32, ch)
	bit := uint32(1)
	for i := 0; i < ch; i++ {
		pos := uint32(0)
		for mask != 0 && bit != 0 && pos == 0 {
			if mask&bit != 0 {
				pos = bit
			}
			bit <<= 1
		}
		switch pos {
		case spFL:
			m.w[i] = [2]float32{1, 0}
		case spFR:
			m.w[i] = [2]float32{0, 1}
		case spLFE:
		case spFC, spBC, spTC, spTFC, spTBC:
			m.w[i] = [2]float32{h, h}
		case spBL, spFLC, spSL, spTFL, spTBL:
			m.w[i] = [2]float32{h, 0}
		case spBR, spFRC, spSR, spTFR, spTBR:
			m.w[i] = [2]float32{0, h}
		default: // no known place: alternately left and right
			m.w[i] = [2]float32{h * float32(1-i%2), h * float32(i%2)}
		}
	}
	for side := 0; side < 2; side++ {
		var sum float32
		for _, w := range m.w {
			sum += w[side]
		}
		if sum > 1 {
			for i := range m.w {
				m.w[i][side] /= sum
			}
		}
	}
	return m
}

func bitsSet(v uint32) int {
	n := 0
	for ; v != 0; v &= v - 1 {
		n++
	}
	return n
}

// mix folds frames frames of in (m.in channels) into out (m.out); in is
// returned as it is when there's nothing to fold.
func (m *mixer) mix(in []float32, frames int, out []float32) []float32 {
	if m.in <= 2 {
		return in[:frames*m.in]
	}
	out = out[:frames*2]
	for f := 0; f < frames; f++ {
		var l, r float32
		src := in[f*m.in : (f+1)*m.in]
		for i, v := range src {
			l += v * m.w[i][0]
			r += v * m.w[i][1]
		}
		out[2*f], out[2*f+1] = l, r
	}
	return out
}

// stretcher changes the playback rate keeping the pitch: WSOLA on 48 kHz
// stereo, 20 ms Hann windows at 50% overlap, each placed within 5 ms of
// its ideal input position where it best continues the last one.
type stretcher struct {
	rate float64
	in   []float32 // stereo input frames from frame base on
	base int64
	next float64 // the next window's ideal input position
	prev int64   // the last window's chosen start (-1: none)
	tail []float32
	win  []float32
}

const (
	wsolaN     = 960 // window
	wsolaH     = wsolaN / 2
	wsolaDelta = 240 // search tolerance
)

func newStretcher() *stretcher {
	s := &stretcher{prev: -1, tail: make([]float32, wsolaH*2), win: make([]float32, wsolaN)}
	for i := range s.win {
		s.win[i] = float32(0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/wsolaN))
	}
	return s
}

func (s *stretcher) reset() {
	s.in, s.base, s.next, s.prev = s.in[:0], 0, 0, -1
	clear(s.tail)
}

func (s *stretcher) push(in []float32) { s.in = append(s.in, in...) }

// buffered is the input frames waiting.
func (s *stretcher) buffered() int { return len(s.in) / 2 }

// pull writes whole hops (wsolaH frames each) into out; flush lets it
// read past the input's end as silence.
func (s *stretcher) pull(out []float32, flush bool) int {
	n := 0
	have := int64(len(s.in) / 2)
	for n+wsolaH <= len(out)/2 {
		ideal := int64(s.next)
		start := ideal
		need := ideal + wsolaDelta + wsolaN
		if need > s.base+have && !flush {
			break
		}
		if flush && ideal >= s.base+have {
			break
		}
		if s.prev >= 0 {
			start = s.best(ideal, have)
		}
		for i := 0; i < wsolaH; i++ {
			for k := 0; k < 2; k++ {
				a := s.at(start+int64(i), k, have) * s.win[i]
				out[(n+i)*2+k] = s.tail[i*2+k] + a
			}
		}
		for i := 0; i < wsolaH; i++ {
			for k := 0; k < 2; k++ {
				s.tail[i*2+k] = s.at(start+int64(wsolaH+i), k, have) * s.win[wsolaH+i]
			}
		}
		s.prev = start
		s.next += s.rate * wsolaH
		n += wsolaH
	}
	if keep := int64(s.next) - wsolaDelta - wsolaN - s.base; keep > 0 {
		keep = min(keep, have)
		s.in = append(s.in[:0], s.in[keep*2:]...)
		s.base += keep
	}
	return n
}

// at is input frame f's channel k (0 outside the buffer).
func (s *stretcher) at(f int64, k int, have int64) float32 {
	i := f - s.base
	if i < 0 || i >= have {
		return 0
	}
	return s.in[int(i)*2+k]
}

// best is the window start within wsolaDelta of ideal whose first half
// best matches what naturally follows the last window.
func (s *stretcher) best(ideal, have int64) int64 {
	natural := s.prev + wsolaH
	bestAt, bestC := ideal, math.Inf(-1)
	for d := int64(-wsolaDelta); d <= wsolaDelta; d += 2 {
		c := 0.0
		at := ideal + d
		if at < s.base {
			continue
		}
		for i := int64(0); i < wsolaH; i += 2 {
			x := s.at(at+i, 0, have) + s.at(at+i, 1, have)
			y := s.at(natural+i, 0, have) + s.at(natural+i, 1, have)
			c += float64(x * y)
		}
		if c > bestC {
			bestC, bestAt = c, at
		}
	}
	return bestAt
}
