package huddleui

import (
	"image"
	"os"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/system"
	"gioui.org/op"

	"github.com/some-du6e/clack/internal/huddle"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// TestShareButtonStates: HuddleToolbarScreenshareButton's icon, tooltip
// and states (useScreenShareProps, Q9Yv), and the limit
// (getRemotePeersWithScreenshareEnabledCount > 1).
func TestShareButtonStates(t *testing.T) {
	e := newShotEnv(t, false)
	self := huddle.Participant{UserID: huddle.FakeSelfUser, Self: true}
	st := huddle.State{Participants: []huddle.Participant{self, {UserID: "U1", Sharing: true}}}
	if k := e.c.shareCtl(st); k.icon != "screen" || k.tip != "Share your screen" || k.on || k.dim {
		t.Errorf("idle with one other sharing: %+v", k)
	}
	st.Participants = append(st.Participants, huddle.Participant{UserID: "U2", Sharing: true})
	if k := e.c.shareCtl(st); !k.dim || k.tip != "Screen sharing limit reached" {
		t.Errorf("two others sharing: %+v", k)
	}
	st.Share.State = huddle.ShareOn
	if k := e.c.shareCtl(st); k.icon != "screen-off" || k.tip != "Stop sharing screen" || !k.on || k.dim {
		t.Errorf("sharing: %+v", k)
	}
	// the window's toolbar has it after the camera, the dock too
	ids := func(cs []ctl) string {
		var out []string
		for _, c := range cs {
			out = append(out, c.id)
		}
		return strings.Join(out, " ")
	}
	if got := ids(e.c.dockCtls(huddle.State{})); !strings.Contains(got, "camera share hand") {
		t.Errorf("dock buttons %q: share should follow the camera", got)
	}
}

// TestShareFlow runs Slack's flow against the fake Chime with the test
// screen: the click opens ScreenPickerModal in the window, the screens
// and windows load with thumbnails, Share starts the share (the button
// turns into Stop sharing screen, your share takes the stage), the mini
// panel draws, and Stop Sharing ends it. Shots: share-picker-*,
// share-window-sharing, share-mini-panel, share-alert-*.
func TestShareFlow(t *testing.T) {
	t.Setenv("CLACK_HUDDLE_TESTSCREEN", "1280x720")
	e := newShotEnv(t, false)
	e.c.NoWindow = true // no native mini panel window in tests
	s := e.join(nil, nil)
	waitState(t, s, "connected", func(st huddle.State) bool { return st.Phase == huddle.Connected && len(st.Participants) > 1 })
	e.c.mu.Lock()
	e.c.win = e.w
	e.c.mu.Unlock()
	h := winHost{e.w}
	e.c.shareClick(s, s.State(), h, h.post)
	e.w.runPosts()
	e.c.share.mu.Lock()
	p := e.c.share.picker
	e.c.share.mu.Unlock()
	if p == nil {
		t.Fatal("no picker after the click")
	}
	if k := e.c.shareCtl(s.State()); !k.on {
		t.Error("the button isn't lit while the picker is open")
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		p.mu.Lock()
		done := p.loaded
		p.mu.Unlock()
		if done {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the picker's sources didn't load")
		}
		time.Sleep(20 * time.Millisecond)
	}
	p.mu.Lock()
	if len(p.screens) != 1 || p.screens[0].src.Name != "Entire Screen" || p.screens[0].src.Thumb == nil || len(p.windows) != 2 {
		t.Fatalf("sources: %d screens, %d windows", len(p.screens), len(p.windows))
	}
	p.mu.Unlock()
	img := shareShot(t, "share-picker-screens", 900, 640, func(gtx C) { e.w.layout(gtx) })
	// the tabs (base-sec) and the empty Share button state: nothing picked
	if p.selected() != nil {
		t.Fatal("a tile is selected before any click")
	}
	ink := pickerInks(true)
	if img != nil {
		if n := pixeltest.Count(img, image.Rect(0, 0, 900*shotScale, 640*shotScale), ink.baseSec, 4); n < 2000 {
			t.Errorf("no picker card (%d px)", n)
		}
	}
	// pick the screen: its tile is selected (otl-hgl-1 border)
	p.mu.Lock()
	p.sel = p.screens[0].src.ID
	p.mu.Unlock()
	img = shareShot(t, "share-picker-selected", 900, 640, func(gtx C) { e.w.layout(gtx) })
	if img != nil {
		if n := pixeltest.Count(img, img.Bounds(), ink.otlHgl, 8); n < 200 {
			t.Errorf("no selected tile border (%d px)", n)
		}
	}
	// the Window tab: the pick clears (onSourceTypeSelected)
	p.mu.Lock()
	p.kind, p.sel = huddle.ShareWindow, ""
	p.mu.Unlock()
	shareShot(t, "share-picker-windows", 900, 640, func(gtx C) { e.w.layout(gtx) })
	// "Share system audio" with huddles_screenshare_share_audio, where
	// the OS can (macOS here)
	old := shareGOOS
	shareGOOS = "darwin"
	defer func() { shareGOOS = old }()
	e.h.app.SetExperimentGroup(ExpShareAudio, "on")
	if !e.c.shareAudioOffered() {
		t.Error("Share system audio not offered on macOS with the experiment on")
	}
	e.h.app.SetExperimentGroup(ExpShareAudio, "off")
	if e.c.shareAudioOffered() {
		t.Error("Share system audio offered with the experiment off")
	}
	e.h.app.SetExperimentGroup(ExpShareAudio, "on")
	p.m.Dismiss()
	shareShot(t, "share-window-idle", 620, 400, func(gtx C) { e.w.layout(gtx) })
	e.c.shareClick(s, s.State(), h, h.post)
	e.w.runPosts()
	e.c.share.mu.Lock()
	p = e.c.share.picker
	e.c.share.mu.Unlock()
	if p == nil {
		t.Fatal("no picker after the second click")
	}
	for deadline := time.Now().Add(10 * time.Second); ; {
		p.mu.Lock()
		done := p.loaded
		p.mu.Unlock()
		if done {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the second picker's sources didn't load")
		}
		time.Sleep(20 * time.Millisecond)
	}
	p.mu.Lock()
	p.sel, p.audio = p.screens[0].src.ID, true
	p.mu.Unlock()
	shareShot(t, "share-picker-audio", 900, 640, func(gtx C) { e.w.layout(gtx) })

	// Share: toggleScreenShare with the source and the checkbox
	if !p.share() {
		t.Fatal("Share did nothing")
	}
	p.m.Dismiss()
	st := waitState(t, s, "sharing", func(st huddle.State) bool {
		return st.Share.State == huddle.ShareOn && e.fk.ContentShare().Frames >= 3
	})
	if st.Share.Source.ID != "screen:1:0" || !st.Share.Audio {
		t.Errorf("shared %+v (audio %v)", st.Share.Source, st.Share.Audio)
	}
	if k := e.c.shareCtl(st); k.icon != "screen-off" || k.tip != "Stop sharing screen" || !k.on {
		t.Errorf("sharing button: %+v", k)
	}
	waitState(t, s, "your share on the stage", func(st huddle.State) bool {
		for _, v := range st.Videos {
			if v.Key == huddle.SelfShareKey {
				return true
			}
		}
		return false
	})
	shareShot(t, "share-window-sharing", 620, 400, func(gtx C) { e.w.layout(gtx) })

	// the presenter's mini panel
	mp := &miniPanel{c: e.c, src: st.Share.Source}
	th := theme.Bare(true)
	sh := theme.NewShaper()
	th.SetShapers(sh, sh)
	panel := shareShot(t, "share-mini-panel", panelW, panelH, func(gtx C) { mp.layout(gtx, th) })
	if panel != nil {
		if n := pixeltest.Count(panel, panel.Bounds(), panelBg, 4); n < panelW*panelH*shotScale*shotScale/3 {
			t.Errorf("mini panel background %d px", n)
		}
		if n := pixeltest.Count(panel, image.Rect((panelW-200)*shotScale, 0, panelW*shotScale, panelH*shotScale), panelStop, 8); n < 1500 {
			t.Errorf("no Stop Sharing button at the panel's right (%d px)", n)
		}
	}
	// .p-huddle_mini_panel is -webkit-app-region: drag and its buttons
	// no-drag: the background moves the window, the buttons don't
	// (the buttons from the right: 12dp in, Stop Sharing, then the video
	// and mic buttons, 44dp wide, 12dp apart)
	var pops op.Ops
	pgtx := shotCtx(&pops, panelW, panelH)
	mp.layout(pgtx, th)
	var rt input.Router
	rt.Frame(&pops)
	stopW := 12 + 20 + 12 + 12 + labelSize(pgtx, th, 15, theme.Bold, nsHuddles.T("Stop Sharing")).X/shotScale
	stopX := panelW - 12 - stopW
	moves := func(x, y int) bool {
		a, ok := rt.ActionAt(f32.Pt(float32(x*shotScale), float32(y*shotScale)))
		return ok && a == system.ActionMove
	}
	for _, pt := range []image.Point{{16, 8}, {200, panelH / 2}, {panelW / 2, panelH - 4}, {stopX - 6, panelH / 2}, {stopX - 12 - 44 - 6, panelH / 2}} {
		if !moves(pt.X, pt.Y) { // the edges, the activity, the gaps between buttons
			t.Errorf("the panel at %v isn't its drag region", pt)
		}
	}
	for _, x := range []int{stopX + stopW/2, stopX - 12 - 22, stopX - 12 - 44 - 12 - 22} { // Stop Sharing, video, mic
		if moves(x, panelH/2) {
			t.Errorf("the button at x=%d moves the window", x)
		}
	}
	line, sub := e.c.activityText(st, nil)
	if line == "" || sub != "2 people in the huddle" {
		t.Errorf("activity %q / %q", line, sub)
	}
	// HuddleActivity's joiningMembers: who joins while the panel is up
	// shows for 2s after the last of them; who was there when it opened
	// doesn't
	jp := &miniPanel{c: e.c}
	t0 := time.Now()
	you := huddle.Participant{UserID: huddle.FakeSelfUser, Self: true}
	alone := huddle.State{Channel: st.Channel, Participants: []huddle.Participant{you}}
	if j := jp.joins(alone, t0); len(j) != 0 {
		t.Errorf("joined at open: %v", j)
	}
	two := alone
	two.Participants = []huddle.Participant{you, {UserID: huddle.FakeRemoteUser}}
	j := jp.joins(two, t0.Add(time.Second))
	want := e.c.userName(huddle.FakeRemoteUser) + " joined"
	if line, _ := e.c.activityText(two, j); len(j) != 1 || line != want {
		t.Errorf("one joined: %v, %q (want %q)", j, line, want)
	}
	three := two
	three.Participants = append(append([]huddle.Participant(nil), two.Participants...), huddle.Participant{UserID: "U0THIRD"})
	j = jp.joins(three, t0.Add(1500*time.Millisecond))
	if line, _ := e.c.activityText(three, j); line != "Several people joined" {
		t.Errorf("two joined: %v, %q", j, line)
	}
	if j = jp.joins(three, t0.Add(3400*time.Millisecond)); len(j) != 2 {
		t.Errorf("cleared before 2s after the last join: %v", j)
	}
	if j = jp.joins(three, t0.Add(3600*time.Millisecond)); len(j) != 0 {
		t.Errorf("still joining 2.1s after the last join: %v", j)
	}
	if _, sub := e.c.activityText(alone, nil); sub != "Just you in the huddle" {
		t.Errorf("alone: %q", sub)
	}
	if r := panelPlace(image.Rect(0, 0, 1920, 1080), 1); r != image.Rect(650, 920, 1270, 980) {
		t.Errorf("panel placed at %v on a 1920×1080 screen", r)
	}

	// Stop Sharing
	e.c.stopShare(s)
	waitState(t, s, "share off", func(st huddle.State) bool { return st.Share.State == huddle.ShareOff })
	deadline = time.Now().Add(10 * time.Second)
	for e.fk.ContentShare().Leaves < 1 {
		if time.Now().After(deadline) {
			t.Fatal("the content attendee didn't leave")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if k := e.c.shareCtl(s.State()); k.icon != "screen" || k.on {
		t.Errorf("after stopping: %+v", k)
	}

	// the alerts: noSystemScreenShare (macOS's text with its link) and
	// ScreenShareNotReadableError, in the window and above the dock
	e.c.setShareAlert(alertNoScreenShare)
	parts, link := alertParts(alertNoScreenShare)
	var text strings.Builder
	for _, p := range parts {
		text.WriteString(p.Text)
	}
	if got := text.String(); got != "Allow screen recording access in system preferences. If you’ve already granted permissions, quit and reopen "+AppName+"." || link == "" {
		t.Errorf("permission alert %q (link %q)", got, link)
	}
	img = shareShot(t, "share-alert-permission", 620, 400, func(gtx C) { e.w.layout(gtx) })
	if img != nil {
		if n := pixeltest.Count(img, image.Rect(0, 30*shotScale, 620*shotScale, 140*shotScale), hudWarnBg, 6); n < 2000 {
			t.Errorf("no alert banner (%d px)", n)
		}
	}
	e.c.mu.Lock()
	e.c.win = nil
	e.c.mu.Unlock()
	shareShot(t, "share-alert-dock", 1000, 140, func(gtx C) {
		theme.Fill(gtx, image.Rectangle{Max: gtx.Constraints.Max}, hudFrame)
		e.c.LayoutDock(gtx, e.h.th)
	})
	e.c.setShareAlert(alertShareBusy)
	parts, _ = alertParts(alertShareBusy)
	if len(parts) == 0 || !strings.HasPrefix(parts[0].Text, "You may be screensharing in another app.") {
		t.Errorf("busy alert %+v", parts)
	}
	e.c.mu.Lock()
	e.c.win = e.w
	e.c.mu.Unlock()
	shareShot(t, "share-alert-busy", 620, 400, func(gtx C) { e.w.layout(gtx) })
	e.c.setShareAlert("")
}

// shareShot is render where there's a GPU (CLACK_PIXEL_TESTS), saved under
// CLACK_SHOTS; elsewhere it lays the frame out twice and returns nil, so
// the flow above runs in plain go test too.
func shareShot(t *testing.T, name string, w, h int, f func(gtx C)) *image.RGBA {
	t.Helper()
	if os.Getenv("CLACK_PIXEL_TESTS") != "" {
		return render(t, name, w, h, f)
	}
	var ops op.Ops
	for i := 0; i < 2; i++ {
		ops.Reset()
		f(shotCtx(&ops, w, h))
	}
	return nil
}
