package goaudio

import "math"

// The output is 48 kHz stereo (the huddle's sound card format). A file at
// another rate goes through resampler, a playbackRate other than 1 through
// stretcher, which keeps the pitch as Chromium's AudioRendererAlgorithm
// does (WSOLA).

const outRate = 48000

// resampler converts interleaved float frames from inRate to 48 kHz with
// a Blackman-windowed sinc (8 zero crossings a side; the cutoff follows
// the lower of the two rates).
type resampler struct {
	ratio  float64 // input frames per output frame
	ch     int
	cutoff float64
	hist   []float32 // input frames from frame base on
	base   int64
	t      float64 // the next output frame's position, in input frames
}

const sincZeros = 8

func newResampler(inRate, ch int) *resampler {
	r := &resampler{ratio: float64(inRate) / outRate, ch: ch, cutoff: math.Min(1, outRate/float64(inRate))}
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
	w := float64(sincZeros) / r.cutoff // the kernel's half width, in input frames
	for n < len(out)/r.ch {
		c := int64(math.Floor(r.t))
		if c+int64(w)+1 >= r.base+have && !flush {
			break
		}
		if flush && c >= r.base+have {
			break
		}
		for k := 0; k < r.ch; k++ {
			var acc, norm float64
			lo := c - int64(w)
			hi := c + int64(w) + 1
			for j := lo; j <= hi; j++ {
				x := r.t - float64(j)
				if x <= -w || x >= w {
					continue
				}
				kv := sinc(x*r.cutoff) * r.cutoff * blackman(x/w)
				norm += kv
				if i := j - r.base; i >= 0 && i < have {
					acc += kv * float64(r.hist[int(i)*r.ch+k])
				}
			}
			if norm != 0 {
				acc /= norm // unity gain at DC
			}
			out[n*r.ch+k] = float32(acc)
		}
		n++
		r.t += r.ratio
	}
	// drop input no output will need again
	if keep := int64(math.Floor(r.t)) - int64(w) - 1 - r.base; keep > 0 {
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
