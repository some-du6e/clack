package huddleui

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/huddle"
	"github.com/some-du6e/clack/internal/huddle/avperm"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
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
	render(t, "dock-mic-denied-macos", 900, dockBar+barMicRow+4+dockPad, func(gtx C) {
		theme.Fill(gtx, image.Rectangle{Max: gtx.Constraints.Max}, hudFrame)
		e.c.LayoutDock(gtx, e.h.th)
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
		return e.c.DockHeight(shotCtx(&ops, 900, 200)) / shotScale
	}()
	if gtxH != dockBar+barMicRow+4+dockPad {
		t.Fatalf("dock height %d dp", gtxH)
	}
	bar := render(t, "dock-mic-denied", 900, gtxH, func(gtx C) {
		theme.Fill(gtx, image.Rectangle{Max: gtx.Constraints.Max}, hudFrame)
		e.c.LayoutDock(gtx, e.h.th)
	})
	if n := pixeltest.Count(bar, bar.Bounds(), hudWarn, 24); n < 20 {
		t.Fatalf("no warning over the dock (%d px)", n)
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
	if h := e.c.DockHeight(shotCtx(&ops, 900, 200)); h != (dockBar+dockPad)*shotScale {
		t.Fatalf("dock height %d", h)
	}
	if h := e.c.BarHeight(shotCtx(&ops, 260, 200)); h != 0 {
		t.Fatalf("a sidebar bar in a huddle (%d): the controls are the dock's", h)
	}
}

// The channel header's live huddle button: mint with faces, headphones and
// a bare count when you're not in it; solid jade with white headphones
// when you are. Never "Huddle N".
func TestHeaderShots(t *testing.T) {
	for _, dark := range []bool{false, true} {
		e := newShotEnv(t, dark)
		suffix := "light"
		page := color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
		if dark {
			suffix, page = "dark", color.NRGBA{R: 0x1a, G: 0x1d, B: 0x21, A: 0xff}
		}
		m := mode(e.h.th)
		draw := func(gtx C) {
			theme.Fill(gtx, image.Rectangle{Max: gtx.Constraints.Max}, page)
			off := op.Offset(image.Pt(gtx.Dp(8), gtx.Dp(6))).Push(gtx.Ops)
			e.c.HeaderPill(gtx, e.h.th, hcChannel, gtx.Dp(28))
			off.Pop()
		}

		// live, not in it: two people
		e.c.mu.Lock()
		e.c.live[hcChannel] = liveRoom{ID: "R1", Channel: hcChannel, Participants: []string{huddle.FakeRemoteUser, "U0THIRD"}}
		e.c.mu.Unlock()
		var ops op.Ops
		g := shotCtx(&ops, 160, 40)
		w := e.c.HeaderPillWidth(g, e.h.th, hcChannel) / shotScale
		// 2+8 faces(20+18.5) 8 icon 20, 4 + "2", 4, caret 20
		if w < 104 || w > 118 {
			t.Fatalf("live width %d dp", w)
		}
		img := render(t, "header-live-"+suffix, 160, 40, draw)
		pill := image.Rect(8*shotScale, 6*shotScale, (8+w)*shotScale, 34*shotScale)
		if n := pixeltest.Count(img, pill, hdrJade5[m], 3); n < pill.Dx()*pill.Dy()/4 {
			t.Fatalf("%s: live pill isn't jade-5 (%d px)", suffix, n)
		}
		if n := pixeltest.Count(img, pill, hdrJade100[m], 40); n < 30 {
			t.Fatalf("%s: no jade-100 ink (%d px)", suffix, n)
		}

		// in it: the fill, headphones only
		e.join(nil, nil)
		g = shotCtx(&ops, 160, 40)
		if w := e.c.HeaderPillWidth(g, e.h.th, hcChannel) / shotScale; w != 48 {
			t.Fatalf("in-huddle width %d dp, want 28+20", w)
		}
		img = render(t, "header-in-"+suffix, 160, 40, draw)
		pill = image.Rect(8*shotScale, 6*shotScale, 56*shotScale, 34*shotScale)
		if n := pixeltest.Count(img, pill, hdrJade50[m], 3); n < pill.Dx()*pill.Dy()/2 {
			t.Fatalf("%s: in-huddle button isn't jade-50 (%d px)", suffix, n)
		}
		if n := pixeltest.Count(img, pill, hdrJade5[m], 3); n > 10 {
			t.Fatalf("%s: mint left in the in-huddle button (%d px)", suffix, n)
		}
	}
}

func TestHeaderTip(t *testing.T) {
	e := newShotEnv(t, false)
	if got := e.c.headerTip(hcChannel, true, nil); got != "Leave huddle" {
		t.Fatal(got)
	}
	if got := e.c.headerTip(hcChannel, false, []string{"U1", "U2"}); got != "Join huddle in #huddle-test\n2 in the huddle" {
		t.Fatalf("%q", got)
	}
	if got := e.c.headerTip(hcChannel, false, []string{huddle.FakeSelfUser}); got != "Joined on another device\nJoin on this device" {
		t.Fatalf("%q", got)
	}
}
