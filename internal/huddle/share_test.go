package huddle

import (
	"context"
	"image"
	"image/color"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/thesyncim/goh264"
)

// TestFakeScreenShare shares the test screen against the fake Chime: a
// second attendee joins with "#content" on its id and join token,
// subscribes DUPLEX with the audio and screen send streams and nothing to
// receive, sends H.264 the fake decodes (a keyframe when it asks) and
// audio; the main session sends ContentShareDetails and the draw
// capability, marks you as sharing, previews your share unmirrored, never
// subscribes to your own content and makes no tile for a content
// attendee. Stopping leaves the content attendee; sharing again works,
// and leaving the huddle ends the share.
func TestFakeScreenShare(t *testing.T) {
	clip, err := os.ReadFile("testdata/testsrc-320x180.h264")
	if err != nil {
		t.Fatal(err)
	}
	fk := NewFake(clip, t.Errorf)
	defer fk.Close()
	// The first keyframe never arrives, as when it was written between
	// pion's DTLS transport saying connected and SRTP starting (CI, 0
	// pictures for 20 s): the receiver asks for another.
	fk.f.content.dropFirst.Store(true)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, err := Join(ctx, Options{
		Client: fk.Caller(), SelfID: fakeSelf, Device: NewToneDevice(440), Logf: t.Logf,
		OpenShare: func(src ShareSource, audio bool, _ func(string, ...any)) (ScreenCapture, error) {
			return NewTestScreen(640, 360, 15, audio), nil
		},
	}, "C0TEST")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Leave()
	waitFor(t, 15*time.Second, "media", func() bool { return s.State().Phase == Connected && fk.f.subs.Load() >= 2 },
		func() string { return s.State().Phase.String() })

	src := ShareSource{ID: "screen:1:0", Name: "Entire Screen"}
	s.StartShare(src, true)
	if st := s.State(); !st.Share.State.Sharing() || !st.Participants[0].Sharing {
		t.Errorf("right after StartShare: share %s, self sharing %v", st.Share.State, st.Participants[0].Sharing)
	}
	info := func() string {
		c := fk.ContentShare()
		return s.State().Share.State.String() + " " + s.State().Share.Err + "; fake content: " + itoa64(int64(c.Joins)) + " joins, " + itoa64(c.Packets) + " video packets, " +
			itoa64(c.Frames) + " pictures (" + itoa64(c.Undecodable) + " undecodable, " + itoa64(c.KeyRequests) + " keyframe requests), " + itoa64(c.Keys) + " keyframes (" + itoa64(c.KeysAfterPLI) + " after PLI), " +
			itoa64(c.AudioPackets) + " audio packets; " + s.Stats()
	}
	waitFor(t, 20*time.Second, "the share at the fake", func() bool {
		c := fk.ContentShare()
		return s.State().Share.State == ShareOn && c.Frames >= 10 && c.KeysAfterPLI >= 1 && c.AudioPackets >= 20 && c.Present
	}, info)
	t.Logf("sharing: %s", info())
	c := fk.ContentShare()
	if c.Token != fakeToken+"#content" {
		t.Errorf("content join token %q", c.Token)
	}
	if c.Duplex != streamDuplex {
		t.Errorf("content SUBSCRIBE duplex %d, want DUPLEX", c.Duplex)
	}
	if len(c.Video) != 1 {
		t.Fatalf("content video send streams: %+v", c.Video)
	}
	if v := c.Video[0]; v.StreamID != 2 || v.GroupID != 2 || v.TrackLabel != "AmazonChimeExpressVideo" || v.AttendeeID != fakeSelfAtt+"#content" ||
		v.MediaType != mediaVideo || v.Framerate != 15 || v.MaxBitrateKbps != 1500 {
		t.Errorf("content video stream: %+v", v)
	}
	if len(c.Audio) != 1 || c.Audio[0].AttendeeID != fakeSelfAtt+"#content" || c.Audio[0].TrackLabel != "AmazonChimeExpressAudio" {
		t.Errorf("content audio stream: %+v", c.Audio)
	}
	if len(c.Receive) != 1 || c.Receive[0] != 0 {
		t.Errorf("content receive stream ids %v, want [0] (nothing to receive)", c.Receive)
	}
	if c.W != 640 || c.H != 360 || !strings.EqualFold(c.Mime, "video/H264") {
		t.Errorf("fake decoded %dx%d %s, want 640x360 H.264", c.W, c.H, c.Mime)
	}

	// what the main session said and shows
	var details *ContentShareDetails
	var caps *Capabilities
	for _, m := range fk.PeerMessages() {
		if m.ContentShareDetails != nil {
			details = m.ContentShareDetails
		}
		if m.Capabilities != nil {
			caps = m.Capabilities
		}
	}
	if details == nil || details.SourceType != "desktop:screen" || details.ItemID != fakeSelfAtt {
		t.Errorf("ContentShareDetails: %+v", details)
	}
	if caps == nil || caps.IsDrawEnabled {
		t.Errorf("Capabilities: %+v (Clack shows presenters no drawings: isDrawEnabled false)", caps)
	}
	st := s.State()
	if !st.Participants[0].Self || !st.Participants[0].Sharing {
		t.Errorf("self participant: %+v", st.Participants[0])
	}
	for _, p := range st.Participants {
		if strings.Contains(p.AttendeeID, "#content") || strings.Contains(p.UserID, "#content") {
			t.Errorf("a content attendee got a tile: %+v", p)
		}
	}
	if len(st.Participants) != 2 {
		t.Errorf("%d participants, want 2 (you and the other person; no content attendees)", len(st.Participants))
	}
	var mine *Video
	for i, v := range st.Videos {
		if v.Key == fakeSelfAtt+"#content" {
			t.Errorf("subscribed to your own share: %+v", v)
		}
		if v.Key == SelfShareKey {
			mine = &st.Videos[i]
		}
	}
	if mine == nil || !mine.Content || mine.UserID != fakeSelf {
		t.Errorf("your share in State().Videos: %+v", st.Videos)
	}
	var pv VideoFrame
	waitFor(t, 5*time.Second, "share preview", func() bool { pv = s.Frame(SelfShareKey); return pv.Img != nil }, info)
	if b := pv.Img.Bounds(); b.Dx() != 640 || b.Dy() != 360 {
		t.Errorf("share preview %v", b)
	}
	// the test screen's "text" starts at the left (x = w/16): not mirrored
	row := 360/24*2 + 2
	if l, r := pv.Img.RGBAAt(640/16+4, row), pv.Img.RGBAAt(640-4, row); l.R > 0xa0 || r.R < 0xd0 {
		t.Errorf("share preview mirrored or wrong: left %v, right %v", l, r)
	}

	// stop: the content attendee leaves, Chime drops it, frames stop
	s.StopShare()
	waitFor(t, 10*time.Second, "share off", func() bool {
		c := fk.ContentShare()
		return s.State().Share.State == ShareOff && c.Leaves >= 1 && !c.Present
	}, info)
	if st := s.State(); st.Participants[0].Sharing {
		t.Error("still marked as sharing")
	}
	n := fk.ContentShare().Frames
	time.Sleep(800 * time.Millisecond)
	if d := fk.ContentShare().Frames - n; d > 1 {
		t.Errorf("%d screen frames after stopping", d)
	}
	if pv := s.Frame(SelfShareKey); pv.Img != nil {
		t.Error("share preview still showing")
	}

	// a window this time, then leave the huddle while sharing
	before := fk.ContentShare().Frames
	s.StartShare(ShareSource{ID: "window:101:0", Name: "Release notes"}, false)
	waitFor(t, 20*time.Second, "sharing again", func() bool {
		return s.State().Share.State == ShareOn && fk.ContentShare().Frames >= before+10
	}, info)
	details = nil
	for _, m := range fk.PeerMessages() {
		if m.ContentShareDetails != nil {
			details = m.ContentShareDetails
		}
	}
	if details == nil || details.SourceType != "desktop:window" {
		t.Errorf("ContentShareDetails for a window: %+v", details)
	}
	s.Leave()
	waitFor(t, 5*time.Second, "content attendee left with you", func() bool { return fk.ContentShare().Leaves >= 2 }, info)
	if st := s.ShareStatus(); st.State != ShareOff {
		t.Errorf("share after Leave: %s", st.State)
	}
}

// TestShareFailures: a capture that can't open maps to Slack's alerts.
func TestShareFailures(t *testing.T) {
	clip, err := os.ReadFile("testdata/testsrc-320x180.h264")
	if err != nil {
		t.Fatal(err)
	}
	fk := NewFake(clip, t.Errorf)
	defer fk.Close()
	var fail error
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, err := Join(ctx, Options{Client: fk.Caller(), SelfID: fakeSelf, Device: NewToneDevice(440), Logf: t.Logf,
		OpenShare: func(ShareSource, bool, func(string, ...any)) (ScreenCapture, error) { return nil, fail }}, "C0TEST")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Leave()
	waitFor(t, 15*time.Second, "media", func() bool { return s.State().Phase == Connected }, func() string { return s.State().Phase.String() })
	for _, c := range []struct {
		err  error
		want ShareState
	}{
		{ErrShareDenied, ShareDenied},
		{ErrShareNotReadable, ShareNotReadable},
		{ErrShareNotFound, ShareNotFound},
		{ErrShareUnsupported, ShareUnsupported},
		{os.ErrPermission, ShareFailed},
	} {
		fail = c.err
		s.StartShare(ShareSource{ID: "screen:1:0"}, false)
		waitFor(t, 5*time.Second, c.want.String(), func() bool { return s.ShareStatus().State == c.want },
			func() string { return s.ShareStatus().State.String() + " " + s.ShareStatus().Err })
		if s.State().Participants[0].Sharing {
			t.Errorf("%s: still marked as sharing", c.want)
		}
	}
	if fk.ContentShare().Joins != 0 {
		t.Errorf("a content attendee joined for a capture that failed")
	}
}

// TestShareSources: the test backend's picker sources, Stage Manager's
// pseudo-windows dropped and ids normalized as Slack's mTT/ and aOZQ do.
func TestShareSources(t *testing.T) {
	t.Setenv("CLACK_HUDDLE_TESTSCREEN", "1280x720")
	l, err := ListShareSources(ShareScreen)
	if err != nil || len(l) != 1 {
		t.Fatalf("screens: %v %+v", err, l)
	}
	if l[0].Name != "Entire Screen" || l[0].ID != "screen:1:0" || l[0].Kind() != ShareScreen {
		t.Errorf("screen: %+v", l[0])
	}
	if b := l[0].Thumb.Bounds(); b.Dx() != 346 || b.Dy() != 194 {
		t.Errorf("1280x720 thumbnail %v, want 346x194 (fit in 346x260)", b)
	}
	w, err := ListShareSources(ShareWindow)
	if err != nil || len(w) != 2 || w[0].Name != "Release notes" || w[0].AppName != "TextEdit" || w[1].Kind() != ShareWindow {
		t.Fatalf("windows: %v %+v", err, w)
	}

	t.Setenv("CLACK_HUDDLE_TESTSCREEN", "")
	old := shareOS
	defer func() { shareOS = old }()
	shareOS = stubShares{{ID: "window:7:3", Name: "Notes"}, {ID: "window:8:0", Name: "Gesture Blocking Overlay"}, {ID: "screen:1:0", Name: "Entire Screen"}}
	w, err = ListShareSources(ShareWindow)
	if err != nil || len(w) != 1 || w[0].ID != "window:7:0" {
		t.Errorf("filtered windows: %v %+v", err, w)
	}
	if names := screenNames(2); names[0] != "Screen 1" || names[1] != "Screen 2" {
		t.Errorf("screen names %v", names)
	}
}

type stubShares []ShareSource

func (s stubShares) List(ShareKind, image.Point) ([]ShareSource, error) {
	return append([]ShareSource(nil), s...), nil
}
func (stubShares) Open(ShareSource, bool, func(string, ...any)) (ScreenCapture, error) {
	return nil, ErrShareUnsupported
}
func (stubShares) SystemAudio() bool { return false }

// TestShareScalesForThePCMEncoder: without an OS encoder a screen bigger
// than the pure-Go encoder takes is scaled down to fit, not dropped.
func TestShareScalesForThePCMEncoder(t *testing.T) {
	old := newHWEncoders
	newHWEncoders = nil
	defer func() { newHWEncoders = old }()
	img := image.NewRGBA(image.Rect(0, 0, 1280, 720))
	paintTestScreen(img, 3)
	var f CameraFrame
	rgbaToNV12(&f, img)
	f.At = time.Now()
	p := &h264Pub{what: "test", fps: 15, bps: 1_000_000, minBps: 100_000, screen: true}
	var au []byte
	tr := &captureTrack{}
	p.sendTo(&f, tr.write, t.Logf)
	if au = tr.last; au == nil {
		t.Fatal("nothing encoded")
	}
	if p.encSize != [2]int{640, 360} {
		t.Errorf("encoder %v, want 640x360", p.encSize)
	}
	frames, err := goh264.NewDecoder().DecodeFrames(au)
	if err != nil || len(frames) == 0 {
		t.Fatalf("decode: %v (%d frames)", err, len(frames))
	}
	if fr := frames[0]; fr.Width != 640 || fr.Height != 360 {
		t.Errorf("decoded %dx%d", fr.Width, fr.Height)
	}
}

type captureTrack struct{ last []byte }

func (c *captureTrack) write(au []byte, _ time.Duration) error {
	c.last = append([]byte(nil), au...)
	return nil
}

// TestScreenPixels: thumbnails, RGB to NV12 and back, and scaling.
func TestScreenPixels(t *testing.T) {
	for _, c := range []struct{ w, h, bw, bh, ww, wh int }{
		{1920, 1080, 346, 260, 346, 194},
		{1000, 2000, 346, 260, 130, 260},
		{200, 100, 346, 260, 200, 100},
	} {
		if w, h := fitIn(c.w, c.h, image.Pt(c.bw, c.bh)); w != c.ww || h != c.wh {
			t.Errorf("fitIn(%d,%d) = %d,%d, want %d,%d", c.w, c.h, w, h, c.ww, c.wh)
		}
	}
	if w, h := fitArea(1920, 1080, maxPCMPixels); w*h > maxPCMPixels || w%2 != 0 || h%2 != 0 || w < 560 {
		t.Errorf("fitArea(1920,1080) = %dx%d", w, h)
	}
	if w, h := capSize(5120, 2880); w != 2560 || h != 1440 {
		t.Errorf("capSize(5120,2880) = %dx%d", w, h)
	}
	img := image.NewRGBA(image.Rect(0, 0, 64, 32))
	fillRect(img, image.Rect(0, 0, 32, 32), color.RGBA{0xe0, 0x1e, 0x5a, 0xff})
	fillRect(img, image.Rect(32, 0, 64, 32), color.RGBA{0x12, 0x64, 0xa3, 0xff})
	var f CameraFrame
	rgbaToNV12(&f, img)
	back := image.NewRGBA(image.Rect(0, 0, 64, 32))
	nv12ToRGBA(back, &f, 1)
	near := func(a, b color.RGBA) bool {
		d := func(x, y uint8) int { return max(int(x)-int(y), int(y)-int(x)) }
		return d(a.R, b.R) < 8 && d(a.G, b.G) < 8 && d(a.B, b.B) < 8
	}
	if c := back.RGBAAt(8, 8); !near(c, color.RGBA{0xe0, 0x1e, 0x5a, 0xff}) {
		t.Errorf("left after the round trip: %v", c)
	}
	if c := back.RGBAAt(56, 8); !near(c, color.RGBA{0x12, 0x64, 0xa3, 0xff}) {
		t.Errorf("right after the round trip: %v", c)
	}
	bgra := make([]byte, 64*32*4)
	for i := 0; i < len(bgra); i += 4 {
		bgra[i], bgra[i+1], bgra[i+2] = img.Pix[i+2], img.Pix[i+1], img.Pix[i]
	}
	var g CameraFrame
	bgraToNV12(&g, bgra, 64*4, 64, 32)
	if string(g.Y) != string(f.Y) || string(g.UV) != string(f.UV) {
		t.Error("BGRA and RGBA convert differently")
	}
	var half CameraFrame
	scaleNV12(&half, &f, 32, 16)
	small := image.NewRGBA(image.Rect(0, 0, 32, 16))
	nv12ToRGBA(small, &half, 1)
	if c := small.RGBAAt(4, 4); !near(c, color.RGBA{0xe0, 0x1e, 0x5a, 0xff}) {
		t.Errorf("scaled left: %v", c)
	}
	if c := small.RGBAAt(28, 4); !near(c, color.RGBA{0x12, 0x64, 0xa3, 0xff}) {
		t.Errorf("scaled right: %v", c)
	}
	th := thumbOf(img, image.Pt(16, 16))
	if b := th.Bounds(); b.Dx() != 16 || b.Dy() != 8 {
		t.Errorf("thumbnail %v", b)
	}
}
