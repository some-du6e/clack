package huddle

import (
	"math"
	"math/rand"
	"testing"
)

// synthSpeech is a voiced, speech-like test signal at 48 kHz: a glottal
// pulse train (f0 gliding 100–150 Hz) through three formant resonators
// that change per syllable, in 250 ms syllables with 120 ms gaps and a
// 600 ms pause every 4 syllables. on[i] reports whether sample i is
// inside a syllable. The RMS over the syllables is rmsDB dBFS.
func synthSpeech(seconds float64, rmsDB float64) (pcm []float64, on []bool) {
	n := int(seconds * sampleRate)
	pcm, on = make([]float64, n), make([]bool, n)
	vowels := [][3]float64{{730, 1090, 2440}, {270, 2290, 3010}, {530, 1840, 2480}, {300, 870, 2240}, {660, 1720, 2410}}
	type res struct{ a1, a2, b, y1, y2 float64 }
	mk := func(f, bw float64) res {
		r := math.Exp(-math.Pi * bw / sampleRate)
		th := 2 * math.Pi * f / sampleRate
		return res{a1: 2 * r * math.Cos(th), a2: -r * r, b: 1 - r}
	}
	var rs [3]res
	phase, t, syl := 0.0, 0, 0
	for t < n {
		v := vowels[syl%len(vowels)]
		for k := range rs {
			rs[k] = mk(v[k], 80+40*float64(k))
		}
		sylLen := int(0.25 * sampleRate)
		for i := 0; i < sylLen && t < n; i, t = i+1, t+1 {
			env := math.Sin(math.Pi * float64(i) / float64(sylLen)) // rise and fall
			f0 := 100 + 50*float64(i)/float64(sylLen)
			phase += f0 / sampleRate
			x := 0.0
			if phase >= 1 {
				phase -= 1
				x = 1
			}
			for k := range rs {
				r := &rs[k]
				y := r.b*x + r.a1*r.y1 + r.a2*r.y2
				r.y2, r.y1 = r.y1, y
				x = y
			}
			pcm[t], on[t] = x*env, true
		}
		gap := int(0.12 * sampleRate)
		if syl%4 == 3 {
			gap = int(0.6 * sampleRate)
		}
		t += gap
		syl++
	}
	var s float64
	var c int
	for i, v := range pcm {
		if on[i] {
			s += v * v
			c++
		}
	}
	scale := fromDB(rmsDB) * 32768 / math.Sqrt(s/float64(c))
	for i := range pcm {
		pcm[i] *= scale
	}
	return pcm, on
}

func addNoise(pcm []float64, dbFS float64, seed int64) []int16 {
	r := rand.New(rand.NewSource(seed))
	amp := fromDB(dbFS) * 32768
	out := make([]int16, len(pcm))
	for i, v := range pcm {
		out[i] = clamp16(float32(v + amp*r.NormFloat64()))
	}
	return out
}

// levels is the RMS (dBFS) of pcm inside and outside the syllables, from
// sample `from` on; delay shifts the mask (RNNoise's output is 20 ms late).
func levels(pcm []int16, on []bool, from, delay int) (speech, gaps float64) {
	var s, g float64
	var sc, gc int
	for i := from; i < len(pcm); i++ {
		j := i - delay
		if j < 0 {
			continue
		}
		f := float64(pcm[i])
		// skip the 30 ms around each edge so smearing doesn't count
		edge := false
		for _, d := range []int{-1440, 1440} {
			if k := j + d; k >= 0 && k < len(on) && on[k] != on[j] {
				edge = true
			}
		}
		if edge {
			continue
		}
		if on[j] {
			s += f * f
			sc++
		} else {
			g += f * f
			gc++
		}
	}
	return dbfs(math.Sqrt(s / float64(max(sc, 1)))), dbfs(math.Sqrt(g / float64(max(gc, 1))))
}

func runMic(p *micProcessor, in []int16) []int16 {
	out := append([]int16(nil), in...)
	for off := 0; off+frameSamples <= len(out); off += frameSamples {
		p.process(out[off : off+frameSamples])
	}
	return out
}

// Automatic gain control lifts quiet speech toward AGC2's target, keeps
// loud speech from clipping, and doesn't pump the noise floor past
// max_output_noise_level_dbfs. Off, the microphone passes untouched.
func TestMicAGC(t *testing.T) {
	const secs = 14.0
	settle := int(8 * sampleRate) // measure once the gain has converged
	quiet, on := synthSpeech(secs, -42)
	in := addNoise(quiet, -75, 1)

	off := runMic(newMicProcessor(false, false), in)
	for i := range in {
		if off[i] != in[i] {
			t.Fatal("AGC and NS off changed the microphone")
		}
	}

	p := newMicProcessor(true, false)
	out := runMic(p, in)
	inS, inG := levels(in, on, settle, 0)
	outS, outG := levels(out, on, settle, 0)
	t.Logf("quiet: speech %.1f → %.1f dBFS, gaps %.1f → %.1f dBFS, gain %.1f dB, speech level %.1f, headroom %.1f, noise %.1f",
		inS, outS, inG, outG, p.agc.gainDB, p.agc.speechDB, p.agc.headroomDB, p.agc.noiseDB)
	if gain := outS - inS; gain < 12 {
		t.Errorf("quiet speech gained %.1f dB, want ≥ 12 (AGC2 brings it toward -%.0f dBFS peaks)", gain, agcHeadroomDB)
	}
	if outS < -32 || outS > -12 {
		t.Errorf("quiet speech came out at %.1f dBFS, want -32…-12", outS)
	}
	if outG > agcMaxOutputNoiseDBFS+1 {
		t.Errorf("the noise floor came out at %.1f dBFS, over AGC2's %.0f", outG, agcMaxOutputNoiseDBFS)
	}

	// loud speech (peaks near full scale): no boost, nothing clips
	loud, on2 := synthSpeech(6, -10)
	in2 := addNoise(loud, -70, 2)
	p2 := newMicProcessor(true, false)
	out2 := runMic(p2, in2)
	clipped := 0
	for _, v := range out2 {
		if v >= 32767 || v <= -32768 {
			clipped++
		}
	}
	lS, _ := levels(in2, on2, 3*sampleRate, 0)
	oS, _ := levels(out2, on2, 3*sampleRate, 0)
	t.Logf("loud: speech %.1f → %.1f dBFS, gain %.1f dB, %d clipped", lS, oS, p2.agc.gainDB, clipped)
	if clipped > 0 {
		t.Errorf("%d samples clipped", clipped)
	}
	if oS > lS+1 {
		t.Errorf("loud speech was boosted %.1f dB", oS-lS)
	}
}

// Noise suppression (RNNoise) takes broadband noise out of the gaps
// between words while keeping the words.
func TestMicNoiseSuppression(t *testing.T) {
	speech, on := synthSpeech(8, -26)
	in := addNoise(speech, -40, 3) // 14 dB SNR
	p := newMicProcessor(false, true)
	out := runMic(p, in)
	from := 2 * sampleRate
	delay := 2 * apmFrame // RNNoise's algorithmic delay
	inS, inG := levels(in, on, from, 0)
	outS, outG := levels(out, on, from, delay)
	t.Logf("speech %.1f → %.1f dBFS, noise in the gaps %.1f → %.1f dBFS", inS, outS, inG, outG)
	if red := inG - outG; red < 12 {
		t.Errorf("noise in the gaps fell %.1f dB, want ≥ 12", red)
	}
	if loss := inS - outS; loss > 6 {
		t.Errorf("speech lost %.1f dB, want ≤ 6", loss)
	}
	if snrIn, snrOut := inS-inG, outS-outG; snrOut < snrIn+10 {
		t.Errorf("SNR %.1f → %.1f dB, want ≥ 10 dB better", snrIn, snrOut)
	}
}

// The sender runs the chain: set changes it mid-huddle.
func TestMicProcessorToggle(t *testing.T) {
	p := newMicProcessor(true, true)
	if a, n := p.enabled(); !a || !n {
		t.Fatal("not on")
	}
	p.set(false, false)
	buf := []int16{1, 2, 3}
	p.process(buf)
	if buf[0] != 1 || buf[2] != 3 {
		t.Fatal("off but processed")
	}
}
