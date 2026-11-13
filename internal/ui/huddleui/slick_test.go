package huddleui

import (
	"context"
	"image"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/huddle"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/slick"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/taut"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Slick's huddle plugins on Clack's real call UI: a fake Chime meeting
// (huddle.NewFake, loopback only: another person talking and sharing
// their screen), the native session in the controller, the huddle
// window's layout and the sidebar bar drawn headlessly, and the real
// built plugins (SLICK_PLUGINS_DIR, the ui-slick workflow).

const hcChannel = "C0TEST" // the fake meeting's channel

type hcHost struct {
	app  *core.App
	th   *theme.Theme
	tt   *taut.Taut
	imgs *imgcache.Cache // nil: no pictures
}

func (h *hcHost) Core() *core.App           { return h.app }
func (h *hcHost) Theme() *theme.Theme       { return h.th }
func (h *hcHost) Images() *imgcache.Cache   { return h.imgs }
func (h *hcHost) Invalidate()               {}
func (h *hcHost) Navigate(string)           {}
func (h *hcHost) OpenThread(string, string) {}
func (h *hcHost) Toast(string)              {}
func (h *hcHost) Taut() *taut.Taut          { return h.tt }

type hcEnv struct {
	t     *testing.T
	c     *Controller
	w     *window
	h     *hcHost
	s     *slick.Slick
	mu    sync.Mutex
	trees map[uint64][]byte
	ops   op.Ops

	tmu    sync.Mutex
	toasts []string // enqueueHuddleToast's texts
}

func (e *hcEnv) toast(text, icon string, d time.Duration) {
	e.tmu.Lock()
	e.toasts = append(e.toasts, text)
	e.tmu.Unlock()
}

// toastWith reports whether some toast had every sub.
func (e *hcEnv) toastWith(subs ...string) bool {
	e.tmu.Lock()
	defer e.tmu.Unlock()
	for _, t := range e.toasts {
		ok := true
		for _, s := range subs {
			if !strings.Contains(t, s) {
				ok = false
			}
		}
		if ok {
			return true
		}
	}
	return false
}

func newHuddleCallEnv(t *testing.T) *hcEnv {
	t.Helper()
	dir := os.Getenv("SLICK_PLUGINS_DIR")
	if dir == "" {
		t.Skip("SLICK_PLUGINS_DIR not set (the ui-slick workflow builds Slick's plugins)")
	}
	c := slack.New(slack.Creds{Token: "xoxc-test", Cookie: "xoxd-test", UserID: huddle.FakeSelfUser, Team: "T0266FRGM",
		TeamURL: "http://127.0.0.1:1/", EnterpriseURL: "http://127.0.0.1:1/"})
	c.EdgeURL = "http://127.0.0.1:1/edge/"
	st := store.New("")
	st.PutUsers([]slack.User{
		{ID: huddle.FakeSelfUser, Name: "me", RealName: "Test Self"},
		{ID: huddle.FakeRemoteUser, Name: "remote", RealName: "Remote Example"},
	})
	st.PutConv(slack.Conversation{ID: hcChannel, Name: "huddle-test", IsChannel: true, IsMember: true, NumMembers: 2})
	app := core.New(c, st)
	if u, ok := st.User(huddle.FakeSelfUser); ok {
		app.S.Self = *u
	}
	app.ServeOffline()

	e := &hcEnv{t: t, trees: map[uint64][]byte{}}
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
			if level == "error" || level == "warn" || os.Getenv("CLACK_HUDDLE_JSLOG") != "" {
				t.Logf("js %s: %s", level, msg)
			}
		}})
	if err != nil {
		t.Fatal(err)
	}
	s, err := slick.New(slick.Options{Taut: tt, Dir: filepath.Join(tdir, "slick"), NoAutoStart: true, NoUpdates: true})
	if err != nil {
		t.Fatal(err)
	}
	slackc.SetBridge(tt.Bridge())
	t.Cleanup(func() {
		_ = s.Close()
		_ = tt.Close()
		slackc.Reset()
		slackc.SetBridge(nil)
		slackc.SetPatched(nil)
	})
	main := theme.New(false)
	e.h = &hcHost{app: app, th: main, tt: tt}
	e.s = s
	e.c = New(e.h)
	e.c.SelfID = huddle.FakeSelfUser
	w := &window{c: e.c, clicks: map[string]*widget.Clickable{}}
	w.th = theme.Bare(true)
	sh := theme.NewShaper()
	w.th.SetShapers(sh, sh)
	e.w = w
	return e
}

func (e *hcEnv) install(id string) {
	e.t.Helper()
	code, err := os.ReadFile(filepath.Join(os.Getenv("SLICK_PLUGINS_DIR"), id+".js"))
	if err != nil {
		e.t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := e.s.Import(ctx, code); err != nil {
		e.t.Fatalf("import %s: %v", id, err)
	}
	if err := e.s.SetEnabled(ctx, id, true); err != nil {
		e.t.Fatalf("enable %s: %v", id, err)
	}
	for _, p := range e.s.Settings().Plugins {
		if p.ID == id && (!p.Running || p.StartError != "") {
			e.t.Fatalf("%s didn't start: running=%v error=%q", id, p.Running, p.StartError)
		}
	}
}

// join joins the fake meeting the way Controller.Start does, without an
// OS window or sound card: the session, then taut's active huddle.
func (e *hcEnv) join() *huddle.Fake {
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
	e.c.bindTaut(e.h.tt, s, hcChannel)
	return fk
}

// frame draws the huddle window (its own frame, as run does) and the main
// window's docked huddle bar.
func (e *hcEnv) frame() {
	e.ops.Reset()
	gtx := layout.Context{Ops: &e.ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(960, 640)), Now: time.Now()}
	slackc.LockFrame()
	restore := slackc.SubFrame(gtx, "p-multi_view_window p-huddle_window", hudWhite)
	e.w.layout(gtx)
	restore()
	slackc.Frame(gtx)
	bar := gtx
	bar.Constraints = layout.Constraints{Max: image.Pt(960, 200)}
	e.c.LayoutDock(bar, e.h.th)
	slackc.UnlockFrame()
}

func (e *hcEnv) until(what string, d time.Duration, cond func() bool) {
	e.t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		e.frame()
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	e.tmu.Lock()
	e.t.Logf("toasts: %q", e.toasts)
	e.tmu.Unlock()
	e.mu.Lock()
	for root, tr := range e.trees {
		s := string(tr)
		if len(s) > 1500 {
			s = s[:1500] + "…"
		}
		e.t.Logf("tree %d: %s", root, s)
	}
	e.mu.Unlock()
	e.t.Fatalf("timed out waiting for %s", what)
}

// tree reports whether some delivered tree has all of subs.
func (e *hcEnv) tree(subs ...string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, tr := range e.trees {
		s, ok := string(tr), true
		for _, sub := range subs {
			if !strings.Contains(s, sub) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

// CallTimer: in a live call, its timer follows the huddle window's title
// (HuddleMvaWindowTitle) and sits in the bottom bar's recording indicator
// (HuddleBottomBarContent > HuddleRecordingIndicator), counting from when
// you joined: clientHuddles through Slack's ClientStoreContext.
func TestSlickViewCallTimer(t *testing.T) {
	e := newHuddleCallEnv(t)
	e.install("CallTimer")
	e.until("CallTimer's patches", 5*time.Second, func() bool {
		return slackc.Patched("HuddleMvaWindowTitle") && slackc.Patched("HuddleBottomBarContent") && slackc.Patched("HuddleRecordingIndicator")
	})
	e.join()
	e.until("the timer after the window title", 15*time.Second, func() bool {
		return e.tree(`"n":"HuddleMvaWindowTitle"`, "slick-call-timer")
	})
	e.until("the timer in the bottom bar's recording indicator", 10*time.Second, func() bool {
		return e.tree(`"n":"HuddleRecordingIndicator"`, "slick-call-timer")
	})
}

// LetMeSee: its anchor follows the docked huddle bar (HuddleMiniPanel),
// and the drawing layer over the other person's shared screen
// (BaseDrawRenderer) is wrapped so drawings can be hidden.
func TestSlickViewLetMeSee(t *testing.T) {
	e := newHuddleCallEnv(t)
	e.install("LetMeSee")
	e.until("LetMeSee's patches", 5*time.Second, func() bool {
		return slackc.Patched("HuddleMiniPanel") && slackc.Patched("BaseDrawRenderer")
	})
	e.join()
	e.until("the anchor after the docked huddle bar", 15*time.Second, func() bool {
		return e.tree(`"n":"HuddleMiniPanel"`, `"display":"none"`)
	})
	e.until("the drawing layer over the shared screen, shown", 25*time.Second, func() bool {
		return e.tree(`"n":"BaseDrawRenderer"`, `"display":"contents"`)
	})
}

// HuddleMuteDetector: when someone mutes you, Slack's displayAlertMessage
// (peerMuteRequest, with who did it) reaches its thunk patch, which looks
// them up in the huddle's peers and toasts "{name} muted you.".
func TestSlickViewHuddleMuteDetector(t *testing.T) {
	e := newHuddleCallEnv(t)
	e.install("HuddleMuteDetector")
	fk := e.join()
	e.until("the other person in the huddle's peers (as plugins see them)", 15*time.Second, func() bool {
		return e.h.tt.HuddlePeers()[huddle.FakeRemoteAttendee] == huddle.FakeRemoteUser
	})
	s := e.c.Session()
	if err := fk.PeerMessage(huddle.FakeRemoteAttendee, &huddle.PeerMessage{MuteRequest: &huddle.MuteRequest{PeerID: s.SelfAttendee()}}); err != nil {
		t.Fatal(err)
	}
	e.until("you're muted", 10*time.Second, func() bool { return s.State().Muted })
	t.Logf("peers for plugins: %v; displayAlertMessage patched: %v", e.h.tt.HuddlePeers(), e.h.tt.ThunkPatched("displayAlertMessage"))
	// its name: profile display name, real name, else the username
	e.until("the toast naming who muted you", 15*time.Second, func() bool {
		return e.toastWith("Remote Example muted you.") || e.toastWith("remote muted you.")
	})
}
