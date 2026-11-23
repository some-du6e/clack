package jsrt

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeAudio is an AudioHost the tests steer: it notes what it's given, and
// its decoder and encoder can panic, as another's code can on a file.
type fakeAudio struct {
	mu                       sync.Mutex
	panicDecode, panicEncode bool
	rates                    []int // Decode's
	encodes                  []fakeEncode
}

type fakeEncode struct {
	samples, ch, rate int
	mime              string
}

func (h *fakeAudio) Decode(_ context.Context, data []byte, rate int) ([][]float32, error) {
	h.mu.Lock()
	h.rates = append(h.rates, rate)
	p := h.panicDecode
	h.mu.Unlock()
	if p {
		panic("index out of range [38] with length 38") // go-mp3's, on an MPEG-2 granule with mixed blocks
	}
	return [][]float32{make([]float32, 480)}, nil
}

func (h *fakeAudio) Supports(mime string) bool { return strings.HasPrefix(mime, "audio/mp4") }

func (h *fakeAudio) Encode(pcm []float32, ch, rate, bps int, mime string) ([]byte, error) {
	h.mu.Lock()
	h.encodes = append(h.encodes, fakeEncode{len(pcm), ch, rate, mime})
	p := h.panicEncode
	h.mu.Unlock()
	if p {
		panic("the encoder's own bug")
	}
	return []byte("m4a"), nil
}

func (h *fakeAudio) got() ([]int, []fakeEncode) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]int(nil), h.rates...), append([]fakeEncode(nil), h.encodes...)
}

// callJSON calls the page's fn and decodes its result into v.
func callJSON(t *testing.T, r *Runtime, fn string, v any, args ...any) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	got, err := r.Call(ctx, fn, args...)
	if err != nil {
		t.Fatalf("%s: %v", fn, err)
	}
	if err := json.Unmarshal(got, v); err != nil {
		t.Fatalf("%s: %s: %v", fn, got, err)
	}
}

// recordJS is a page recording an oscillator through a MediaStream
// destination: record(ms) stops after ms (or lets the recorder stop
// itself) and resolves with the recorder's events.
const recordJS = `
globalThis.record = (ms) => new Promise((resolve) => {
  const ctx = new AudioContext();
  const osc = ctx.createOscillator(), dest = ctx.createMediaStreamDestination();
  osc.connect(dest); osc.start();
  const rec = new MediaRecorder(dest.stream, { mimeType: 'audio/mp4' });
  const seen = [];
  rec.addEventListener('start', () => seen.push('start'));
  rec.addEventListener('error', (e) => seen.push('error ' + e.error.name + ': ' + e.error.message));
  rec.addEventListener('dataavailable', (e) => seen.push('dataavailable ' + e.data.size));
  rec.addEventListener('stop', () => { seen.push('stop ' + rec.state); ctx.close(); resolve(seen); });
  rec.start();
  if (ms > 0) setTimeout(() => rec.stop(), ms);
});
`

// TestAudioHostPanicsReject: a host whose decoder or encoder panics (a
// third-party decoder on a malformed file: go-mp3 on an MPEG-2 granule
// with mixed blocks) fails the page's call as any failure does,
// decodeAudioData with EncodingError and the recorder with an error event
// before its (empty) file and stop, and the runtime and the process carry
// on. The host's goroutine had no recover: the panic took the app down.
func TestAudioHostPanicsReject(t *testing.T) {
	host := &fakeAudio{panicDecode: true, panicEncode: true}
	r := newTest(t, func(o *Options) { withAudio(o, host) })
	evalString(t, r, recordJS+`
globalThis.decodeBad = async () => {
  const ctx = new AudioContext();
  try { await ctx.decodeAudioData(new Uint8Array([0xff, 0xf3, 0x80, 0xc0]).buffer); return 'resolved'; }
  catch (e) { return e.name + ': ' + e.message; } finally { ctx.close(); }
};
0`)
	var got string
	callJSON(t, r, "decodeBad", &got)
	if got != "EncodingError: Unable to decode audio data" {
		t.Errorf("decodeAudioData with a decoder that panics: %q, want EncodingError", got)
	}
	var seen []string
	callJSON(t, r, "record", &seen, 150)
	want := []string{"start", "error UnknownError: MediaRecorder: the encoder failed", "dataavailable 0", "stop inactive"}
	if strings.Join(seen, "|") != strings.Join(want, "|") {
		t.Errorf("a recording whose encoder panics: %q, want %q", seen, want)
	}
	if s := evalString(t, r, `String(6 * 7)`); s != "42" {
		t.Errorf("the runtime after the panics: %q", s)
	}
}

// TestAudioHostClampsRateAndChannels: plugin code can call the host
// functions itself (__jr.host), and what they hand the host keeps to Web
// Audio's bounds, as dom.js's callers do: 3000 to 768000 Hz and 1 to 32
// channels. A rate of 10^12 asked the resampler for terabytes.
func TestAudioHostClampsRateAndChannels(t *testing.T) {
	host := &fakeAudio{}
	r := newTest(t, func(o *Options) { withAudio(o, host) })
	evalString(t, r, `
globalThis.direct = async () => {
  const H = globalThis.__jr.host, rates = [];
  for (const rate of [1e12, 1, -5, NaN, 44100]) rates.push((await H.audioDecode(new Uint8Array([1, 2, 3]), rate)).rate);
  const a = H.audioRecord();
  H.audioRecordPush(a, new Float32Array(64));
  await H.audioRecordEncode(a, 1e6, 1e12, 128000, 'audio/mp4');
  const b = H.audioRecord();
  await H.audioRecordEncode(b, -3, 0, 128000, 'audio/mp4');
  return rates;
};
0`)
	var rates []int
	callJSON(t, r, "direct", &rates)
	asked, encodes := host.got()
	want := []int{768000, 3000, 3000, 3000, 44100}
	if !equalInts(rates, want) || !equalInts(asked, want) {
		t.Errorf("decode rates: the page got %v, the host was asked %v; want %v", rates, asked, want)
	}
	if len(encodes) != 2 || encodes[0] != (fakeEncode{64, 32, 768000, "audio/mp4"}) || encodes[1] != (fakeEncode{0, 1, 3000, "audio/mp4"}) {
		t.Errorf("encodes %+v, want 64 samples of 32 channels at 768000 Hz, then none of 1 at 3000", encodes)
	}
}

// TestMediaRecorderStreamsToTheHost: a MediaRecorder hands what it renders
// to the host as it records (each 100 ms capture), so the page's heap
// never holds the recording and stop() has none to gather; the host
// encodes the lot at stop. The recording kept raw float32 stereo in JS
// and copied it all once more at stop, in one task.
func TestMediaRecorderStreamsToTheHost(t *testing.T) {
	host := &fakeAudio{}
	r := newTest(t, func(o *Options) { withAudio(o, host) })
	evalString(t, r, recordJS+"0")
	var seen []string
	done := make(chan struct{})
	held := 0
	go func() {
		defer close(done)
		for i := 0; i < 50 && held == 0; i++ { // while it records
			time.Sleep(20 * time.Millisecond)
			held = recordedHeld(r)
		}
	}()
	callJSON(t, r, "record", &seen, 600)
	<-done
	if held == 0 {
		t.Error("the host held nothing while the recorder recorded: the page kept it all to stop")
	}
	want := []string{"start", "dataavailable 3", "stop inactive"}
	if strings.Join(seen, "|") != strings.Join(want, "|") {
		t.Errorf("the recorder's events %q, want %q", seen, want)
	}
	_, encodes := host.got()
	// 600 ms of stereo at 48 kHz, give or take the timers' lateness
	if len(encodes) != 1 || encodes[0].ch != 2 || encodes[0].rate != 48000 || encodes[0].samples < 2*48000*55/100 || encodes[0].samples > 2*48000*90/100 {
		t.Errorf("encodes %+v, want one of about 600 ms of stereo at 48 kHz", encodes)
	}
	if n := recordedHeld(r); n != 0 {
		t.Errorf("the host still holds %d samples after the recording was encoded", n)
	}
}

// TestMediaRecorderStopsAtTheCap: the host holds what the page's
// recorders record up to maxRecordSamples. A recording reaching it stops
// there, as a recorder does on a fatal error: error, then dataavailable
// with the file so far, then stop. A long recording grew the page's heap
// without end.
func TestMediaRecorderStopsAtTheCap(t *testing.T) {
	host := &fakeAudio{}
	setRecordCap(t, 2*4800) // 100 ms of stereo at 48 kHz
	r := newTest(t, func(o *Options) { withAudio(o, host) })
	evalString(t, r, recordJS+"0")
	var seen []string
	callJSON(t, r, "record", &seen, 0) // only the cap stops it
	want := []string{"start", "error UnknownError: MediaRecorder: the recording is longer than this page records", "dataavailable 3", "stop inactive"}
	if strings.Join(seen, "|") != strings.Join(want, "|") {
		t.Errorf("the recorder's events %q, want %q", seen, want)
	}
	if _, encodes := host.got(); len(encodes) != 1 || encodes[0].samples != 2*4800 {
		t.Errorf("encodes %+v, want one of the cap's 9600 samples", encodes)
	}
	if n := recordedHeld(r); n != 0 {
		t.Errorf("the host still holds %d samples", n)
	}
}

// TestAudioClockTimerFarOff: a source stopping 3,000,000 s from now (past
// setTimeout's 2^31-1 ms) arms its context's timer for that far ahead at
// most. A delay past it was setTimeout's 0, and the timer re-armed at once,
// over and over, pinning a core.
func TestAudioClockTimerFarOff(t *testing.T) {
	r := newTest(t)
	evalString(t, r, `
globalThis.far = () => {
  const ctx = globalThis.farCtx = new AudioContext();
  const osc = ctx.createOscillator();
  osc.connect(ctx.destination);
  osc.start(); osc.stop(3e6);
  return new Promise((resolve) => setTimeout(() => resolve(ctx.state), 400));
};
0`)
	before := timersFired(r)
	var state string
	callJSON(t, r, "far", &state)
	if n := timersFired(r) - before; n > 20 {
		t.Errorf("%d timers fired in the 400 ms after a source's far-off stop, want a few", n)
	}
	if state != "running" {
		t.Errorf("the context is %q", state)
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
