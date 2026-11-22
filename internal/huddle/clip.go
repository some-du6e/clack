package huddle

import (
	"context"
	"fmt"
	"sync"

	opus "github.com/kazzmir/opus-go/opus"

	"github.com/some-du6e/clack/internal/huddle/avperm"
)

// Voice clips (internal/clip) record through the huddle's microphone path:
// the same permission prompt (avperm), the microphone Audio & video picked
// (SelectAudioDevices), the same mic chain (apm.go), and libopus on the
// codec goroutine (codecstack.go).
//
// Slack's clip recorder opens its microphone with createStreamManager
// (boot-shared 3xSY): StreamManager (KwNj) with noiseSuppression: true,
// autoGainControl: prefs.audio.enableAGC and echoCancellation left at
// false, the preferred microphone's id and audioOnly. ClipMicOptions.AGC is
// that pref; noise suppression is always on.

// HasMicrophone reports whether this build can open a microphone at all:
// miniaudio needs cgo (audio_malgo.go). It is Slack's !!window.MediaRecorder
// for the clip buttons (boot-shared Qre6 hP).
func HasMicrophone() bool { return openDefaultDevice != nil }

// ClipMicOptions configure OpenClipMic.
type ClipMicOptions struct {
	// AGC is prefs.audio.enableAGC (Preferences > Audio & video, "Enable
	// automatic gain control"; on by default).
	AGC bool
	// Device replaces the sound card (tests, the demo): no permission
	// prompt, as Options.Device does for a session.
	Device Device
	Logf   func(string, ...any)
}

// ClipMic is a microphone open for a voice clip.
type ClipMic struct {
	dev    Device
	apm    *micProcessor
	mu     sync.Mutex
	buf    []int16
	closed bool
	frames chan []int16
	done   chan struct{}
	drops  int
}

// clipQueue is how many 10 ms frames may wait for the clip's goroutine
// (the encoder runs there) before capture drops one: 1.28 s.
const clipQueue = 128

// OpenClipMic asks for microphone access (the OS prompt on macOS), opens
// the microphone and delivers its PCM (mono, 48 kHz) to capture in 10 ms
// frames after the mic chain. It blocks until the OS answered and the
// device opened, or ctx ended. capture runs on one goroutine of its own;
// it gets every frame captured before Close returns.
//
// A refusal comes back as its MicStatus (MicDenied, MicRestricted,
// MicNoUsage) with an error; a device that wouldn't open is MicFailed.
func OpenClipMic(ctx context.Context, o ClipMicOptions, capture func(pcm []int16)) (*ClipMic, MicStatus, error) {
	logf := o.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}
	dev := o.Device
	if dev == nil {
		var st avperm.Status
		select {
		case st = <-avperm.Request(avperm.Mic):
		case <-ctx.Done():
			return nil, MicStarting, ctx.Err()
		}
		if m := micFromPerm(st); m != MicOK {
			return nil, m, fmt.Errorf("huddle: microphone %s", m)
		}
		if openDefaultDevice == nil {
			return nil, MicFailed, ErrNoAudio
		}
		d, err := openDefaultDevice(logf)
		if err != nil {
			return nil, MicFailed, err
		}
		dev = d
	}
	m := &ClipMic{dev: dev, apm: newMicProcessor(o.AGC, true), frames: make(chan []int16, clipQueue), done: make(chan struct{})}
	go m.run(capture)
	if err := dev.Start(m.write, nil); err != nil {
		dev.Close()
		m.finish()
		return nil, MicFailed, err
	}
	if ctx.Err() != nil {
		m.Close()
		return nil, MicStarting, ctx.Err()
	}
	return m, MicOK, nil
}

// write is the device's capture callback: whole 10 ms frames go to the
// clip's goroutine.
func (m *ClipMic) write(pcm []int16) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return
	}
	m.buf = append(m.buf, pcm...)
	for len(m.buf) >= apmFrame {
		f := make([]int16, apmFrame)
		copy(f, m.buf)
		m.buf = append(m.buf[:0], m.buf[apmFrame:]...)
		select {
		case m.frames <- f:
		default:
			m.drops++
		}
	}
}

func (m *ClipMic) run(capture func([]int16)) {
	defer close(m.done)
	for f := range m.frames {
		m.apm.process(f)
		if capture != nil {
			capture(f)
		}
	}
}

// finish stops queueing frames and lets run drain the queue.
func (m *ClipMic) finish() {
	m.mu.Lock()
	if !m.closed {
		m.closed = true
		close(m.frames)
	}
	m.mu.Unlock()
}

// Close stops the microphone and waits until capture has had every frame
// captured before it.
func (m *ClipMic) Close() {
	m.dev.Close()
	m.finish()
	<-m.done
}

// Dropped is how many 10 ms frames capture fell behind on and lost.
func (m *ClipMic) Dropped() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.drops
}

// Opus applications (opus_encoder_create's application argument).
const (
	OpusApplicationVoIP  = opus.ApplicationVoIP
	OpusApplicationAudio = opus.ApplicationAudio
)

// OpusEncoder is libopus's 48 kHz encoder with every call on the codec
// goroutine (codecstack.go), for voice clips.
type OpusEncoder struct {
	e             *opus.Encoder
	channels, app int
	bps           int
	vbr           bool
}

// NewOpusEncoder makes a 48 kHz encoder for channels (1 or 2) with
// OPUS_SET_BITRATE(bps) and OPUS_SET_VBR(vbr).
func NewOpusEncoder(channels, application, bps int, vbr bool) (*OpusEncoder, error) {
	o := &OpusEncoder{channels: channels, app: application, bps: bps, vbr: vbr}
	if err := o.reset(); err != nil {
		return nil, err
	}
	return o, nil
}

func (o *OpusEncoder) reset() (err error) {
	codecCall(func() {
		var e *opus.Encoder
		if e, err = opus.NewEncoder(sampleRate, o.channels, o.app); err != nil {
			return
		}
		if err = e.SetBitrate(o.bps); err != nil {
			return
		}
		if err = e.SetVBR(o.vbr); err != nil {
			return
		}
		o.e = e
	})
	return err
}

// Encode encodes one frame (frame samples per channel, interleaved) into
// out's capacity and returns the packet.
func (o *OpusEncoder) Encode(pcm []int16, frame int, out []byte) (pkt []byte, err error) {
	// libopus's assertions abort (a Go panic in the transpiled build): the
	// frame is lost and a fresh encoder takes the next one
	defer func() {
		if r := recover(); r != nil {
			pkt, err = nil, fmt.Errorf("opus encoder: %v", r)
			_ = o.reset()
		}
	}()
	var n int
	codecCall(func() { n, err = o.e.Encode(pcm, frame, out[:cap(out)]) })
	if err != nil {
		return nil, err
	}
	return out[:n], nil
}

// Close frees the encoder.
func (o *OpusEncoder) Close() {
	if o.e != nil {
		e := o.e
		o.e = nil
		codecCall(func() { _ = e.Close() })
	}
}

// OpusDecoder is libopus's 48 kHz decoder on the codec goroutine (a
// clip's playback, tests).
type OpusDecoder struct {
	d        *opus.Decoder
	channels int
	pcm      []int16
}

// NewOpusDecoder makes a 48 kHz decoder for channels (1 or 2).
func NewOpusDecoder(channels int) (dec *OpusDecoder, err error) {
	codecCall(func() {
		var d *opus.Decoder
		if d, err = opus.NewDecoder(sampleRate, channels); err != nil {
			return
		}
		dec = &OpusDecoder{d: d, channels: channels, pcm: make([]int16, 120*sampleRate/1000*channels)}
	})
	return dec, err
}

// Decode returns one packet's PCM, interleaved; it's valid until the next
// call.
func (o *OpusDecoder) Decode(pkt []byte) (pcm []int16, err error) {
	defer func() {
		if r := recover(); r != nil {
			pcm, err = nil, fmt.Errorf("opus decoder: %v", r)
		}
	}()
	var n int
	codecCall(func() { n, err = o.d.Decode(pkt, o.pcm, len(o.pcm)/o.channels, false) })
	if err != nil {
		return nil, err
	}
	return o.pcm[:n*o.channels], nil
}

// Close frees the decoder.
func (o *OpusDecoder) Close() {
	if o.d != nil {
		d := o.d
		o.d = nil
		codecCall(func() { _ = d.Close() })
	}
}
