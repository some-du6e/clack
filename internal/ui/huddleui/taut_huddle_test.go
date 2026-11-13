package huddleui

import (
	"context"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/huddle"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/taut"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// Taut's huddle plugins on Clack's real call: the published HuddleActivity
// bundle (TAUT_PLUGINS_DIR, built unmodified by the ui-taut workflow) over
// the fake Chime meeting (loopback only, nothing reaches Slack). People
// join, mute someone, mute you and leave; the huddle is renamed. The
// plugin's log lines show in the huddle window's Captions panel and its
// toasts reach Clack's toast.

const (
	thirdUser = "U0THIRD001" // a third person who joins the fake meeting
	remoteNm  = "Robin Remote"
	thirdNm   = "Tess Third"
)

type tautCallEnv struct {
	*hcEnv
	tt *taut.Taut
}

func newTautCallEnv(t *testing.T) *tautCallEnv {
	t.Helper()
	dir := os.Getenv("TAUT_PLUGINS_DIR")
	if dir == "" {
		t.Skip("TAUT_PLUGINS_DIR not set (the ui-taut workflow builds Taut's plugins)")
	}
	c := slack.New(slack.Creds{Token: "xoxc-test", Cookie: "xoxd-test", UserID: huddle.FakeSelfUser, Team: "T0TESTTEAM",
		TeamURL: "http://127.0.0.1:1/", EnterpriseURL: "http://127.0.0.1:1/"})
	c.EdgeURL = "http://127.0.0.1:1/edge/"
	st := store.New("")
	st.PutUsers([]slack.User{
		{ID: huddle.FakeSelfUser, Name: "me", RealName: "Test Self", Profile: slack.Profile{RealName: "Test Self", DisplayName: "Test Self"}},
		{ID: huddle.FakeRemoteUser, Name: "remote", RealName: remoteNm, Profile: slack.Profile{RealName: remoteNm, DisplayName: remoteNm}},
		{ID: thirdUser, Name: "third", RealName: thirdNm, Profile: slack.Profile{RealName: thirdNm, DisplayName: thirdNm}},
	})
	st.PutConv(slack.Conversation{ID: hcChannel, Name: "huddle-test", IsChannel: true, IsMember: true, NumMembers: 3})
	app := core.New(c, st)
	if u, ok := st.User(huddle.FakeSelfUser); ok {
		app.S.Self = *u
	}
	app.ServeOffline()

	e := &tautCallEnv{hcEnv: &hcEnv{t: t, trees: map[uint64][]byte{}}}
	tdir := t.TempDir()
	tt, err := taut.New(taut.Options{App: app, Dir: filepath.Join(tdir, "taut"), CredsPath: filepath.Join(tdir, "creds.json"),
		NoAutoStart: true, CacheDir: filepath.Join(tdir, "cache"),
		UI: taut.UIHooks{
			Attached: func(b *taut.Bridge) { slackc.SetBridge(b) },
			Deliver: func(root uint64, tree []byte) {
				e.mu.Lock()
				e.trees[root] = append([]byte(nil), tree...)
				e.mu.Unlock()
				_ = slackc.Deliver(root, tree)
			},
			SetPatched: slackc.SetPatched, CallGo: slackc.CallGo, SetStyle: slackc.SetStyle, RemoveStyle: slackc.RemoveStyle,
			Toast: e.toast,
		},
		Log: func(level, msg string) {
			if level == "error" || level == "warn" {
				t.Logf("js %s: %s", level, msg)
			}
		}})
	if err != nil {
		t.Fatal(err)
	}
	slackc.SetBridge(tt.Bridge())
	t.Cleanup(func() {
		_ = tt.Close()
		slackc.Reset()
		slackc.SetBridge(nil)
		slackc.SetPatched(nil)
	})
	e.tt = tt
	e.h = &hcHost{app: app, th: theme.New(false), tt: tt}
	e.c = New(e.h)
	e.c.SelfID = huddle.FakeSelfUser
	w := &window{c: e.c, clicks: map[string]*widget.Clickable{}}
	w.th = theme.Bare(true)
	sh := theme.NewShaper()
	w.th.SetShapers(sh, sh)
	e.w = w
	e.c.mu.Lock()
	e.c.win = w // the window's banners (no OS window: w.w stays nil)
	e.c.mu.Unlock()
	return e
}

func (e *tautCallEnv) installTaut(name string) {
	e.t.Helper()
	code, err := os.ReadFile(filepath.Join(os.Getenv("TAUT_PLUGINS_DIR"), name+".js"))
	if err != nil {
		e.t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	id, err := e.tt.Import(ctx, code, "")
	if err != nil {
		e.t.Fatalf("import %s: %v", name, err)
	}
	if err := e.tt.SetEnabled(ctx, id, true); err != nil {
		e.t.Fatalf("enable %s: %v", name, err)
	}
}

// joinTaut joins the fake meeting as Controller.Start does (without an OS
// window or sound card), binding the session to taut (bindTaut).
func (e *tautCallEnv) joinTaut() (*huddle.Fake, *huddle.Session) {
	e.t.Helper()
	clip, err := os.ReadFile(filepath.Join("..", "..", "huddle", "testdata", "testsrc-320x180.h264"))
	if err != nil {
		e.t.Fatal(err)
	}
	fk := huddle.NewFake(clip, e.t.Errorf)
	e.t.Cleanup(fk.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s, err := huddle.Join(ctx, e.c.testOptions(fk), hcChannel)
	if err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(s.Leave)
	e.c.mu.Lock()
	e.c.s = s
	e.c.mu.Unlock()
	e.c.bindTaut(e.tt, s, hcChannel)
	return fk, s
}

// testOptions are the session options a test join uses: the fake's
// caller, a tone microphone, and the controller's own peer handling.
func (c *Controller) testOptions(fk *huddle.Fake) huddle.Options {
	return huddle.Options{Client: fk.Caller(), SelfID: huddle.FakeSelfUser, Device: huddle.NewToneDevice(440),
		Logf: func(string, ...any) {}, OnChange: c.onSession, OnPeer: c.onPeerEvent}
}

// logHas reports whether the log, as plugins present it, has an event of
// typ (by memberId) whose raw JSON has every sub.
func (e *tautCallEnv) logHas(typ, member string, subs ...string) bool {
	v, ok := e.tt.HuddleEvents()
	if !ok {
		return false
	}
	for _, raw := range v.Events {
		var ev struct {
			Type     string `json:"type"`
			MemberID string `json:"memberId"`
		}
		if json.Unmarshal(raw, &ev) != nil || ev.Type != typ || (member != "" && ev.MemberID != member) {
			continue
		}
		s, all := string(raw), true
		for _, sub := range subs {
			if !strings.Contains(s, sub) {
				all = false
			}
		}
		if all {
			return true
		}
	}
	return false
}

func (e *tautCallEnv) dumpLog() {
	v, _ := e.tt.HuddleEvents()
	b, _ := json.Marshal(v)
	e.t.Logf("huddleEvents as plugins see it: %s", b)
	e.tmu.Lock()
	e.t.Logf("toasts: %q", e.toasts)
	e.tmu.Unlock()
}

func (e *tautCallEnv) wait(what string, d time.Duration, cond func() bool) {
	e.t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		e.frame()
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	e.dumpLog()
	e.until(what, 0, cond) // logs the trees and fails
}

func roomUpdate(name string) slack.Event {
	room := map[string]any{"id": huddle.FakeRoomID, "channels": []string{hcChannel}, "name": name}
	raw, _ := json.Marshal(map[string]any{"type": "sh_room_update", "user": huddle.FakeRemoteUser, "room": room})
	return slack.Event{Type: "sh_room_update", Raw: raw}
}

// HuddleActivity: it keeps the event log open in every huddle (Slack's
// enableEventLog, which turns the session's log on without
// transcription), forces the side-by-side view on so the log's Captions
// panel shows, and adds its own lines: who muted whom, who muted you,
// renames. Slack's own JOIN and LEAVE lines come from the session.
func TestTautHuddleActivity(t *testing.T) {
	e := newTautCallEnv(t)
	e.installTaut("HuddleActivity")
	e.wait("HuddleActivity's patches", 10*time.Second, func() bool {
		return slackc.Patched("HuddleMetaEvent") && slackc.Patched("HuddleBaseEvent") && slackc.Patched("HuddlesAlertBannerBase")
	})
	fk, s := e.joinTaut()
	e.wait("the event log kept open (enableEventLog reached the session)", 15*time.Second, func() bool {
		on, _ := s.EventLog()
		return on
	})
	e.wait("the Captions panel (side-by-side forced on)", 10*time.Second, func() bool {
		v, ok := e.tt.HuddleEvents()
		gtx := layout.Context{Ops: new(op.Ops), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(960, 640))}
		return ok && v.Enabled && v.IsSideBySideEnabled && e.w.captionsPanelW(gtx) > 0
	})
	if on, _ := s.EventLog(); !on {
		t.Fatal("the log isn't on")
	}
	if calls := fk.Calls(); len(calls) > 0 {
		for _, c := range calls {
			if c.Method == "rooms.startTranscription" {
				t.Errorf("keeping the log open started a transcription: %v", c)
			}
		}
	}

	// someone joins: Slack's JOIN, drawn by HuddleMetaEvent
	if err := fk.PeerJoin(thirdUser); err != nil {
		t.Fatal(err)
	}
	e.wait("the JOIN line", 10*time.Second, func() bool { return e.logHas("JOIN", thirdUser) })
	e.wait("the log's row drawn through HuddleMetaEvent and HuddleBaseEvent", 10*time.Second, func() bool {
		return e.tree(`"n":"HuddleMetaEvent"`) && e.tree(`"n":"HuddleBaseEvent"`)
	})

	// the other person mutes the third: the plugin's line and its toast
	third := huddle.FakeAttendee(thirdUser)
	if err := fk.PeerMessage(huddle.FakeRemoteAttendee, &huddle.PeerMessage{MuteRequest: &huddle.MuteRequest{PeerID: third}}); err != nil {
		t.Fatal(err)
	}
	e.wait("“muted Tess Third.” in the log", 10*time.Second, func() bool {
		return e.logHas("JOIN", huddle.FakeRemoteUser, `"tautNote"`, `"muted "`, thirdUser)
	})
	e.wait("its line drawn (the plugin's .p-huddle_event_log__meta_text)", 10*time.Second, func() bool {
		return e.tree("p-huddle_event_log__meta_text", "muted ")
	})
	e.wait("the toast “Robin Remote muted Tess Third.”", 10*time.Second, func() bool {
		return e.toastWith(remoteNm + " muted " + thirdNm + ".")
	})

	// the other person mutes you: Slack's displayAlertMessage (peerMuteRequest),
	// 3 s later the banner, which the plugin names; with two others, a toast
	if err := fk.PeerMessage(huddle.FakeRemoteAttendee, &huddle.PeerMessage{MuteRequest: &huddle.MuteRequest{PeerID: s.SelfAttendee()}}); err != nil {
		t.Fatal(err)
	}
	e.wait("you're muted", 10*time.Second, func() bool { return s.State().Muted })
	e.wait("“muted you.” in the log", 15*time.Second, func() bool {
		return e.logHas("JOIN", huddle.FakeRemoteUser, `"muted you."`)
	})
	e.wait("the toast “Robin Remote muted you.”", 10*time.Second, func() bool { return e.toastWith(remoteNm + " muted you.") })
	e.wait("the banner naming who muted you", 15*time.Second, func() bool {
		return e.tree(`"n":"HuddlesAlertBannerBase"`, "muted you.")
	})

	// the huddle is renamed (the room's sh_room_update): a line and a toast
	e.h.app.Inject(roomUpdate("Standup"))
	e.frame()
	time.Sleep(200 * time.Millisecond)
	e.h.app.Inject(roomUpdate("Retro"))
	e.wait("“renamed the huddle” in the log", 10*time.Second, func() bool {
		return e.logHas("JOIN", huddle.FakeRemoteUser, `renamed the huddle to \"Retro\".`)
	})
	e.wait("the rename toast", 10*time.Second, func() bool { return e.toastWith(remoteNm, `renamed the huddle to "Retro".`) })

	// the third person leaves: Slack's LEAVE
	if err := fk.PeerLeave(third); err != nil {
		t.Fatal(err)
	}
	e.wait("the LEAVE line", 10*time.Second, func() bool { return e.logHas("LEAVE", thirdUser) })
	e.dumpLog()

	// light and dark pictures of the window with its Captions panel
	t.Run("shots", func(t *testing.T) {
		for _, dark := range []bool{false, true} {
			mode := map[bool]string{false: "light", true: "dark"}[dark]
			e.h.th = theme.New(dark)
			e.frame()
			img := renderHuddleWindow(t, e.hcEnv, 960, 600)
			saveTautShot(t, "huddle-activity-"+mode, img)
		}
	})
}

// renderHuddleWindow draws the huddle window w×h dp (at shotScale), as
// its own frame does, on the GPU.
func renderHuddleWindow(t *testing.T, e *hcEnv, w, h int) *image.RGBA {
	t.Helper()
	var ops op.Ops
	for i := 0; i < 3; i++ {
		ops.Reset()
		gtx := shotCtx(&ops, w, h)
		slackc.LockFrame()
		restore := slackc.SubFrame(gtx, "p-multi_view_window p-huddle_window", hudWhite)
		e.w.layout(gtx)
		restore()
		slackc.Frame(gtx)
		slackc.UnlockFrame()
		time.Sleep(50 * time.Millisecond)
	}
	return pixeltest.Render(t, w*shotScale, h*shotScale, &ops)
}

// saveTautShot writes img to TAUT_SHOTS_DIR (the ui-taut workflow uploads it).
func saveTautShot(t *testing.T, name string, img *image.RGBA) {
	t.Helper()
	dir := os.Getenv("TAUT_SHOTS_DIR")
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(dir, name+".png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}
