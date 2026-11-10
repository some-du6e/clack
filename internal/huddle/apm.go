package huddle

import (
	"math"
	"sync"

	rnnoise "github.com/VoiceBlender/rnnoise-go"
	rnnmodel "github.com/VoiceBlender/rnnoise-go/model"
)

// The microphone's audio processing: Preferences > Audio & video "Enable
// automatic gain control" and "Enable noise suppression".
//
// Slack keeps both on the device (localStorage prefs.audio, enableAGC
// default true, enableNoiseSuppression default false) and opens the
// huddle microphone with getHuddleMicrophoneConstraints (client-boot
// 8Di1): {echoCancellation: {ideal: true}, autoGainControl: {ideal:
// enableAGC}} plus, when noise suppression is on, {noiseSuppression:
// {ideal: false}}: it turns the browser's suppressor off because the
// Chime SDK's Voice Focus (a proprietary WebAssembly model fetched from
// Amazon) replaces it (MediaSessionSdk.voiceFocusManager). Chromium's
// autoGainControl is WebRTC's AGC2 adaptive digital controller.
//
// Clack's equivalents run on the send goroutine, before the Opus encoder:
// noise suppression is RNNoise (third_party/rnnoise-go, BSD-3, a pure-Go
// port of Xiph's model), and gain control is a port of AGC2's adaptive
// digital controller (speech level, saturation protector, noise-limited
// gain, 6 dB/s slew, then a limiter), with RNNoise's voice probability as
// its VAD like AGC2's own RNN VAD.

// AGC2's AdaptiveDigital defaults (audio_processing.h) and agc2_common.h.
const (
	agcHeadroomDB          = 5.0   // headroom_db
	agcMaxGainDB           = 50.0  // max_gain_db
	agcInitialGainDB       = 15.0  // initial_gain_db
	agcMaxGainChangeDBps   = 6.0   // max_gain_change_db_per_second
	agcMaxOutputNoiseDBFS  = -50.0 // max_output_noise_level_dbfs
	agcVadThreshold        = 0.95  // kVadConfidenceThreshold
	agcAdjacentSpeech      = 12    // kAdjacentSpeechFramesThreshold (10 ms frames)
	agcInitialSpeechDBFS   = -30.0 // kInitialSpeechLevelEstimateDbfs
	agcLevelConfidenceMs   = 400   // kLevelEstimatorTimeToConfidenceMs
	agcSatInitialHeadroom  = 20.0  // kSaturationProtectorInitialHeadroomDb
	agcSatMinHeadroom      = 12.0  // saturation_protector.cc kMinMarginDb
	agcSatMaxHeadroom      = 25.0  // kMaxMarginDb
	agcSatAttack           = 0.9988
	agcSatDecay            = 0.9997
	agcNoiseRiseDBps       = 2.0 // the noise floor's climb (it falls at once)
	agcLimiterCeiling      = 0.98 * 32767
	agcLimiterReleaseDBps  = 20.0
	apmFrame               = sampleRate / 100 // 10 ms, AGC2's and RNNoise's frame
	agcMaxGainChangePer10  = agcMaxGainChangeDBps / 100
	agcNoiseRisePer10      = agcNoiseRiseDBps / 100
	vadDelay               = 2 // RNNoise's voice probability is for the frame 20 ms back
	agcLimiterReleasePer10 = agcLimiterReleaseDBps / 100
	minDBFS                = -100.0
)

// micProcessor is the capture chain. process runs on one goroutine (the
// sender); set may be called from any.
type micProcessor struct {
	mu      sync.Mutex
	agcOn   bool
	nsOn    bool
	dn      *rnnoise.Denoiser // nil until either is on (or if it failed)
	dnErr   bool
	f32     []float32
	agc     agc2
	lastVAD float32
}

func newMicProcessor(agc, ns bool) *micProcessor {
	p := &micProcessor{f32: make([]float32, apmFrame)}
	p.agc.reset()
	p.set(agc, ns)
	return p
}

// set turns the stages on or off (the prefs changed mid-huddle).
func (p *micProcessor) set(agc, ns bool) {
	p.mu.Lock()
	if agc && !p.agcOn {
		p.agc.reset()
	}
	p.agcOn, p.nsOn = agc, ns
	p.mu.Unlock()
}

func (p *micProcessor) enabled() (agc, ns bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.agcOn, p.nsOn
}

// process runs the chain over pcm (any whole number of 10 ms frames) in
// place.
func (p *micProcessor) process(pcm []int16) {
	agcOn, nsOn := p.enabled()
	if !agcOn && !nsOn {
		return
	}
	if p.dn == nil && !p.dnErr {
		d, err := rnnoise.New(rnnoise.Options{SampleRate: sampleRate, Model: rnnmodel.MustLoad()})
		if err != nil || d.FrameSize() != apmFrame {
			p.dnErr = true
		} else {
			p.dn = d
		}
	}
	for off := 0; off+apmFrame <= len(pcm); off += apmFrame {
		fr := pcm[off : off+apmFrame]
		for i, v := range fr {
			p.f32[i] = float32(v)
		}
		vad := float32(-1) // unknown: AGC2 then holds its gain
		if p.dn != nil {
			// RNNoise always runs while either stage is on: its voice
			// probability is the AGC's VAD. Its output (20 ms late) is
			// only used with noise suppression on.
			out := p.f32
			if !nsOn {
				out = p.agc.scratch()
			}
			if v, err := p.dn.Process(out, p.f32); err == nil {
				vad = v
			}
		}
		p.lastVAD = vad
		if agcOn {
			p.agc.process(p.f32, vad)
		}
		for i, v := range p.f32 {
			fr[i] = clamp16(v)
		}
	}
}

func clamp16(v float32) int16 {
	switch {
	case v >= 32767:
		return 32767
	case v <= -32768:
		return -32768
	}
	return int16(v)
}

// agc2 is AGC2's adaptive digital controller on 10 ms float frames in the
// int16 range.
type agc2 struct {
	gainDB float64 // the applied gain at the end of the last frame

	// speech level (SpeechLevelEstimator): a leaky average of speech
	// frames' RMS, trusted after agcAdjacentSpeech frames in a row
	speechDB     float64
	speechWeight float64
	adjacent     int

	// saturation protector: the speech peak above the level
	headroomDB float64

	// the last frames' levels: the VAD answers for the frame vadDelay back
	hist [vadDelay + 1]struct{ rms, peak float64 }

	// noise floor (NoiseLevelEstimator): set by the first frame that
	// isn't speech, then falls at once and climbs slowly
	noiseDB   float64
	noiseSeen bool

	limGain float64 // the limiter's linear gain (≤ 1)
	tmp     []float32
}

func (a *agc2) reset() {
	*a = agc2{gainDB: agcInitialGainDB, speechDB: agcInitialSpeechDBFS, headroomDB: agcSatInitialHeadroom, limGain: 1, tmp: a.tmp}
	for i := range a.hist {
		a.hist[i].rms, a.hist[i].peak = minDBFS, minDBFS
	}
}

func (a *agc2) scratch() []float32 {
	if len(a.tmp) != apmFrame {
		a.tmp = make([]float32, apmFrame)
	}
	return a.tmp
}

func dbfs(v float64) float64 {
	if v <= 1e-9 {
		return minDBFS
	}
	return 20 * math.Log10(v/32768)
}

func fromDB(db float64) float64 { return math.Pow(10, db/20) }

// process applies the gain to fr in place; vad is the frame's speech
// probability (negative: unknown).
func (a *agc2) process(fr []float32, vad float32) {
	var sum, peak float64
	for _, v := range fr {
		f := float64(v)
		sum += f * f
		if f < 0 {
			f = -f
		}
		peak = max(peak, f)
	}
	copy(a.hist[1:], a.hist[:vadDelay])
	a.hist[0].rms, a.hist[0].peak = dbfs(math.Sqrt(sum/float64(len(fr)))), dbfs(peak)
	// the levels the VAD's answer is about (vad < 0: no VAD, nothing is speech)
	rmsDB, peakDB := a.hist[vadDelay].rms, a.hist[vadDelay].peak
	speech := vad >= agcVadThreshold

	// noise floor from the frames that aren't speech
	if !speech && rmsDB > minDBFS {
		if !a.noiseSeen || rmsDB < a.noiseDB {
			a.noiseSeen = true
			a.noiseDB = rmsDB
		} else {
			a.noiseDB = min(rmsDB, a.noiseDB+agcNoiseRisePer10)
		}
	}

	// speech level: AGC2 averages the RMS of runs of speech frames, with
	// the weight saturating at agcLevelConfidenceMs
	if speech {
		a.adjacent++
	} else {
		a.adjacent = 0
	}
	if speech && a.adjacent >= agcAdjacentSpeech {
		const maxW = agcLevelConfidenceMs / 10
		a.speechWeight = min(a.speechWeight+1, maxW)
		a.speechDB += (rmsDB - a.speechDB) / a.speechWeight
		// saturation protector: the margin follows the speech's peak over
		// its level, quickly up and slowly down, within AGC2's 12–25 dB
		diff := peakDB - a.speechDB
		k := agcSatDecay
		if diff > a.headroomDB {
			k = agcSatAttack
		}
		a.headroomDB = min(max(k*a.headroomDB+(1-k)*diff, agcSatMinHeadroom), agcSatMaxHeadroom)
	}

	// the target (AdaptiveDigitalGainController::ComputeGainDb), capped so
	// the noise floor stays under max_output_noise_level_dbfs
	in := a.speechDB + a.headroomDB
	target := 0.0
	switch {
	case in < -(agcHeadroomDB + agcMaxGainDB):
		target = agcMaxGainDB
	case in < -agcHeadroomDB:
		target = -agcHeadroomDB - in
	}
	if a.noiseSeen {
		target = min(target, max(agcMaxOutputNoiseDBFS-a.noiseDB, 0))
	}

	// slew: up only on confident speech runs, down any time
	g := a.gainDB
	switch {
	case target > g && speech && a.adjacent >= agcAdjacentSpeech:
		g = min(target, g+agcMaxGainChangePer10)
	case target < g:
		g = max(target, g-agcMaxGainChangePer10)
	}

	// the gain ramps linearly over the frame (GainApplier), then the
	// limiter keeps the peak under the ceiling
	g0, g1 := fromDB(a.gainDB), fromDB(g)
	a.gainDB = g
	lim := min(1, a.limGain*fromDB(agcLimiterReleasePer10))
	if over := peak * g1 * lim; over > agcLimiterCeiling {
		lim *= agcLimiterCeiling / over
	}
	l0 := a.limGain
	a.limGain = lim
	n := float64(len(fr))
	for i := range fr {
		t := float64(i+1) / n
		gi := (g0 + (g1-g0)*t) * (l0 + (lim-l0)*t)
		v := float64(fr[i]) * gi
		if v > agcLimiterCeiling {
			v = agcLimiterCeiling
		} else if v < -agcLimiterCeiling {
			v = -agcLimiterCeiling
		}
		fr[i] = float32(v)
	}
}
