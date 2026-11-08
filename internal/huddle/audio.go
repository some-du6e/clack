package huddle

import (
	"math"
	"sync"
	"time"

	opus "github.com/kazzmir/opus-go/opus"
)

// Audio runs at Opus's native 48 kHz: the microphone is mono, the huddle's
// mixed audio from Chime is decoded and played as stereo.
const (
	sampleRate    = 48000
	frameSamples  = sampleRate / 50 // 20 ms
	playChannels  = 2
	maxPlayBuffer = sampleRate * playChannels / 4 // 250 ms
)

// Device is the sound card. Start begins delivering microphone PCM (mono,
// 48 kHz) to capture, in any chunk size, and pulls playback PCM (stereo
// interleaved, 48 kHz) from playback. Either side may be unavailable.
type Device interface {
	Start(capture func(pcm []int16), playback func(out []int16)) error
	Close()
}

// openDefaultDevice opens the system's default microphone and speakers
// (miniaudio when built with cgo; see audio_malgo.go).
var openDefaultDevice func(logf func(string, ...any)) (Device, error)

// toneDevice replaces the microphone with a synthetic tone (for testing on
// machines without a microphone) and discards playback.
type toneDevice struct {
	hz   float64
	stop chan struct{}
	once sync.Once
}

// NewToneDevice is a Device whose "microphone" is a hz sine tone that
// pulses on and off every second (so the speaking indicator moves). The
// playback side is pulled at real time and discarded.
func NewToneDevice(hz float64) Device { return &toneDevice{hz: hz, stop: make(chan struct{})} }

func (d *toneDevice) Start(capture func([]int16), playback func([]int16)) error {
	go func() {
		t := time.NewTicker(20 * time.Millisecond)
		defer t.Stop()
		mic := make([]int16, frameSamples)
		out := make([]int16, frameSamples*playChannels)
		var n int
		for {
			select {
			case <-d.stop:
				return
			case <-t.C:
			}
			for i := range mic {
				ph := float64(n) / sampleRate
				amp := 0.0
				if int(ph)%2 == 0 { // 1 s on, 1 s off
					amp = 0.25
				}
				mic[i] = int16(amp * 32767 * math.Sin(2*math.Pi*d.hz*ph))
				n++
			}
			if capture != nil {
				capture(mic)
			}
			if playback != nil {
				playback(out)
			}
		}
	}()
	return nil
}

func (d *toneDevice) Close() { d.once.Do(func() { close(d.stop) }) }

// nullDevice discards playback at real time and captures silence.
func newNullDevice() Device { return &toneDevice{hz: 0, stop: make(chan struct{})} }

// playBuffer is the jitter/FIFO between the network decoder and the sound
// card callback.
type playBuffer struct {
	mu     sync.Mutex
	buf    []int16
	muted  bool
	primed bool
}

func (p *playBuffer) push(pcm []int16) {
	p.mu.Lock()
	p.buf = append(p.buf, pcm...)
	if over := len(p.buf) - maxPlayBuffer; over > 0 {
		p.buf = append(p.buf[:0], p.buf[over:]...)
	}
	if len(p.buf) >= sampleRate*playChannels/25 { // 40 ms prebuffer
		p.primed = true
	}
	p.mu.Unlock()
}

func (p *playBuffer) pull(out []int16) {
	p.mu.Lock()
	n := 0
	if p.primed {
		n = copy(out, p.buf)
		p.buf = append(p.buf[:0], p.buf[n:]...)
		if len(p.buf) == 0 {
			p.primed = false
		}
	}
	muted := p.muted
	p.mu.Unlock()
	if muted {
		n = 0
	}
	for i := n; i < len(out); i++ {
		out[i] = 0
	}
}

func (p *playBuffer) setMuted(m bool) {
	p.mu.Lock()
	p.muted = m
	p.mu.Unlock()
}

// micFramer cuts captured PCM into 20 ms frames.
type micFramer struct {
	mu   sync.Mutex
	buf  []int16
	out  chan []int16
	drop int
	w    micWatch
}

func newMicFramer() *micFramer { return &micFramer{out: make(chan []int16, 16)} }

func (m *micFramer) write(pcm []int16) {
	m.mu.Lock()
	m.note(pcm, time.Now())
	m.buf = append(m.buf, pcm...)
	for len(m.buf) >= frameSamples {
		f := make([]int16, frameSamples)
		copy(f, m.buf)
		m.buf = append(m.buf[:0], m.buf[frameSamples:]...)
		select {
		case m.out <- f:
		default:
			m.drop++
		}
	}
	m.mu.Unlock()
}

// level is the RMS of pcm on a 0..1 scale.
func level(pcm []int16) float64 {
	if len(pcm) == 0 {
		return 0
	}
	var s float64
	for _, v := range pcm {
		f := float64(v) / 32768
		s += f * f
	}
	return math.Sqrt(s / float64(len(pcm)))
}

type opusEnc struct{ e *opus.Encoder }

func newOpusEnc() (*opusEnc, error) {
	e, err := opus.NewEncoder(sampleRate, 1, opus.ApplicationVoIP)
	if err != nil {
		return nil, err
	}
	_ = e.SetBitrate(32000)
	_ = e.SetComplexity(5)
	return &opusEnc{e: e}, nil
}

func (o *opusEnc) encode(pcm []int16, out []byte) ([]byte, error) {
	n, err := o.e.Encode(pcm, frameSamples, out[:cap(out)])
	if err != nil {
		return nil, err
	}
	return out[:n], nil
}

type opusDec struct {
	d   *opus.Decoder
	pcm []int16
}

func newOpusDec() (*opusDec, error) {
	d, err := opus.NewDecoder(sampleRate, playChannels)
	if err != nil {
		return nil, err
	}
	return &opusDec{d: d, pcm: make([]int16, 120*sampleRate/1000*playChannels)}, nil
}

// decode returns interleaved stereo PCM (valid until the next call). A nil
// packet runs packet-loss concealment for one 20 ms frame.
func (o *opusDec) decode(pkt []byte) ([]int16, error) {
	frame := len(o.pcm) / playChannels
	if pkt == nil {
		frame = frameSamples
	}
	n, err := o.d.Decode(pkt, o.pcm, frame, false)
	if err != nil {
		return nil, err
	}
	return o.pcm[:n*playChannels], nil
}
