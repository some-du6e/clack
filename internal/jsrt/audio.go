package jsrt

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"sync"
)

// AudioHost decodes and encodes the page's audio (Options.Audio): what
// Chromium's media pipeline does under Web Audio's decodeAudioData and
// MediaRecorder's encoders (dom.js keeps the audio graph, its clock and
// the recorder).
type AudioHost interface {
	// Decode is decodeAudioData's: the file's audio as one slice of float
	// samples per channel, converted to rate (the context's). An error is
	// decodeAudioData's EncodingError. Called on its own goroutine.
	Decode(ctx context.Context, data []byte, rate int) ([][]float32, error)
	// Supports is MediaRecorder.isTypeSupported for an audio type.
	Supports(mime string) bool
	// Encode is a MediaRecorder's recording: interleaved float PCM, ch
	// channels at rate, encoded as mime (one Supports took) at bps bits a
	// second. Called on its own goroutine.
	Encode(pcm []float32, ch, rate, bps int, mime string) ([]byte, error)
}

// Web Audio's bounds: a context's rate is 3000 to 768000 Hz and a buffer
// has 1 to 32 channels (BaseAudioContext, AudioBuffer). dom.js keeps to
// them; the host functions clamp what they're given to them too, as
// plugin code can call __jr.host itself, so no rate asks a resampler for
// gigabytes.
const (
	audioRateMin, audioRateMax = 3000, 768000
	audioChannelsMax           = 32
)

// maxRecordSamples caps the PCM the page's MediaRecorders hold to encode
// (frames × channels, every recording together: 128 MiB of float32, about
// six minutes of stereo at 48 kHz), as decodeAudioData's decode is capped
// (goaudio.MaxDecodeSamples). A recording that reaches it ends there: its
// recorder stops with an error, then hands over what it recorded.
// Chromium sets no such limit; its encoder streams to the blob. Tests
// lower it.
var maxRecordSamples = 1 << 25

// audioRecordings are the page's MediaRecorders' PCM, interleaved, as
// they record (the audioRecord host functions): the page's heap holds
// none of it, and stop() encodes what's here.
type audioRecordings struct {
	mu   sync.Mutex
	pcm  map[int][]float32
	held int // samples all of them hold
	next int
}

func clampRate(r int) int     { return min(max(r, audioRateMin), audioRateMax) }
func clampChannels(c int) int { return min(max(c, 1), audioChannelsMax) }

// installAudio defines the audio host functions on h (installHost).
// Without Options.Audio none is defined: decodeAudioData rejects and
// MediaRecorder supports no type, as a browser without the codecs.
func (r *Runtime) installAudio(def func(name string, fn HostFunc)) {
	host := r.opts.Audio
	if host == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	recs := &r.audioRecs
	recs.pcm = map[int][]float32{}
	go func() {
		<-r.done
		cancel()
		recs.mu.Lock()
		clear(recs.pcm)
		recs.held = 0
		recs.mu.Unlock()
	}()
	arg := func(a []Value, i int) Value {
		if i < len(a) {
			return a[i]
		}
		return Value{}
	}
	// spawn runs a host call off the JS thread. One that panics (another's
	// decoder on a malformed file) rejects its promise, never ends the
	// process: failed rejects it.
	spawn := func(what string, failed func(), work func()) {
		go func() {
			defer func() {
				if v := recover(); v != nil {
					r.opts.Log("error", fmt.Sprintf("jsrt: the audio host's %s panicked: %v", what, v))
					failed()
				}
			}()
			work()
		}()
	}
	// audioDecode(bytes, rate): {rate, channels, length, data}, data the
	// channels' float32 samples one after another (little-endian, as a
	// Float32Array over the bytes reads them); rejects with EncodingError
	def("audioDecode", func(js *Ctx, _ Value, a []Value) (any, error) {
		data, rate := arg(a, 0).Bytes(), clampRate(arg(a, 1).Int())
		p, resolve, reject := js.NewPromise()
		fail := func() { reject(&JSError{Name: "EncodingError", Message: "Unable to decode audio data"}) }
		spawn("decode", fail, func() {
			chans, err := host.Decode(ctx, data, rate)
			if err != nil || len(chans) == 0 || len(chans) > audioChannelsMax || len(chans[0]) == 0 {
				fail()
				return
			}
			n := len(chans[0])
			b := make([]byte, 4*n*len(chans))
			for c, s := range chans {
				o := 4 * n * c
				for i := 0; i < n && i < len(s); i++ {
					binary.LittleEndian.PutUint32(b[o+4*i:], math.Float32bits(s[i]))
				}
			}
			resolve(func(js *Ctx) (Value, error) {
				o := js.NewObject()
				_ = o.Set("rate", float64(rate))
				_ = o.Set("channels", float64(len(chans)))
				_ = o.Set("length", float64(n))
				_ = o.Set("data", b)
				return o, nil
			})
		})
		return p, nil
	})
	// audioSupports(mime): MediaRecorder.isTypeSupported
	def("audioSupports", func(js *Ctx, _ Value, a []Value) (any, error) {
		return host.Supports(arg(a, 0).String()), nil
	})
	// audioRecord(): a new recording's id
	def("audioRecord", func(js *Ctx, _ Value, a []Value) (any, error) {
		recs.mu.Lock()
		defer recs.mu.Unlock()
		recs.next++
		recs.pcm[recs.next] = nil
		return float64(recs.next), nil
	})
	// audioRecordPush(id, pcm): pcm (interleaved float32, a Float32Array)
	// added to the recording; false once the recordings hold
	// maxRecordSamples (what's past it is dropped) or for no recording
	def("audioRecordPush", func(js *Ctx, _ Value, a []Value) (any, error) {
		id, raw := arg(a, 0).Int(), arg(a, 1).Bytes()
		recs.mu.Lock()
		defer recs.mu.Unlock()
		pcm, ok := recs.pcm[id]
		if !ok {
			return false, nil
		}
		n := len(raw) / 4
		take := min(n, max(0, maxRecordSamples-recs.held))
		for i := 0; i < take; i++ {
			pcm = append(pcm, math.Float32frombits(binary.LittleEndian.Uint32(raw[4*i:])))
		}
		recs.pcm[id] = pcm
		recs.held += take
		return take == n, nil
	})
	// audioRecordEncode(id, ch, rate, bps, mime): the recording's file
	// bytes, and the recording let go; rejects with UnknownError
	def("audioRecordEncode", func(js *Ctx, _ Value, a []Value) (any, error) {
		id := arg(a, 0).Int()
		ch, rate, bps, mime := clampChannels(arg(a, 1).Int()), clampRate(arg(a, 2).Int()), arg(a, 3).Int(), arg(a, 4).String()
		recs.mu.Lock()
		pcm := recs.pcm[id]
		delete(recs.pcm, id)
		recs.held -= len(pcm)
		recs.mu.Unlock()
		p, resolve, reject := js.NewPromise()
		fail := func(why string) { reject(&JSError{Name: "UnknownError", Message: "MediaRecorder: " + why}) }
		spawn("encode", func() { fail("the encoder failed") }, func() {
			out, err := host.Encode(pcm, ch, rate, bps, mime)
			if err != nil {
				fail(err.Error())
				return
			}
			resolve(out)
		})
		return p, nil
	})
}
