package huddleui

import (
	"context"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/huddle"
	"github.com/some-du6e/yoloslackgoclient/internal/huddle/avperm"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/store"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme/pixeltest"
)

// Headless pictures of the huddle UI against the fake Chime: the pixel
// checks run where there is a GPU (CI: Mesa llvmpipe, CLACK_PIXEL_TESTS),
// and CLACK_SHOTS=<dir> also writes the PNGs (the huddle workflow uploads
// them).

type shotEnv struct {
	t  *testing.T
	c  *Controller
	w  *window
	h  *hcHost
	fk *huddle.Fake
}

func newShotEnv(t *testing.T, dark bool) *shotEnv {
	t.Helper()
	st := store.New("")
	st.PutUsers([]slack.User{
		{ID: huddle.FakeSelfUser, Name: "me", RealName: "Test Self"},
		{ID: huddle.FakeRemoteUser, Name: "remote", RealName: "Remote Example"},
	})
	st.PutConv(slack.Conversation{ID: hcChannel, Name: "huddle-test", IsChannel: true, IsMember: true, NumMembers: 2})
	app := core.New(slack.New(slack.Creds{}), st)
	if u, ok := st.User(huddle.FakeSelfUser); ok {
		app.S.Self = *u
	}
	e := &shotEnv{t: t, h: &hcHost{app: app, th: theme.New(dark)}}
	e.c = New(e.h)
	e.c.SelfID = huddle.FakeSelfUser
	w := &window{c: e.c, clicks: map[string]*widget.Clickable{}}
	w.th = theme.Bare(true)
	sh := theme.NewShaper()
	w.th.SetShapers(sh, sh)
	e.w = w
	return e
}

// join joins the fake meeting with the given microphone answer; nil
// access means a working tone microphone.
func (e *shotEnv) join(access func() <-chan avperm.Status, open func(func(string, ...any)) (huddle.Device, error)) *huddle.Session {
	e.t.Helper()
	clip, err := os.ReadFile(filepath.Join("..", "..", "huddle", "testdata", "testsrc-320x180.h264"))
	if err != nil {
		e.t.Fatal(err)
	}
	e.fk = huddle.NewFake(clip, e.t.Errorf)
	e.t.Cleanup(e.fk.Close)
	o := huddle.Options{Client: e.fk.Caller(), SelfID: huddle.FakeSelfUser, Logf: e.t.Logf, OnChange: e.c.onSession}
	if access == nil {
		o.Device = huddle.NewToneDevice(440)
	} else {
		o.MicAccess, o.OpenAudio = access, open
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s, err := huddle.Join(ctx, o, hcChannel)
	if err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(s.Leave)
	e.c.mu.Lock()
	e.c.s = s
	e.c.mu.Unlock()
	return s
}

const shotScale = 2

func shotCtx(ops *op.Ops, w, h int) layout.Context {
	return layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: shotScale, PxPerSp: shotScale},
		Constraints: layout.Exact(image.Pt(w*shotScale, h*shotScale)), Now: time.Now()}
}

// render draws f into a w×h dp picture (two frames: clicks and text
// measurement settle on the second).
func render(t *testing.T, name string, w, h int, f func(gtx C)) *image.RGBA {
	t.Helper()
	var ops op.Ops
	for i := 0; i < 2; i++ {
		ops.Reset()
		f(shotCtx(&ops, w, h))
	}
	img := pixeltest.Render(t, w*shotScale, h*shotScale, &ops)
	if dir := os.Getenv("CLACK_SHOTS"); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		fp, err := os.Create(filepath.Join(dir, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		defer fp.Close()
		if err := png.Encode(fp, img); err != nil {
			t.Fatal(err)
		}
	}
	return img
}

func waitState(t *testing.T, s *huddle.Session, what string, cond func(huddle.State) bool) huddle.State {
	t.Helper()
	deadline := time.Now().Add(25 * time.Second)
	for {
		st := s.State()
		if cond(st) {
			return st
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s: %+v", what, st)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func deny() <-chan avperm.Status {
	ch := make(chan avperm.Status, 1)
	ch <- avperm.Denied
	return ch
}

func nullAudio(func(string, ...any)) (huddle.Device, error) { return huddle.NewToneDevice(0), nil }

func TestMicNoteFor(t *testing.T) {
	for _, m := range []huddle.MicStatus{huddle.MicDenied, huddle.MicRestricted, huddle.MicNoUsage, huddle.MicFailed, huddle.MicSilent} {
		n, ok := micNoteFor(huddle.State{Mic: m, MicErr: "boom"})
		if !ok || n.title == "" || n.body == "" || micShort(m) == "" {
			t.Fatalf("%v: no note", m)
		}
		if m == huddle.MicDenied && n.settings != avperm.SettingsURL(avperm.Mic) {
			t.Fatalf("denied: settings %q", n.settings)
		}
	}
	for _, m := range []huddle.MicStatus{huddle.MicOK, huddle.MicStarting} {
		if _, ok := micNoteFor(huddle.State{Mic: m}); ok {
			t.Fatalf("%v: a note", m)
		}
	}
	var b micBanner
	st := huddle.State{Mic: huddle.MicDenied}
	if _, ok := b.visible(st); !ok {
		t.Fatal("not visible")
	}
	b.dismissed = huddle.MicDenied
	if _, ok := b.visible(st); ok {
		t.Fatal("visible after dismissal")
	}
	if _, ok := b.visible(huddle.State{Mic: huddle.MicSilent}); !ok {
		t.Fatal("a new problem stays hidden")
	}
}

// The microphone was denied: the huddle window shows the banner over the
// stage, the sidebar bar a warning row, and the mic buttons a badge.
func TestHuddleShotsMicDenied(t *testing.T) {
	e := newShotEnv(t, false)
	s := e.join(deny, nullAudio)
	waitState(t, s, "connected, mic denied", func(st huddle.State) bool {
		return st.Phase == huddle.Connected && st.Mic == huddle.MicDenied
	})
	img := render(t, "window-mic-denied", 620, 400, func(gtx C) { e.w.layout(gtx) })
	// how it looks on a Mac: with the System Settings button
	goos, set := micGOOS, micSettings
	micGOOS = "darwin"
	micSettings = func(m avperm.Media) string {
		return "x-apple.systempreferences:com.apple.preference.security?Privacy_Microphone"
	}
	render(t, "window-mic-denied-macos", 620, 400, func(gtx C) { e.w.layout(gtx) })
	render(t, "bar-mic-denied-macos", 260, 112+barMicRow, func(gtx C) {
		theme.Fill(gtx, image.Rectangle{Max: gtx.Constraints.Max}, hudFrame)
		e.c.LayoutBar(gtx, e.h.th)
	})
	micGOOS, micSettings = goos, set
	top := image.Rect(0, 30*shotScale, 620*shotScale, 160*shotScale)
	if n := pixeltest.Count(img, top, hudWarn, 24); n < 20 {
		t.Fatalf("no warning icon in the window banner (%d px)", n)
	}
	if n := pixeltest.Count(img, top, hudWarnBg, 12); n < 2000 {
		t.Fatalf("no banner card (%d px)", n)
	}
	gtxH := func() int {
		var ops op.Ops
		return e.c.BarHeight(shotCtx(&ops, 260, 200)) / shotScale
	}()
	if gtxH != 112+barMicRow {
		t.Fatalf("bar height %d dp", gtxH)
	}
	bar := render(t, "bar-mic-denied", 260, gtxH, func(gtx C) {
		theme.Fill(gtx, image.Rectangle{Max: gtx.Constraints.Max}, hudFrame)
		e.c.LayoutBar(gtx, e.h.th)
	})
	if n := pixeltest.Count(bar, bar.Bounds(), hudWarn, 24); n < 20 {
		t.Fatalf("no warning in the bar (%d px)", n)
	}
}

// A working microphone: no banner.
func TestHuddleShotsMicOK(t *testing.T) {
	e := newShotEnv(t, false)
	s := e.join(nil, nil)
	waitState(t, s, "connected", func(st huddle.State) bool { return st.Phase == huddle.Connected })
	img := render(t, "window-mic-ok", 620, 400, func(gtx C) { e.w.layout(gtx) })
	if n := pixeltest.Count(img, image.Rect(0, 30*shotScale, 620*shotScale, 160*shotScale), hudWarnBg, 6); n > 200 {
		t.Fatalf("a banner with a working mic (%d px)", n)
	}
	var ops op.Ops
	if h := e.c.BarHeight(shotCtx(&ops, 260, 200)); h != 112*shotScale {
		t.Fatalf("bar height %d", h)
	}
}
