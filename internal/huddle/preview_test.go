package huddle

import (
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/huddle/avperm"
)

func until(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		if cond() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

// The join preview's microphone level lights while you talk, and its
// camera shows your mirrored picture until turned off; nothing joins.
func TestPreviewDevices(t *testing.T) {
	dev := NewToneDevice(440) // 1 s of tone, 1 s of silence
	defer dev.Close()
	p := StartPreview(Options{Device: dev, OpenCamera: func(func(string, ...any)) (Camera, error) {
		return NewTestCamera(320, 180, 15), nil
	}}, true)
	defer p.Close()
	until(t, "the mic", func() bool { st, _ := p.Mic(); return st == MicOK })
	until(t, "talking", p.Talking)
	until(t, "the camera", func() bool { return p.Camera().State == CameraOn && p.Frame().Img != nil })
	if b := p.Frame().Img.Bounds(); b.Dx() != 320 || b.Dy() != 180 {
		t.Fatalf("preview %v", b)
	}
	p.SetCamera(false)
	if p.Camera().State != CameraOff || p.Frame().Img != nil {
		t.Fatalf("camera off: %v, picture %v", p.Camera(), p.Frame().Img != nil)
	}
	until(t, "the tone's silent second", func() bool { return !p.Talking() })
}

// A denied microphone shows as denied (the preview's red mic badge).
func TestPreviewMicDenied(t *testing.T) {
	p := StartPreview(Options{MicAccess: func() <-chan avperm.Status {
		ch := make(chan avperm.Status, 1)
		ch <- avperm.Denied
		return ch
	}}, false)
	defer p.Close()
	until(t, "denied", func() bool { st, _ := p.Mic(); return st == MicDenied })
}
