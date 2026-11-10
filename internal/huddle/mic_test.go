package huddle

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/huddle/avperm"
)

func TestMicSilentWatch(t *testing.T) {
	m := newMicFramer()
	t0 := time.Unix(1000, 0)
	if s, _ := m.silent(t0.Add(time.Hour)); s {
		t.Fatal("silent before capture started")
	}
	m.started(t0)
	if s, _ := m.silent(t0.Add(time.Second)); s {
		t.Fatal("silent within the grace period")
	}
	if s, why := m.silent(t0.Add(3 * time.Second)); !s || !strings.Contains(why, "no audio") {
		t.Fatalf("no callbacks: %v %q", s, why)
	}
	zeros := make([]int16, 480)
	for i := 0; i < 30; i++ {
		m.mu.Lock()
		m.note(zeros, t0.Add(time.Duration(i)*100*time.Millisecond))
		m.mu.Unlock()
	}
	if s, why := m.silent(t0.Add(3 * time.Second)); !s || !strings.Contains(why, "silence") {
		t.Fatalf("zeros: %v %q", s, why)
	}
	noise := append([]int16(nil), zeros...)
	noise[7] = 3
	m.mu.Lock()
	m.note(noise, t0.Add(3*time.Second))
	m.mu.Unlock()
	if s, _ := m.silent(t0.Add(3100 * time.Millisecond)); s {
		t.Fatal("still silent after a non-zero sample")
	}
}

// micDevice is a sound card whose microphone sends zeros until loud is
// closed, then a tone.
type micDevice struct {
	loud chan struct{}
	stop chan struct{}
}

func (d *micDevice) Start(capture func([]int16), playback func([]int16)) error {
	go func() {
		t := time.NewTicker(10 * time.Millisecond)
		defer t.Stop()
		pcm := make([]int16, 480)
		out := make([]int16, 960)
		for n := 0; ; n++ {
			select {
			case <-d.stop:
				return
			case <-t.C:
			}
			select {
			case <-d.loud:
				for i := range pcm {
					pcm[i] = int16(2000 * ((n + i) % 7))
				}
			default:
			}
			if capture != nil {
				capture(pcm)
			}
			if playback != nil {
				playback(out)
			}
		}
	}()
	return nil
}

func (d *micDevice) Close() {
	select {
	case <-d.stop:
	default:
		close(d.stop)
	}
}

func joinMic(t *testing.T, o Options) *Session {
	t.Helper()
	clip, err := os.ReadFile("testdata/testsrc-320x180.h264")
	if err != nil {
		t.Fatal(err)
	}
	fk := NewFake(clip, t.Errorf)
	t.Cleanup(fk.Close)
	o.Client, o.SelfID, o.Logf = fk.Caller(), fakeSelf, t.Logf
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, err := Join(ctx, o, "C0TEST")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Leave)
	return s
}

func waitMic(t *testing.T, s *Session, want MicStatus, d time.Duration) State {
	t.Helper()
	deadline := time.Now().Add(d)
	for {
		st := s.State()
		if st.Mic == want {
			return st
		}
		if time.Now().After(deadline) {
			t.Fatalf("mic %v (%s), want %v", st.Mic, st.MicErr, want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func answer(st avperm.Status, after time.Duration) func() <-chan avperm.Status {
	return func() <-chan avperm.Status {
		ch := make(chan avperm.Status, 1)
		time.AfterFunc(after, func() { ch <- st })
		return ch
	}
}

func TestMicDeniedStillJoins(t *testing.T) {
	opened := make(chan struct{}, 4)
	s := joinMic(t, Options{MicAccess: answer(avperm.Denied, 200*time.Millisecond),
		OpenAudio: func(func(string, ...any)) (Device, error) {
			opened <- struct{}{}
			return &micDevice{loud: make(chan struct{}), stop: make(chan struct{})}, nil
		}})
	if st := s.State(); st.Mic != MicStarting {
		t.Fatalf("while the prompt is up: %v", st.Mic)
	}
	waitMic(t, s, MicDenied, 5*time.Second)
	if !MicDenied.Problem() || MicStarting.Problem() {
		t.Fatal("Problem")
	}
	// only the speakers opened; the huddle still connects and sends silence
	deadline := time.Now().Add(20 * time.Second)
	for s.State().Phase != Connected || s.audioOutCount() < 10 {
		if time.Now().After(deadline) {
			t.Fatalf("phase %v, audio out %d", s.State().Phase, s.audioOutCount())
		}
		time.Sleep(50 * time.Millisecond)
	}
	if n := len(opened); n != 1 {
		t.Fatalf("opened the sound card %d times, want 1 (speakers only)", n)
	}
}

func TestMicFailedSurfaces(t *testing.T) {
	n := 0
	s := joinMic(t, Options{MicAccess: answer(avperm.Authorized, 0),
		OpenAudio: func(func(string, ...any)) (Device, error) {
			n++
			if n == 1 {
				return &micDevice{loud: make(chan struct{}), stop: make(chan struct{})}, nil
			}
			return nil, errors.New("ma_device_init: no capture device")
		}})
	st := waitMic(t, s, MicFailed, 5*time.Second)
	if !strings.Contains(st.MicErr, "no capture device") {
		t.Fatalf("MicErr %q", st.MicErr)
	}
}

func TestMicZerosThenSound(t *testing.T) {
	dev := &micDevice{loud: make(chan struct{}), stop: make(chan struct{})}
	s := joinMic(t, Options{MicAccess: answer(avperm.Authorized, 0),
		OpenAudio: func(func(string, ...any)) (Device, error) {
			return &micDevice{loud: dev.loud, stop: make(chan struct{})}, nil
		}})
	waitMic(t, s, MicOK, 5*time.Second)
	st := waitMic(t, s, MicSilent, 25*time.Second) // sendAudio checks once connected
	if !strings.Contains(st.MicErr, "silence") {
		t.Fatalf("MicErr %q", st.MicErr)
	}
	close(dev.loud)
	waitMic(t, s, MicOK, 5*time.Second)
}

func (s *Session) audioOutCount() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stats.audioOut
}
