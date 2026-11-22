package clip

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/huddle"
)

// State is where a recording is (useAudioWaveformRecorder's state and the
// StreamManager events behind it).
type State int

const (
	// Opening: the popover is up and the microphone is opening (the OS
	// prompt can sit here). Nothing records.
	Opening State = iota
	// Starting: microphonestart fired. The start sound plays, and
	// StartDelay later startAudioRecording begins.
	Starting
	// Recording: recordingstart fired; the timer runs from 0.
	Recording
	// Stopped: recordingend made the file (File).
	Stopped
	// Failed: microphoneerror (MicrophoneAccessErrorDialog). Mic says why.
	Failed
	// Cancelled: onCancel, or the popover closed.
	Cancelled
)

func (s State) String() string {
	return [...]string{"opening", "starting", "recording", "stopped", "failed", "cancelled"}[s]
}

// File is the clip useAudioWaveformRecorder hands onFileAttach: File([blob],
// fileName, {type: blob.type}) plus the fields it sets on it.
type File struct {
	Name    string // FileName
	Mime    string // the blob's type, Mime
	Subtype string // Subtype
	Data    []byte
	// Duration is the timer's seconds at the stop (file.duration = N, the
	// last 100 ms tick's).
	Duration float64
	// WaveSamples is audio_wave_samples: the strip's 24 dots × 100 (-100
	// for a dot not drawn yet).
	WaveSamples []float64
}

// Options configure a recording.
type Options struct {
	// AGC is prefs.audio.enableAGC (createStreamManager's autoGainControl).
	AGC bool
	// Device replaces the microphone (tests, CLACK_CLIP_TONE): no
	// permission prompt.
	Device huddle.Device
	// PlayStart plays StartSound (Ofe8 ZH(pop_v2)); nil plays nothing.
	PlayStart func()
	// OnChange runs (on any goroutine) whenever the state, the timer or
	// the waveform changed: the UI redraws.
	OnChange func()
	// Now is the clock (tests); nil is time.Now.
	Now func() time.Time
	// Channels is 1 (the microphone track is mono).
	Channels int
	// BitRate is audioBitsPerSecond (0: BitRate).
	BitRate int
	// TimeLimit (0: TimeLimit) stops and attaches the recording.
	TimeLimit time.Duration
}

// Recorder is one recording: the popover's StreamManager and
// useAudioWaveformRecorder.
type Recorder struct {
	o    Options
	wave *Waveform

	mu       sync.Mutex
	state    State
	mic      *huddle.ClipMic
	micSt    huddle.MicStatus
	micErr   error
	started  time.Time     // recordingstart
	elapsed  time.Duration // the timer, as of its last 100 ms tick
	file     File
	stopping bool
	cancel   context.CancelFunc
	tick     chan struct{} // closed to stop the ticker

	encMu sync.Mutex
	enc   *Encoder
}

// Start opens the microphone and starts a recording, as the popover's
// mount does (StreamManager starts the microphone; microphonestart plays
// the start sound, waits StartDelay and starts recording). It returns at
// once.
func Start(o Options) *Recorder {
	if o.Channels == 0 {
		o.Channels = 1
	}
	if o.BitRate == 0 {
		o.BitRate = BitRate
	}
	if o.TimeLimit == 0 {
		o.TimeLimit = TimeLimit
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := &Recorder{o: o, wave: NewWaveform(), cancel: cancel}
	go r.open(ctx)
	return r
}

func (r *Recorder) now() time.Time {
	if r.o.Now != nil {
		return r.o.Now()
	}
	return time.Now()
}

func (r *Recorder) changed() {
	if r.o.OnChange != nil {
		r.o.OnChange()
	}
}

func (r *Recorder) open(ctx context.Context) {
	mic, st, err := huddle.OpenClipMic(ctx, huddle.ClipMicOptions{AGC: r.o.AGC, Device: r.o.Device, Logf: log.Printf}, r.capture)
	// the encoder before the lock: NewEncoder waits for the huddle's codec
	// goroutine (codecstack.go), which a huddle's audio keeps busy, and
	// State, Dots and Elapsed (every frame of the popover) take r.mu
	var enc *Encoder
	var eerr error
	if err == nil {
		enc, eerr = NewEncoder(r.o.Channels, r.o.BitRate)
	}
	r.mu.Lock()
	if r.state != Opening { // cancelled while the OS asked
		r.mu.Unlock()
		if mic != nil {
			mic.Close()
		}
		if enc != nil {
			enc.Close()
		}
		return
	}
	if err != nil {
		// microphoneerror: "AUDIO-RECORDER", "Microphone failed"
		r.state, r.micSt, r.micErr = Failed, st, err
		r.mu.Unlock()
		log.Printf("clip: microphone failed: %s", st)
		r.changed()
		return
	}
	if eerr != nil {
		r.state, r.micSt, r.micErr = Failed, huddle.MicFailed, eerr
		r.mu.Unlock()
		mic.Close()
		log.Printf("clip: encoder: %v", eerr)
		r.changed()
		return
	}
	// the encoder is the recorder's before the state says so: a Cancel that
	// sees Starting has an encoder to free
	r.encMu.Lock()
	r.enc = enc
	r.encMu.Unlock()
	r.mic, r.state = mic, Starting // microphonestart
	r.mu.Unlock()
	r.changed()
	if r.o.PlayStart != nil {
		r.o.PlayStart()
	}
	select {
	case <-time.After(StartDelay):
	case <-ctx.Done():
		return
	}
	r.mu.Lock()
	if r.state != Starting {
		r.mu.Unlock()
		return
	}
	// startAudioRecording → recordingstart: the timer from 0
	r.state, r.started, r.elapsed = Recording, r.now(), 0
	r.tick = make(chan struct{})
	tick := r.tick
	r.mu.Unlock()
	r.changed()
	go r.ticker(tick)
}

// capture is the microphone's frames (its own goroutine): the analyser
// always hears them, MediaRecorder only once it started.
func (r *Recorder) capture(pcm []int16) {
	r.wave.Write(pcm)
	r.mu.Lock()
	rec := r.state == Recording || r.stopping
	r.mu.Unlock()
	if !rec {
		return
	}
	r.encMu.Lock()
	if r.enc != nil {
		_ = r.enc.Write(pcm)
	}
	r.encMu.Unlock()
}

// ticker is the waveform's 20 ms reads and the timer's 100 ms ticks (the
// hook's setInterval(100): duration = (now - start) / 1000, and at the
// limit the recording stops and attaches itself).
func (r *Recorder) ticker(stop chan struct{}) {
	t := time.NewTicker(WaveReadMs * time.Millisecond)
	defer t.Stop()
	n := 0
	for {
		select {
		case <-stop:
			return
		case <-t.C:
		}
		dot := r.wave.Read()
		n++
		if n%5 != 0 {
			if dot {
				r.changed()
			}
			continue
		}
		r.mu.Lock()
		if r.state != Recording {
			r.mu.Unlock()
			return
		}
		r.elapsed = r.now().Sub(r.started)
		over := r.elapsed >= r.o.TimeLimit
		r.mu.Unlock()
		r.changed()
		if over {
			go r.Stop()
			return
		}
	}
}

// State is the recording's state now.
func (r *Recorder) State() State {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.state
}

// Mic is why the microphone failed (Failed).
func (r *Recorder) Mic() (huddle.MicStatus, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.micSt, r.micErr
}

// Elapsed is the timer (the hook's duration): 0 until recording starts,
// then updated every 100 ms.
func (r *Recorder) Elapsed() time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.elapsed
}

// Warning is warningHighlight: no file yet and the limit 10 s or less
// away. The timer and the waveform turn red.
func (r *Recorder) Warning() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.state != Stopped && r.o.TimeLimit-r.elapsed <= TimeWarning
}

// Dots is the waveform strip (Waveform.Dots).
func (r *Recorder) Dots() []float64 { return r.wave.Dots() }

// Waveform is the recorder's analyser (tests drive its reads).
func (r *Recorder) Waveform() *Waveform { return r.wave }

// Stop is onAttach before the file exists: stopAudioRecording, then
// recordingend makes the File and the state is Stopped. Before recording
// started it does nothing (stopAudioRecording with nothing recording), and
// so does a second Stop, or one after Cancel. It blocks until the file is
// made; call it off the UI goroutine.
//
// Stopped always has a File, as Slack's recordingend always does: the
// Blob of every chunk MediaRecorder delivered, which is empty when no
// audio reached the encoder (a stop as recording began, a microphone that
// delivered nothing), and the hook attaches that too (StreamManager's
// recordingend, useAudioWaveformRecorder's V: `if (a)` holds for an empty
// Blob). A frame libopus failed on is only lost, as in Chromium.
func (r *Recorder) Stop() {
	r.mu.Lock()
	if r.state != Recording || r.stopping {
		r.mu.Unlock()
		return
	}
	r.stopping = true
	// Stop owns the microphone and the ticker now: a Cancel meanwhile, or
	// after, finds neither to close again
	mic, tick := r.mic, r.tick
	r.mic, r.tick = nil, nil
	dur := r.elapsed
	r.mu.Unlock()
	if tick != nil {
		close(tick)
	}
	if mic != nil {
		mic.Close() // every frame captured so far reaches the encoder
		if n := mic.Dropped(); n > 0 {
			log.Printf("clip: the encoder fell behind the microphone: %d of its 10 ms frames lost", n)
		}
	}
	r.encMu.Lock()
	enc := r.enc
	r.enc = nil
	r.encMu.Unlock()
	data := []byte{}
	if enc != nil {
		d, err := enc.Finish()
		if err != nil {
			log.Printf("clip: no packet encoded: %v", err)
		}
		if d != nil {
			data = d
		}
	}
	f := File{Name: FileName, Mime: Mime, Subtype: Subtype, Data: data, Duration: dur.Seconds(), WaveSamples: r.wave.Samples()}
	r.mu.Lock()
	r.stopping = false
	if r.state == Recording {
		r.state, r.file = Stopped, f
	}
	r.mu.Unlock()
	log.Printf("clip: recorded %.1f s, %d bytes", f.Duration, len(f.Data))
	r.changed()
}

// File is the recording once Stopped.
func (r *Recorder) File() (File, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.file, r.state == Stopped
}

// Cancel is onCancel (and the popover's unmount, w.stop()): the recording
// and the microphone stop and nothing is kept. It returns at once, since
// the popover's onClose calls it on the UI goroutine: the state is
// Cancelled when it returns, and the microphone and the encoder close on
// a goroutine of their own (a sound card's Close can take a while, and
// the microphone's Close waits for its queued frames). Safe to call more
// than once, on any goroutine.
func (r *Recorder) Cancel() {
	r.cancel()
	r.mu.Lock()
	switch r.state {
	case Stopped, Cancelled, Failed:
		// the recording is over: what it held is released (Stop, open's
		// failure) or being released (an earlier Cancel)
		r.mu.Unlock()
		return
	}
	stopping := r.stopping
	r.state = Cancelled
	mic, tick := r.mic, r.tick
	r.mic, r.tick = nil, nil
	r.mu.Unlock()
	if !stopping { // else Stop owns the microphone and the encoder now
		if tick != nil {
			close(tick)
		}
		go r.release(mic)
	}
	r.changed()
}

// release closes a cancelled recording's microphone, then its encoder:
// capture can be in the encoder until the microphone has closed.
func (r *Recorder) release(mic *huddle.ClipMic) {
	if mic != nil {
		mic.Close()
		if n := mic.Dropped(); n > 0 {
			log.Printf("clip: the encoder fell behind the microphone: %d of its 10 ms frames lost", n)
		}
	}
	r.encMu.Lock()
	if r.enc != nil {
		r.enc.Close()
		r.enc = nil
	}
	r.encMu.Unlock()
}
