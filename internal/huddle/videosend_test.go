package huddle

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// firstVideoDirection is the direction attribute of the offer's first
// video m-section (the camera's).
func firstVideoDirection(sdp string) (dir string, ssrc bool) {
	i := strings.Index(sdp, "m=video")
	if i < 0 {
		return "", false
	}
	sec := sdp[i+1:]
	if j := strings.Index(sec, "\nm="); j >= 0 {
		sec = sec[:j]
	}
	for _, d := range []string{"sendrecv", "recvonly", "sendonly", "inactive"} {
		if strings.Contains(sec, "a="+d) {
			dir = d
		}
	}
	return dir, strings.Contains(sec, "a=ssrc:")
}

func waitFor(t *testing.T, d time.Duration, what string, ok func() bool, info func() string) {
	t.Helper()
	deadline := time.Now().Add(d)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s: %s", what, info())
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestFakeCamera turns the test camera on against the fake Chime, checks
// SUBSCRIBE and the SDP advertise it the way the Chime SDK does, that H.264
// arrives and decodes at the fake, that a PLI gets a keyframe, that your
// tile is marked and previewed (mirrored), then turns it off and on again.
func TestFakeCamera(t *testing.T) {
	clip, err := os.ReadFile("testdata/testsrc-320x180.h264")
	if err != nil {
		t.Fatal(err)
	}
	fk := NewFake(clip, t.Errorf)
	defer fk.Close()
	f := fk.f
	f.cam.dropFirst.Store(true) // the first keyframe lost (TestFakeScreenShare): the fake asks for another
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, err := Join(ctx, Options{
		Client: fk.Caller(), SelfID: fakeSelf, Device: NewToneDevice(440), Logf: t.Logf,
		OpenCamera: func(func(string, ...any)) (Camera, error) { return NewTestCamera(320, 180, 15), nil },
	}, "C0TEST")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Leave()
	waitFor(t, 15*time.Second, "media", func() bool { return s.State().Phase == Connected && f.subs.Load() >= 2 },
		func() string { return s.State().Phase.String() })
	if dir, _ := firstVideoDirection(f.lastSub().SDP); dir != "recvonly" {
		t.Errorf("camera m-section before SetCamera: %q, want recvonly", dir)
	}
	subsBefore := f.subs.Load()

	s.SetCamera(true)
	if st := s.State().Camera.State; st != CameraStarting && st != CameraOn {
		t.Errorf("right after SetCamera(true): %s", st)
	}
	c := &f.cam
	info := func() string {
		return s.State().Camera.State.String() + "; fake got " + itoa64(c.pkts.Load()) + " packets, " + itoa64(c.frames.Load()) +
			" pictures (" + itoa64(c.undecodable.Load()) + " undecodable, " + itoa64(c.keyReqs.Load()) + " keyframe requests), " + itoa64(c.keys.Load()) + " keyframes (" + itoa64(c.keysPLI.Load()) + " after PLI); " + s.Stats()
	}
	waitFor(t, 20*time.Second, "camera video at the fake", func() bool {
		return s.State().Camera.State == CameraOn && c.frames.Load() >= 10 && c.keysPLI.Load() >= 1
	}, info)
	t.Logf("on: %s", info())
	if w, h := c.w.Load(), c.h.Load(); w != 320 || h != 180 {
		t.Errorf("fake decoded %dx%d, want 320x180", w, h)
	}
	if m, _ := c.mime.Load().(string); !strings.EqualFold(m, "video/H264") {
		t.Errorf("camera codec %q", m)
	}
	sub := f.lastSub()
	if f.subs.Load() <= subsBefore || sub.Duplex != streamDuplex {
		t.Errorf("SUBSCRIBE duplex %d (want DUPLEX=3)", sub.Duplex)
	}
	if len(sub.Video) != 1 {
		t.Fatalf("SUBSCRIBE video send streams: %+v", sub.Video)
	}
	if v := sub.Video[0]; v.StreamID != 2 || v.GroupID != 2 || v.TrackLabel != "AmazonChimeExpressVideo" || v.AttendeeID != fakeSelfAtt ||
		v.MediaType != mediaVideo || v.Framerate != 15 || v.MaxBitrateKbps != 1500 {
		t.Errorf("SUBSCRIBE video stream: %+v", v)
	}
	if dir, ssrc := firstVideoDirection(sub.SDP); dir != "sendrecv" || !ssrc {
		t.Errorf("camera m-section while on: %q (ssrc lines %v), want sendrecv with an ssrc", dir, ssrc)
	}

	// you: marked as on camera, a mirrored preview, no subscription to yourself
	st := s.State()
	if len(st.Participants) == 0 || !st.Participants[0].Self || !st.Participants[0].Camera {
		t.Errorf("self participant: %+v", st.Participants)
	}
	for _, v := range st.Videos {
		if v.AttendeeID == fakeSelfAtt {
			t.Errorf("subscribed to your own camera: %+v", v)
		}
	}
	var pv VideoFrame
	waitFor(t, 5*time.Second, "self preview", func() bool { pv = s.Frame(SelfVideoKey); return pv.Img != nil && pv.Seq > 1 }, info)
	if b := pv.Img.Bounds(); b.Dx() != 320 || b.Dy() != 180 {
		t.Errorf("preview %v", b)
	}
	// the pattern's white bar is on the left, black on the right: mirrored
	l, r := pv.Img.RGBAAt(2, 30), pv.Img.RGBAAt(317, 30)
	if l.R > 40 || r.R < 220 {
		t.Errorf("preview not mirrored: left %v, right %v", l, r)
	}

	// off: recvonly again, RX, no video stream; the packets stop
	s.SetCamera(false)
	waitFor(t, 10*time.Second, "camera off", func() bool {
		sub := f.lastSub()
		return s.State().Camera.State == CameraOff && sub.Duplex == streamRX && len(sub.Video) == 0
	}, info)
	if dir, _ := firstVideoDirection(f.lastSub().SDP); dir != "recvonly" {
		t.Errorf("camera m-section after SetCamera(false): %q, want recvonly", dir)
	}
	if st := s.State(); st.Participants[0].Camera {
		t.Error("still marked on camera")
	}
	time.Sleep(300 * time.Millisecond)
	n := c.pkts.Load()
	time.Sleep(700 * time.Millisecond)
	if d := c.pkts.Load() - n; d > 0 {
		t.Errorf("%d camera packets after turning it off", d)
	}
	if pv := s.Frame(SelfVideoKey); pv.Img != nil {
		t.Error("preview still showing after the camera went off")
	}

	// and on again (a new sender on the same m-section)
	before := c.frames.Load()
	s.SetCamera(true)
	waitFor(t, 15*time.Second, "camera on again", func() bool {
		return s.State().Camera.State == CameraOn && c.frames.Load() >= before+10 && f.lastSub().Duplex == streamDuplex
	}, info)
	if got := countVideoSections(f.lastSub().SDP); got != 2 {
		t.Errorf("%d video m-sections after camera on/off/on, want 2 (camera + share)", got)
	}
}

func countVideoSections(sdp string) int { return strings.Count(sdp, "m=video") }

func itoa64(n int64) string { return strconv.FormatInt(n, 10) }

// TestCameraUnsupported: without a backend (and no test camera) the
// camera reports unsupported instead of failing silently.
func TestCameraUnsupported(t *testing.T) {
	if openDefaultCamera != nil || testCamEnv() {
		t.Skip("this build has a camera backend")
	}
	clip, err := os.ReadFile("testdata/testsrc-320x180.h264")
	if err != nil {
		t.Fatal(err)
	}
	fk := NewFake(clip, t.Errorf)
	defer fk.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, err := Join(ctx, Options{Client: fk.Caller(), SelfID: fakeSelf, Device: NewToneDevice(440), Logf: t.Logf}, "C0TEST")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Leave()
	s.SetCamera(true)
	waitFor(t, 5*time.Second, "unsupported", func() bool { return s.State().Camera.State == CameraUnsupported },
		func() string { return s.State().Camera.State.String() })
	if CameraSupported() {
		t.Error("CameraSupported() with no backend")
	}
}

// Nothing logs through Options.Logf once Leave has returned: pion runs
// OnConnectionStateChange on its own goroutine, and its "peer connection
// closed" used to reach a test's t.Logf after the test ended (a panic:
// TestCameraUnsupported, TestHuddleShotsTiles).
func TestNoLogAfterLeave(t *testing.T) {
	clip, err := os.ReadFile("testdata/testsrc-320x180.h264")
	if err != nil {
		t.Fatal(err)
	}
	fk := NewFake(clip, t.Errorf)
	defer fk.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var left atomic.Bool
	var late atomic.Value
	logf := func(format string, args ...any) {
		if left.Load() {
			late.Store(fmt.Sprintf(format, args...))
			return
		}
		t.Logf(format, args...)
	}
	s, err := Join(ctx, Options{Client: fk.Caller(), SelfID: fakeSelf, Device: NewToneDevice(440), Logf: logf}, "C0TEST")
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, 5*time.Second, "connected", func() bool { return s.State().Phase == Connected },
		func() string { return s.State().Phase.String() })
	s.Leave()
	left.Store(true)
	s.logf("huddle: peer connection %s", "closed") // what a late pion callback does
	time.Sleep(200 * time.Millisecond)             // and anything else still winding down
	if v := late.Load(); v != nil {
		t.Errorf("logged after Leave returned: %q", v)
	}
}
