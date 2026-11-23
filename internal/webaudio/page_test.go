package webaudio

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/clip"
	"github.com/some-du6e/clack/internal/jsrt"
	"github.com/some-du6e/clack/internal/media/goaudio"
	"github.com/some-du6e/clack/internal/media/mediatest"
)

// TestWebAudioLikeChromium: Web Audio's decodeAudioData and MediaRecorder
// on the plugins' page (jsrt's dom.js), as Chromium has them, over this
// host. (Here, not in jsrt's tests: this host's decoders and encoder bring
// cgo that jsrt's own workflows don't build.)
//
//   - decodeAudioData decodes the whole file into an AudioBuffer at the
//     context's rate (48 kHz by default, or the rate the context was made
//     with), every channel the file has, and detaches the ArrayBuffer; its
//     success callback runs too. A file that isn't audio rejects with
//     EncodingError ("Unable to decode audio data") and runs the error
//     callback; a typed array instead of an ArrayBuffer is a TypeError.
//   - An AudioContext runs at once (Electron's autoplay policy), its clock
//     moving on in 128-frame render quanta; close() closes it.
//   - MediaRecorder supports audio/mp4 (Opus in MP4) and nothing Clack
//     can't encode; a type it doesn't support throws NotSupportedError.
//   - Taut's CustomNameRecording converts a WAV to m4a with its own code
//     (y(): a buffer source into a MediaStreamAudioDestinationNode, a
//     128 kbps audio/mp4 MediaRecorder started first, the source started
//     50 ms later and the recorder stopped when it ends): start, one
//     dataavailable, stop; the blob is Opus in MP4, stereo, as long as the
//     file plus the lead-in, with its tones where they were; it took as
//     long as the audio plays, as a real-time recorder does.
func TestWebAudioLikeChromium(t *testing.T) {
	r, err := jsrt.New(jsrt.Options{Audio: Host{}, Log: func(level, msg string) { t.Logf("js %s: %s", level, msg) }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	call := func(fn string, v any, args ...any) {
		t.Helper()
		got, err := r.Call(ctx, fn, args...)
		if err != nil {
			t.Fatalf("%s: %v", fn, err)
		}
		if err := json.Unmarshal(got, v); err != nil {
			t.Fatalf("%s: %s: %v", fn, got, err)
		}
	}
	if err := r.Do(func(js *jsrt.Ctx) error {
		_, err := js.Eval("test.js", `
const ab = (u8) => u8.buffer.slice(u8.byteOffset, u8.byteOffset + u8.byteLength);
globalThis.decode = async (u8, rate) => {
  const ctx = rate ? new AudioContext({ sampleRate: rate }) : new AudioContext();
  const buf = ab(u8);
  let ok = 0, failed = 0;
  try {
    const b = await ctx.decodeAudioData(buf, () => ok++, () => failed++);
    const d = b.getChannelData(0);
    let peak = 0;
    for (const x of d) peak = Math.max(peak, Math.abs(x));
    return { state: ctx.state, rate: b.sampleRate, channels: b.numberOfChannels, length: b.length, duration: b.duration, peak,
      detached: buf.byteLength === 0, same: b.getChannelData(0) === d, isBuffer: b instanceof AudioBuffer, ok, failed };
  } catch (e) {
    return { error: e.name, message: e.message, ok, failed };
  } finally {
    await ctx.close();
  }
};
globalThis.typed = async (u8) => {
  const ctx = new AudioContext();
  try { await ctx.decodeAudioData(u8); return 'resolved'; } catch (e) { return e.name; } finally { ctx.close(); }
};
globalThis.clock = async () => {
  const ctx = new AudioContext();
  const states = [ctx.state];
  const t0 = ctx.currentTime;
  await new Promise((r) => setTimeout(r, 250));
  const t1 = ctx.currentTime;
  await ctx.close();
  states.push(ctx.state);
  let closedTwice = '';
  try { await ctx.close(); } catch (e) { closedTwice = e.name; }
  return { states, t0, t1, quantum: Math.round(t1 * 48000) % 128, closedTwice };
};
globalThis.types = () => {
  const ask = ['audio/mp4', 'audio/mp4;codecs=opus', 'audio/webm;codecs=opus', 'audio/webm', 'audio/mp4;codecs=mp4a.40.2', 'video/mp4', ''];
  const out = {};
  for (const t of ask) out[t] = MediaRecorder.isTypeSupported(t);
  try { new MediaRecorder(new AudioContext().createMediaStreamDestination().stream, { mimeType: 'audio/webm' }); out.webmCtor = 'constructed'; } catch (e) { out.webmCtor = e.name; }
  return out;
};
// Taut's CustomNameRecording's y() (the Library bundle), verbatim but for its events log
globalThis.convert = async (u8) => {
  const a = new AudioContext();
  const l = await a.decodeAudioData(ab(u8));
  a.close();
  const events = [];
  const t0 = performance.now();
  let e = ["audio/mp4", "audio/webm;codecs=opus", "audio/webm"].find(n => MediaRecorder.isTypeSupported(n));
  if (!e) throw new Error("This browser cannot convert audio");
  let t = new AudioContext({ sampleRate: l.sampleRate });
  let mime = '';
  let blob, startAt = 0, stopAt = 0; // the context's clock when the recorder started and stopped
  try {
    await t.resume();
    let n = t.createBufferSource(); n.buffer = l;
    let s = t.createMediaStreamDestination(); n.connect(s);
    let r = new MediaRecorder(s.stream, { mimeType: e, audioBitsPerSecond: 128e3 }), o = [];
    for (const x of ['start', 'dataavailable', 'stop']) r.addEventListener(x, () => events.push(x));
    n.addEventListener('ended', () => { events.push('ended'); stopAt = t.currentTime; });
    r.addEventListener("dataavailable", ev => o.push(ev.data));
    let i = new Promise(res => r.addEventListener("stop", res, { once: !0 }));
    n.addEventListener("ended", () => r.stop(), { once: !0 });
    r.addEventListener("start", () => n.start(t.currentTime + .05), { once: !0 });
    r.start();
    startAt = t.currentTime;
    mime = r.mimeType;
    await i;
    blob = new Blob(o, { type: e });
  } finally { t.close(); }
  const bytes = new Uint8Array(await blob.arrayBuffer());
  let bin = '';
  for (let i = 0; i < bytes.length; i += 0x8000) bin += String.fromCharCode(...bytes.subarray(i, i + 0x8000));
  return { type: e, blobType: blob.type, mime, events, wall: (performance.now() - t0) / 1000, rate: l.sampleRate, recorded: stopAt - startAt, b64: btoa(bin) };
};
0`)
		return err
	}); err != nil {
		t.Fatal(err)
	}

	type decoded struct {
		State, Error, Message    string
		Rate, Channels, Length   int
		Duration, Peak           float64
		Detached, Same, IsBuffer bool
		OK, Failed               int
	}
	var d decoded
	call("decode", &d, mediatest.TonesMP3, 0)
	if d.Error != "" || !d.IsBuffer || d.Rate != 48000 || d.Channels != 2 || math.Abs(d.Duration-3) > 0.06 || d.Peak < 0.11 || d.Peak > 0.14 {
		t.Errorf("the MP3 (44.1 kHz stereo): %+v, want an AudioBuffer at the context's 48 kHz, 2 channels, 3 s, peak 1/8", d)
	}
	if !d.Detached || !d.Same || d.OK != 1 || d.Failed != 0 || d.State != "running" {
		t.Errorf("the MP3: detached %v, getChannelData the same array %v, callbacks ok %d failed %d, state %q", d.Detached, d.Same, d.OK, d.Failed, d.State)
	}
	d = decoded{}
	call("decode", &d, mediatest.TonesWAV, 22050)
	if d.Error != "" || d.Rate != 22050 || d.Channels != 1 || d.Length < 3*22050-2 || d.Length > 3*22050+2 {
		t.Errorf("the WAV (16 kHz mono) in a 22.05 kHz context: %+v, want 1 channel at 22050, 66150 frames", d)
	}
	d = decoded{}
	call("decode", &d, []byte("not audio, only some words"), 0)
	if d.Error != "EncodingError" || d.Message != "Unable to decode audio data" || d.OK != 0 || d.Failed != 1 {
		t.Errorf("text: %+v, want EncodingError \"Unable to decode audio data\" and the error callback", d)
	}
	var typed string
	call("typed", &typed, mediatest.TonesWAV)
	if typed != "TypeError" {
		t.Errorf("decodeAudioData(a Uint8Array): %q, want TypeError", typed)
	}

	var c struct {
		States      []string
		T0, T1      float64
		Quantum     int
		ClosedTwice string
	}
	call("clock", &c)
	if len(c.States) != 2 || c.States[0] != "running" || c.States[1] != "closed" || c.T1-c.T0 < 0.15 || c.Quantum != 0 || c.ClosedTwice != "InvalidStateError" {
		t.Errorf("the clock: %+v, want running then closed, 250 ms moving it on in 128-frame quanta, a second close() InvalidStateError", c)
	}

	var types map[string]any
	call("types", &types)
	for typ, want := range map[string]any{"audio/mp4": true, "audio/mp4;codecs=opus": true, "audio/webm;codecs=opus": false, "audio/webm": false,
		"audio/mp4;codecs=mp4a.40.2": false, "video/mp4": false, "": true, "webmCtor": "NotSupportedError"} {
		if types[typ] != want {
			t.Errorf("isTypeSupported(%q) = %v, want %v", typ, types[typ], want)
		}
	}

	var cv struct {
		Type, BlobType, Mime, B64 string
		Events                    []string
		Wall, Recorded            float64
		Rate                      int
	}
	call("convert", &cv, mediatest.TonesWAV)
	if cv.Type != "audio/mp4" || cv.BlobType != "audio/mp4" || cv.Mime != "audio/mp4;codecs=opus" {
		t.Errorf("the conversion: type %q, blob %q, recorder %q; want audio/mp4 (Opus)", cv.Type, cv.BlobType, cv.Mime)
	}
	if want := []string{"start", "ended", "dataavailable", "stop"}; len(cv.Events) != len(want) || cv.Events[0] != want[0] || cv.Events[1] != want[1] || cv.Events[2] != want[2] || cv.Events[3] != want[3] {
		t.Errorf("the conversion's events %v, want %v", cv.Events, want)
	}
	if cv.Wall < 2.9 {
		t.Errorf("the conversion took %.2f s: a real-time recorder takes the 3 s the audio plays", cv.Wall)
	}
	b, err := base64.StdEncoding.DecodeString(cv.B64)
	if err != nil {
		t.Fatal(err)
	}
	dur, first, ch, err := recordingOf(b)
	if err != nil {
		t.Fatalf("the converted file (%d bytes) isn't Opus in MP4: %v", len(b), err)
	}
	// the file is the recorder's whole 60 ms packets (Chromium's encoder
	// drops the last part of one), its last packet counted 21 ms (Mp4Muxer's
	// last sample: 1024 frames at 48 kHz): 39 to 99 ms under what the
	// recorder recorded by the context's clock, from start() to the stop()
	// ended's handler made (the 3 s, the lead-in, and how late the timers
	// came, which a busy runner stretches)
	if ch != 2 || cv.Recorded < 3 || dur < cv.Recorded-0.1-0.003 || dur > cv.Recorded-0.039+0.003 {
		t.Errorf("the converted file: %d channels, %.3f s; want stereo, 39 to 99 ms under the %.3f s the recorder recorded (at least the 3 s)", ch, dur, cv.Recorded)
	}
	for s, want := range mediatest.Tones {
		from := int((float64(s) + 0.05 + 0.2) * 48000)
		if to := from + 48000*6/10; to <= len(first) {
			if i, by := mediatest.Loudest(first[from:to], 48000); i != s || by < 4 {
				t.Errorf("the converted file's second %d plays tone %d loudest (%.1fx the next), want %.0f Hz's", s, i, by, want)
			}
		} else {
			t.Errorf("the converted file is %d frames: no second %d", len(first), s)
		}
	}
}

// recordingOf is a MediaRecorder file as Go reads it: Opus in MP4's
// length, and its first channel decoded at 48 kHz with the channel count.
func recordingOf(b []byte) (dur float64, first []float32, channels int, err error) {
	info, err := clip.Probe(b)
	if err != nil {
		return 0, nil, 0, err
	}
	p, err := goaudio.Decode(context.Background(), b, "audio/mp4", 48000)
	if err != nil {
		return 0, nil, 0, err
	}
	return info.Duration().Seconds(), p.Channels[0], len(p.Channels), nil
}
