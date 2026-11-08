package huddle

import (
	"errors"
	"os"
	"strconv"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/huddle/avperm"
)

// MicStatus is whether your microphone reaches the huddle. Anything but
// MicOK and MicStarting is a problem the UI shows (State.Mic, State.MicErr).
type MicStatus int

const (
	MicOK         MicStatus = iota
	MicStarting             // asking the OS for access, or opening the device
	MicDenied               // the OS said no (macOS System Settings › Privacy)
	MicRestricted           // blocked by policy (MDM, parental controls)
	MicNoUsage              // the app bundle can't ask (no usage description)
	MicFailed               // the device didn't open: MicErr says why
	MicSilent               // capture runs but delivers only zeros (or nothing)
)

func (m MicStatus) String() string {
	return [...]string{"ok", "starting", "denied", "restricted", "no usage description", "failed", "silent"}[m]
}

// Problem reports whether m is something to tell the user about.
func (m MicStatus) Problem() bool { return m != MicOK && m != MicStarting }

// micSilentAfter is how long capture may deliver only exact zeros (or no
// callbacks at all) before the mic counts as silent. A real microphone's
// noise floor is never exactly zero for that long; macOS delivers zeros
// when TCC denies capture without telling the app.
const micSilentAfter = 2500 * time.Millisecond

func micFromPerm(st avperm.Status) MicStatus {
	switch st {
	case avperm.Authorized:
		return MicOK
	case avperm.Denied:
		return MicDenied
	case avperm.Restricted:
		return MicRestricted
	case avperm.NoUsageString:
		return MicNoUsage
	}
	return MicStarting
}

func (s *Session) setMic(m MicStatus, err string) {
	s.mu.Lock()
	changed := s.micStatus != m || s.micErr != err
	s.micStatus, s.micErr = m, err
	s.mu.Unlock()
	if changed {
		if m.Problem() {
			s.o.Logf("huddle: microphone %s %s", m, err)
		}
		s.changed()
	}
}

// openDevice starts the speakers at once and the microphone in the
// background: on macOS it first waits for the OS's permission prompt.
func (s *Session) openDevice() {
	if dev := s.o.Device; dev != nil { // tests, the demo: one device, no prompt
		s.micStatus = MicOK
		if err := dev.Start(s.mic.write, s.play.pull); err != nil {
			s.micStatus, s.micErr = MicFailed, err.Error()
			s.o.Logf("huddle: audio device: %v (no audio IO)", err)
			dev.Close()
			dev = newNullDevice()
			_ = dev.Start(nil, s.play.pull)
		}
		s.mic.started(time.Now())
		s.dev = dev
		return
	}
	if hz, _ := strconv.ParseFloat(os.Getenv("CLACK_HUDDLE_TONE"), 64); hz > 0 {
		dev := NewToneDevice(hz)
		_ = dev.Start(s.mic.write, s.play.pull)
		s.dev, s.micStatus = dev, MicOK
		return
	}
	s.dev = s.openSpeakers()
	s.micStatus = MicStarting
	go s.openMic()
}

func (s *Session) openAudio() func(func(string, ...any)) (Device, error) {
	if s.o.OpenAudio != nil {
		return s.o.OpenAudio
	}
	return openDefaultDevice // nil without cgo
}

func (s *Session) openSpeakers() Device {
	if open := s.openAudio(); open != nil {
		d, err := open(s.o.Logf)
		if err == nil {
			if err = d.Start(nil, s.play.pull); err == nil {
				return d
			}
			d.Close()
		}
		s.o.Logf("huddle: speakers: %v (you won't hear the huddle)", err)
	}
	d := newNullDevice() // drains playback at real time
	_ = d.Start(nil, s.play.pull)
	return d
}

func (s *Session) micAccess() <-chan avperm.Status {
	if s.o.MicAccess != nil {
		return s.o.MicAccess()
	}
	return avperm.Request(avperm.Mic)
}

// openMic asks for access, then opens the default microphone. It runs on
// its own goroutine (the OS prompt can sit there for minutes).
func (s *Session) openMic() {
	s.setMic(MicStarting, "")
	var st avperm.Status
	select {
	case st = <-s.micAccess():
	case <-s.ctx.Done():
		return
	}
	if m := micFromPerm(st); m != MicOK {
		s.setMic(m, "")
		return
	}
	open := s.openAudio()
	if open == nil {
		s.setMic(MicFailed, "this build has no audio support")
		return
	}
	dev, err := open(s.o.Logf)
	if err == nil {
		if err = dev.Start(s.mic.write, nil); err != nil {
			dev.Close()
		}
	}
	if err == nil && dev == nil {
		err = errors.New("no microphone")
	}
	if err != nil {
		s.setMic(MicFailed, err.Error())
		return
	}
	s.mu.Lock()
	if s.ended {
		s.mu.Unlock()
		dev.Close()
		return
	}
	old := s.micDev
	s.micDev = dev
	s.mu.Unlock()
	if old != nil {
		old.Close()
	}
	s.mic.started(time.Now())
	s.setMic(MicOK, "")
}

// RetryMic asks for the microphone again (after the user changed System
// Settings, or plugged one in). It returns at once.
func (s *Session) RetryMic() {
	if s.o.Device != nil || os.Getenv("CLACK_HUDDLE_TONE") != "" {
		return
	}
	go s.openMic()
}

// checkMic flags a microphone that delivers only zeros, or nothing, and
// clears the flag once sound arrives. sendAudio calls it every frame.
func (s *Session) checkMic(now time.Time) {
	s.mu.Lock()
	cur := s.micStatus
	s.mu.Unlock()
	if cur != MicOK && cur != MicSilent {
		return
	}
	silent, why := s.mic.silent(now)
	switch {
	case silent && cur == MicOK:
		s.setMic(MicSilent, why)
	case !silent && cur == MicSilent:
		s.setMic(MicOK, "")
	}
}

// micWatch is micFramer's health record. Callers hold micFramer.mu.
type micWatch struct {
	since     time.Time // capture started (zero = not watching)
	lastData  time.Time // last callback with samples
	lastSound time.Time // last callback with a non-zero sample
}

func (m *micFramer) started(now time.Time) {
	m.mu.Lock()
	m.w = micWatch{since: now}
	m.mu.Unlock()
}

func (m *micFramer) note(pcm []int16, now time.Time) {
	if m.w.since.IsZero() || len(pcm) == 0 {
		return
	}
	m.w.lastData = now
	for _, v := range pcm {
		if v != 0 {
			m.w.lastSound = now
			return
		}
	}
}

func (m *micFramer) silent(now time.Time) (bool, string) {
	m.mu.Lock()
	w := m.w
	m.mu.Unlock()
	if w.since.IsZero() || now.Sub(w.since) < micSilentAfter {
		return false, ""
	}
	if w.lastData.IsZero() || now.Sub(w.lastData) >= micSilentAfter {
		return true, "no audio from the microphone"
	}
	ref := w.lastSound
	if ref.IsZero() {
		ref = w.since
	}
	if now.Sub(ref) >= micSilentAfter {
		return true, "the microphone sends only silence"
	}
	return false, ""
}
