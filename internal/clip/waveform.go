package clip

import (
	"math"
	"sync"
)

// Waveform is the recorder's waveform (AudioWaveformRecorder, boot-shared
// Zkm/): an AnalyserNode on the microphone (fftSize 32,
// smoothingTimeConstant .2) read every 20 ms. Each read is
// getByteFrequencyData's 16 bins; the read's level is
// sqrt(mean((bin/229.5)²)), and every 5 reads (100 ms) the loudest of them,
// capped at 1, becomes a dot. The strip keeps the last 24 dots and starts
// as 24 × -1 (no dot yet). At stop the dots × 100 are the File's
// audio_wave_samples.
//
// The analyser is Chromium's RealtimeAnalyser: the last fftSize samples
// in, a Blackman window (alpha .16), the DFT (Blink's FFTFrame is
// unscaled; the Nyquist term packed in imag[0] is zeroed), magnitudes ×
// 1/fftSize, each smoothed with the last read's (k·old + (1-k)·new, in
// float32), then 20·log10 scaled from minDecibels -100..maxDecibels -30
// to 0..255 and truncated.
type Waveform struct {
	mu    sync.Mutex
	ring  [analyserFFT]float32 // the last fftSize samples
	pos   int
	mag   [analyserBins]float32 // the smoothed magnitudes
	reads [dotReads]float64
	nread int
	dots  [WaveDots]float64
}

const (
	analyserFFT  = 32
	analyserBins = analyserFFT / 2
	smoothing    = 0.2
	minDecibels  = -100.0
	maxDecibels  = -30.0
	dotReads     = 5
	// WaveDots is the strip's length (Array(24)).
	WaveDots = 24
	// WaveReadMs is how often the analyser is read (setInterval 20).
	WaveReadMs = 20
)

// blackman is ApplyWindow's window for fftSize samples, as float32.
var blackman = func() (w [analyserFFT]float32) {
	const alpha = 0.16
	a0, a1, a2 := 0.5*(1-alpha), 0.5, 0.5*alpha
	for i := range w {
		x := float64(i) / analyserFFT
		w[i] = float32(a0 - a1*math.Cos(2*math.Pi*x) + a2*math.Cos(2*math.Pi*2*x))
	}
	return w
}()

// NewWaveform starts the strip empty (24 × -1).
func NewWaveform() *Waveform {
	w := &Waveform{}
	for i := range w.dots {
		w.dots[i] = -1
	}
	return w
}

// Write feeds the analyser the microphone's samples (mono 48 kHz).
func (w *Waveform) Write(pcm []int16) {
	w.mu.Lock()
	for _, v := range pcm {
		w.ring[w.pos] = float32(v) / 32768
		w.pos = (w.pos + 1) % analyserFFT
	}
	w.mu.Unlock()
}

// Read is one 20 ms tick: getByteFrequencyData and the read's level; every
// fifth read adds a dot and returns true.
func (w *Waveform) Read() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	var bins [analyserBins]uint8
	w.byteFrequencyData(&bins)
	t := 0.0
	for _, b := range bins {
		v := float64(b) / 229.5
		t += v * v
	}
	w.reads[w.nread] = math.Sqrt(t / analyserBins)
	w.nread++
	if w.nread < dotReads {
		return false
	}
	w.nread = 0
	m := w.reads[0]
	for _, v := range w.reads[1:] {
		m = math.Max(m, v)
	}
	copy(w.dots[:], w.dots[1:])
	w.dots[WaveDots-1] = math.Min(1, m)
	return true
}

// Dots is the strip now, oldest first (-1: no dot yet).
func (w *Waveform) Dots() []float64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]float64(nil), w.dots[:]...)
}

// Samples is the File's audio_wave_samples: the dots × 100.
func (w *Waveform) Samples() []float64 {
	d := w.Dots()
	for i := range d {
		d[i] *= 100
	}
	return d
}

// byteFrequencyData is RealtimeAnalyser::GetByteFrequencyData with a fresh
// DoFFTAnalysis (every 20 ms read is in a later render quantum).
func (w *Waveform) byteFrequencyData(out *[analyserBins]uint8) {
	var x [analyserFFT]float32
	for i := range x {
		x[i] = w.ring[(w.pos+i)%analyserFFT] * blackman[i]
	}
	const scale = 1.0 / analyserFFT
	for k := 0; k < analyserBins; k++ {
		var re, im float64
		for n, v := range x {
			a := -2 * math.Pi * float64(k*n) / analyserFFT
			re += float64(v) * math.Cos(a)
			im += float64(v) * math.Sin(a)
		}
		re, im = float64(float32(re)), float64(float32(im)) // FFTFrame's float32 output
		if k == 0 {
			im = 0 // the packed Nyquist component
		}
		mag := math.Hypot(re, im) * scale
		s := float32(smoothing*float64(w.mag[k]) + (1-smoothing)*mag)
		if math.IsNaN(float64(s)) || math.IsInf(float64(s), 0) {
			s = 0
		}
		w.mag[k] = s
		db := 20 * math.Log10(float64(s)) // LinearToDecibels; -Inf for 0
		v := 255 * (db - minDecibels) / (maxDecibels - minDecibels)
		switch {
		case math.IsNaN(v) || v < 0:
			v = 0
		case v > 255:
			v = 255
		}
		out[k] = uint8(v)
	}
}

// Resample is AudioWaveform's data for width dots (boot-shared d0gJ): the
// data as it is when it already has width values (or width isn't a whole
// positive number); more values are folded by root mean square over each
// dot's span (rounded bounds), fewer are interpolated linearly.
func Resample(data []float64, width int) []float64 {
	n := len(data)
	if n == 0 || width <= 0 || n == width {
		return data
	}
	out := make([]float64, 0, width)
	if n > width {
		step := float64(n) / float64(width)
		for i := 0; i < width; i++ {
			t := float64(i) * step
			lo := max(0, int(math.Round(t)))
			hi := min(n, int(math.Round(t+step)))
			s := 0.0
			for j := lo; j < hi; j++ {
				s += data[j] * data[j]
			}
			out = append(out, math.Sqrt(s/float64(hi-lo))) // NaN for an empty span, as in JS
		}
		return out
	}
	if width == 1 {
		return append(out, data[0])
	}
	step := float64(n-1) / float64(width-1)
	for i := 0; i < width; i++ {
		t := float64(i) * step
		lo := int(math.Floor(t))
		hi := min(n-1, int(math.Ceil(t)))
		f := t - float64(lo)
		out = append(out, data[hi]*f+data[lo]*(1-f))
	}
	return out
}

// ScrubberDots is how many dots the audio player's scrubber draws in w px
// (Scrubber: the width cut to a multiple of 3, then to an odd multiple of
// 3, one dot per 6 px plus the last).
func ScrubberDots(w int) (dots, px int) {
	i := w - w%3
	r := i
	if i%6 == 0 {
		r = i - 3
	}
	if w <= 0 || r <= 0 {
		return 0, 0
	}
	return (r + 3) / 6, r
}

// DotHeight is a dot's height in px for value v: AudioWaveform's style
// height min(minHeight + 34·v, 34) (the recorder's minHeight is 3, a
// player's 6), held up by the dot's CSS min-height, which is the same
// number (.p-audio_waveform__dot 3px, 6px in .c-audio_player_scrubber). A
// dot with no value yet (-1) gets a negative height, which CSS ignores:
// minHeight.
func DotHeight(v, minHeight float64) float64 {
	return math.Max(minHeight, math.Min(minHeight+34*v, 34))
}
