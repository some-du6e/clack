package huddleui

import (
	"image"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/huddle"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// computeShouldShowJoinPreview: with huddles_show_join_preview on, a start
// or join opens the preview first, except from the invite window or for
// the huddle you're already in; Join starts the huddle with the
// preview's mic and camera.
func TestJoinPreviewFlow(t *testing.T) {
	e := newShotEnv(t, false)
	c := e.c
	if c.shouldPreview(hcChannel, EntryHeaderButton) {
		t.Fatal("preview with the pref unset (Slack: off)")
	}
	setPrefs(e.h.app, map[string]any{"huddles_show_join_preview": true})
	c.NoWindow = false
	if !c.shouldPreview(hcChannel, EntryHeaderButton) {
		t.Fatal("no preview with huddles_show_join_preview on")
	}
	if c.shouldPreview(hcChannel, EntryInviteWindow) {
		t.Fatal("preview from the invite window")
	}

	clip, err := os.ReadFile(filepath.Join("..", "..", "huddle", "testdata", "testsrc-320x180.h264"))
	if err != nil {
		t.Fatal(err)
	}
	fk := huddle.NewFake(clip, t.Errorf)
	t.Cleanup(fk.Close)
	c.Client = fk.Caller()
	c.NoWindow = true // the huddle window; the preview is driven by hand
	c.Tweak = func(o *huddle.Options) {
		o.Device = huddle.NewToneDevice(440)
		o.OpenCamera = func(func(string, ...any)) (huddle.Camera, error) { return huddle.NewTestCamera(320, 180, 15), nil }
	}
	w := &previewWindow{c: c, channel: hcChannel, entry: EntryHeaderButton, micOn: false, camOn: true}
	c.mu.Lock()
	c.pwin = w
	c.mu.Unlock()
	w.join(false)
	w.join(false) // a double click joins once
	var s *huddle.Session
	for end := time.Now().Add(15 * time.Second); s == nil && time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		s = c.Session()
	}
	if s == nil {
		t.Fatal("Join didn't start the huddle")
	}
	t.Cleanup(s.Leave)
	if !s.State().Muted {
		t.Fatal("the preview's mic was off but the huddle started unmuted")
	}
	waitState(t, s, "the preview's camera on", func(st huddle.State) bool {
		return st.Camera.State == huddle.CameraOn || st.Camera.State == huddle.CameraStarting
	})
	if c.shouldPreview(hcChannel, EntryHeaderButton) {
		t.Fatal("preview for the huddle you're in")
	}
}

// Pictures of the join preview (CLACK_SHOTS: join-preview-*.png): start in
// a channel with the camera off, the camera on while you talk (the white
// talking border and pill), the microphone menu, and joining a huddle
// someone's in.
func TestHuddleShotsJoinPreview(t *testing.T) {
	for _, dark := range []bool{false, true} {
		mode := "light"
		if dark {
			mode = "dark"
		}
		e := newShotEnv(t, dark)
		c := e.c
		th := theme.New(dark)
		var cl previewClicks
		var vid videoOps
		shot := func(name string, v previewView) *image.RGBA {
			return render(t, "join-preview-"+name+"-"+mode, previewW, previewH, func(gtx C) {
				layoutPreview(gtx, th, c, v, &cl, &vid)
			})
		}
		base := func() previewView {
			v := previewView{Self: huddle.FakeSelfUser, MicOn: true, Labels: [3]string{"MacBook Pro Microphone", "MacBook Pro Speakers", "FaceTime HD Camera"}}
			v.Title, v.Avatars, v.JoinLabel = c.previewTitle(hcChannel)
			return v
		}
		img := shot("start", base())
		k := previewInkFor(th)
		if n := pixeltest.Count(img, image.Rect(0, (previewH-90)*shotScale, previewW*shotScale, previewH*shotScale), k.join, 10); n < 400 {
			t.Fatalf("%s: no Start Huddle button (%d px)", mode, n)
		}

		// a real preview: the tone mic talking, the test camera on
		p := huddle.StartPreview(huddle.Options{Device: huddle.NewToneDevice(440), OpenCamera: func(func(string, ...any)) (huddle.Camera, error) {
			return huddle.NewTestCamera(320, 180, 15), nil
		}}, true)
		for end := time.Now().Add(5 * time.Second); time.Now().Before(end) && !(p.Talking() && p.Frame().Img != nil); time.Sleep(20 * time.Millisecond) {
		}
		v := base()
		v.CamOn, v.Talking = true, p.Talking()
		f := p.Frame()
		v.Frame, v.FrameSeq = f.Img, f.Seq
		p.Close()
		if v.Frame == nil || !v.Talking {
			t.Fatalf("%s: preview frame %v talking %v", mode, v.Frame != nil, v.Talking)
		}
		img = shot("camera-talking", v)
		white := image.Rect(16*shotScale, 100*shotScale, 40*shotScale, 600*shotScale)
		if n := pixeltest.Count(img, white, hudWhite, 4); n < 200 {
			t.Fatalf("%s: no talking border (%d white px at the video's left edge)", mode, n)
		}

		v = base()
		v.Menu = "mic"
		v.MenuItems = []previewItem{{Label: "System default"}, {Key: "a", Label: "MacBook Pro Microphone", Checked: true}, {Key: "b", Label: "AirPods Pro"}}
		shot("mic-menu", v)

		v = base()
		v.MicOn, v.MicBad, v.CamBad = false, true, true
		v.Labels[2] = "No cameras found"
		shot("no-devices", v)

		c.mu.Lock()
		c.live[hcChannel] = liveRoom{ID: "R1", Channel: hcChannel, Participants: []string{huddle.FakeRemoteUser}}
		c.mu.Unlock()
		v = base()
		if v.JoinLabel != "Join" {
			t.Fatalf("join label %q with someone there", v.JoinLabel)
		}
		shot("join", v)
		e.h.app.S.PutConv(slack.Conversation{ID: dmChannel, IsIM: true, User: huddle.FakeRemoteUser})
		v = base()
		v.Title, v.Avatars, v.JoinLabel = c.previewTitle(dmChannel)
		shot("dm", v)
	}
}
