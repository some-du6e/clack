package jsrt

import (
	"context"
	"encoding/binary"
	"math"
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

// installAudio defines the audio host functions on h (installHost).
// Without Options.Audio none is defined: decodeAudioData rejects and
// MediaRecorder supports no type, as a browser without the codecs.
func (r *Runtime) installAudio(def func(name string, fn HostFunc)) {
	host := r.opts.Audio
	if host == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-r.done
		cancel()
	}()
	arg := func(a []Value, i int) Value {
		if i < len(a) {
			return a[i]
		}
		return Value{}
	}
	// audioDecode(bytes, rate): {rate, channels, length, data}, data the
	// channels' float32 samples one after another (little-endian, as a
	// Float32Array over the bytes reads them); rejects with EncodingError
	def("audioDecode", func(js *Ctx, _ Value, a []Value) (any, error) {
		data, rate := arg(a, 0).Bytes(), arg(a, 1).Int()
		p, resolve, reject := js.NewPromise()
		go func() {
			chans, err := host.Decode(ctx, data, rate)
			if err != nil || len(chans) == 0 || len(chans[0]) == 0 {
				reject(&JSError{Name: "EncodingError", Message: "Unable to decode audio data"})
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
		}()
		return p, nil
	})
	// audioSupports(mime): MediaRecorder.isTypeSupported
	def("audioSupports", func(js *Ctx, _ Value, a []Value) (any, error) {
		return host.Supports(arg(a, 0).String()), nil
	})
	// audioEncode(pcm, ch, rate, bps, mime): the recording's file bytes;
	// pcm is interleaved float32 (a Float32Array)
	def("audioEncode", func(js *Ctx, _ Value, a []Value) (any, error) {
		raw := arg(a, 0).Bytes()
		ch, rate, bps, mime := arg(a, 1).Int(), arg(a, 2).Int(), arg(a, 3).Int(), arg(a, 4).String()
		p, resolve, reject := js.NewPromise()
		go func() {
			pcm := make([]float32, len(raw)/4)
			for i := range pcm {
				pcm[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[4*i:]))
			}
			out, err := host.Encode(pcm, ch, rate, bps, mime)
			if err != nil {
				reject(&JSError{Name: "UnknownError", Message: "MediaRecorder: " + err.Error()})
				return
			}
			resolve(out)
		}()
		return p, nil
	})
}
