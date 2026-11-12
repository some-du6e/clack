package huddleui

import (
	"context"
	"encoding/json"
	"errors"
	"image"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/op"

	"github.com/some-du6e/clack/internal/huddle"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/popups"
)

// gap-w9: the huddle toolbar's segmented camera button and its Video
// settings menu (LnqA, DU15), Leave options' End huddle for all (4tmx,
// OsUf, 8ksj, PdsW) and the emoji button's quick picker with its
// Reactions tab (Vw0B). Slack's modules from build d3de3a487b1c.

// menuShape is a menu's rows as text: [header], ---, label (* checked).
func menuShape(items []popups.MenuItem) string {
	var out []string
	for _, it := range items {
		switch {
		case it.Separator:
			out = append(out, "---")
		case it.Header != "":
			out = append(out, "["+it.Header+"]")
		default:
			l := it.Label
			if it.Checked {
				l += "*"
			}
			if it.Danger {
				l += " (danger)"
			}
			out = append(out, l)
		}
	}
	return strings.Join(out, " | ")
}

func ctlShape(cs []ctl) string {
	var out []string
	for _, c := range cs {
		s := c.id + ":" + c.icon
		if c.on {
			s += "+on"
		}
		switch c.seg {
		case segMain:
			s += "/main"
		case segCaret:
			s += "/caret"
		}
		out = append(out, s+" «"+c.tip+"»")
	}
	return strings.Join(out, " ")
}

// HuddleToolbarCameraButton: the camera half and the Video settings caret;
// I4aw's videoLabel is the tooltip; 7d+n's video-filled while on; the
// caret is on while its menu is open or the camera is on. 9d6D draws the
// button only when isHuddlesVideoEnabledForOrgWorkspaces holds.
func TestCameraButtonLikeSlack(t *testing.T) {
	e := newShotEnv(t, false)
	old := cameraSupported
	cameraSupported = func() bool { return true }
	t.Cleanup(func() { cameraSupported = old })
	cases := []struct {
		cam  huddle.CameraState
		open bool
		want string
	}{
		{huddle.CameraOff, false, "camera:video-off/main «Turn on video» videomenu:caret-up/caret «Video settings»"},
		{huddle.CameraOff, true, "camera:video-off/main «Turn on video» videomenu:caret-down+on/caret «Video settings»"},
		{huddle.CameraStarting, false, "camera:video/main «Turn off video» videomenu:caret-up/caret «Video settings»"},
		{huddle.CameraOn, false, "camera:video-filled+on/main «Turn off video» videomenu:caret-up+on/caret «Video settings»"},
	}
	for _, k := range cases {
		st := huddle.State{Camera: huddle.CameraStatus{State: k.cam}}
		if got := ctlShape(e.c.cameraCtls(st, k.open)); got != k.want {
			t.Errorf("%v open=%v:\n got %s\nwant %s", k.cam, k.open, got, k.want)
		}
	}
	// allow_huddles_video: off hides the button; a loaded set without it
	// keeps it (Clack can't tell a pref team.prefs.get leaves out)
	e.h.app.SetTeamPrefs(map[string]json.RawMessage{"allow_huddles_video": json.RawMessage(`false`)})
	if got := e.c.cameraCtls(huddle.State{}, false); len(got) != 0 {
		t.Errorf("allow_huddles_video false: %s", ctlShape(got))
	}
	// an external guest in an external huddle gets video anyway (cB5s U)
	e.h.app.SetExperimentGroup(extHuddles, "on")
	me, _ := e.h.app.S.User(huddle.FakeSelfUser)
	g := *me
	g.IsRestricted, g.RestrictedUserType = true, "external_guest"
	e.h.app.S.PutUsers([]slack.User{g})
	if got := e.c.cameraCtls(huddle.State{}, false); len(got) != 2 {
		t.Errorf("external guest: %s", ctlShape(got))
	}
	e.h.app.S.PutUsers([]slack.User{*me})
	e.h.app.SetExperimentGroup(extHuddles, "")
	e.h.app.SetTeamPrefs(map[string]json.RawMessage{"allow_huddles_video": json.RawMessage(`true`)})
	if got := e.c.cameraCtls(huddle.State{}, false); len(got) != 2 {
		t.Errorf("allow_huddles_video true: %s", ctlShape(got))
	}
	e.h.app.SetTeamPrefs(map[string]json.RawMessage{})
	if got := e.c.cameraCtls(huddle.State{}, false); len(got) != 2 {
		t.Errorf("prefs without allow_huddles_video: %s", ctlShape(got))
	}
}

// HuddleVideoDevicesMenu: the Video header and useCameraSettingsMenuItems'
// radio rows (or "No cameras found"), a separator, then
// useAVPreferencesSettingsMenuItem (not for an external guest).
func TestVideoMenuLikeSlack(t *testing.T) {
	e := newShotEnv(t, false)
	old := listCameras
	t.Cleanup(func() { listCameras = old })
	listCameras = func() []huddle.VideoDevice { return []huddle.VideoDevice{{ID: "cam-1", Name: "FaceTime HD Camera"}} }
	if got, want := menuShape(e.c.videoMenu(nil)), "[Video] | FaceTime HD Camera* | --- | Audio & video preferences"; got != want {
		t.Errorf("one camera:\n got %s\nwant %s", got, want)
	}
	listCameras = func() []huddle.VideoDevice { return nil }
	if got, want := menuShape(e.c.videoMenu(nil)), "[Video] | No cameras found | --- | Audio & video preferences"; got != want {
		t.Errorf("no camera:\n got %s\nwant %s", got, want)
	}
	for _, it := range e.c.videoMenu(nil) {
		if it.Label != "" && !it.Radio {
			t.Errorf("%q isn't a radio row (Slack's type: \"radio\")", it.Label)
		}
	}
	e.h.app.SetTeamPrefs(map[string]json.RawMessage{"allow_huddles_video": json.RawMessage(`false`)})
	if got, want := menuShape(e.c.videoMenu(nil)), "Audio & video preferences"; got != want {
		t.Errorf("video off:\n got %s\nwant %s", got, want)
	}
	// the preferences row opens Preferences › Audio & video
	e.h.app.SetTeamPrefs(map[string]json.RawMessage{})
	ph := &prefsHost{hcHost: *e.h}
	e.c.h = ph
	for _, it := range e.c.videoMenu(nil) {
		if it.Label == "Audio & video preferences" {
			it.Do()
		}
	}
	deadline := time.Now().Add(5 * time.Second)
	for ph.section() == "" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := ph.section(); got != "video_audio" {
		t.Errorf("Audio & video preferences opened %q", got)
	}
}

// prefsHost records OpenPreferences (and runs Post at once).
type prefsHost struct {
	hcHost
	mu sync.Mutex
	at string
}

func (h *prefsHost) Post(f func()) { f() }
func (h *prefsHost) OpenPreferences(section string) {
	h.mu.Lock()
	h.at = section
	h.mu.Unlock()
}
func (h *prefsHost) section() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.at
}

// canEndExternalHuddleForAll (8ksj j): external_huddles on, you aren't a
// guest, an external guest is in the huddle, and you're its host (the
// channel's creator: Clack has no calendar huddles' hostId).
func TestCanEndForAllLikeSlack(t *testing.T) {
	e := newShotEnv(t, false)
	app := e.h.app
	const guestID = "U0EXTGUEST1"
	app.S.PutUsers([]slack.User{{ID: guestID, Name: "guest", IsRestricted: true, RestrictedUserType: "external_guest"},
		{ID: "U0MEMBER01", Name: "member"}})
	app.S.PutConv(slack.Conversation{ID: hcChannel, Name: "huddle-test", IsChannel: true, IsMember: true, Creator: huddle.FakeSelfUser})
	st := func(others ...string) huddle.State {
		s := huddle.State{Channel: hcChannel, Participants: []huddle.Participant{{UserID: huddle.FakeSelfUser, Self: true}}}
		for _, o := range others {
			s.Participants = append(s.Participants, huddle.Participant{UserID: o})
		}
		return s
	}
	if e.c.canEndForAll(st(guestID)) {
		t.Error("without external_huddles")
	}
	app.SetExperimentGroup(extHuddles, "on")
	if !e.c.canEndForAll(st(guestID)) {
		t.Error("host with an external guest: no Leave options")
	}
	if e.c.canEndForAll(st("U0MEMBER01")) {
		t.Error("no external guest in the huddle")
	}
	app.S.PutConv(slack.Conversation{ID: hcChannel, Name: "huddle-test", IsChannel: true, IsMember: true, Creator: "U0MEMBER01"})
	if e.c.canEndForAll(st(guestID)) {
		t.Error("not the channel's creator")
	}
	app.S.PutConv(slack.Conversation{ID: hcChannel, Name: "huddle-test", IsChannel: true, IsMember: true, Creator: huddle.FakeSelfUser})
	me, _ := app.S.User(huddle.FakeSelfUser)
	g := *me
	g.IsRestricted = true
	app.S.PutUsers([]slack.User{g})
	if e.c.canEndForAll(st(guestID)) {
		t.Error("you're a guest (isCurrentUserRestricted)")
	}
}

// failOneCaller fails one method and passes the rest to the fake.
type failOneCaller struct {
	huddle.Caller
	method string
}

func (f failOneCaller) Call(ctx context.Context, method string, p url.Values, out any) error {
	if method == f.method {
		return errors.New("fake slack: " + method + " failed")
	}
	return f.Caller.Call(ctx, method, p, out)
}

// toastHost records toasts.
type toastHost struct {
	overlayHost
	toasts chan string
}

func (h *toastHost) Toast(s string) { h.toasts <- s }

// endHuddleEnv is the fake huddle with an external guest in it, you the
// channel's creator and external_huddles on.
func endHuddleEnv(t *testing.T) (*shotEnv, *huddle.Session, *toastHost) {
	t.Helper()
	e := newShotEnv(t, true)
	app := e.h.app
	app.SetExperimentGroup(extHuddles, "on")
	remote, _ := app.S.User(huddle.FakeRemoteUser)
	g := *remote
	g.IsRestricted, g.RestrictedUserType = true, "external_guest"
	app.S.PutUsers([]slack.User{g})
	app.S.PutConv(slack.Conversation{ID: hcChannel, Name: "huddle-test", IsChannel: true, IsMember: true, NumMembers: 2, Creator: huddle.FakeSelfUser})
	th := &toastHost{overlayHost: overlayHost{hcHost: *e.h}, toasts: make(chan string, 4)}
	e.c.h = th
	e.noShare = true
	s := e.join(nil, nil)
	waitState(t, s, "connected with the guest", func(st huddle.State) bool {
		return st.Phase == huddle.Connected && len(st.Participants) > 1
	})
	return e, s, th
}

// Leave options: the window's Leave is segmented, the caret opens
// HuddleLeaveOptionsMenu, End huddle for all calls huddles.external.end
// with the channel, then stopHuddle; a failure toasts and you stay.
func TestEndHuddleForAll(t *testing.T) {
	e, s, th := endHuddleEnv(t)
	var ops op.Ops
	e.w.layout(shotCtx(&ops, 620, 400))
	if e.w.leaveBtn.Dx() != leaveCaretW*shotScale {
		t.Fatalf("leave caret %v, want %d dp wide", e.w.leaveBtn, leaveCaretW)
	}
	e.w.click("leavemenu").Click()
	e.w.layout(shotCtx(&ops, 620, 400))
	if e.w.leaveMenu == nil {
		t.Fatal("the caret didn't open Leave options")
	}
	if got, want := menuShape(e.c.leaveOptionsMenu(s)), "End huddle for all (danger)"; got != want {
		t.Fatalf("Leave options %q, want %q", got, want)
	}

	// a failure: the toast, and you're still in the huddle
	e.c.Client = failOneCaller{Caller: e.fk.Caller(), method: "huddles.external.end"}
	e.c.EndForAll(s)
	select {
	case got := <-th.toasts:
		if got != "Can’t end the huddle for everyone. Try again." {
			t.Errorf("toast %q", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no toast after huddles.external.end failed")
	}
	if e.c.Session() == nil {
		t.Fatal("left the huddle after huddles.external.end failed")
	}

	e.c.Client = e.fk.Caller()
	e.c.EndForAll(s)
	var end *huddle.FakeCall
	for _, c := range e.fk.Calls() {
		if c.Method == "huddles.external.end" {
			c := c
			end = &c
		}
	}
	if end == nil || end.Params.Get("channel_id") != hcChannel {
		t.Fatalf("huddles.external.end %+v, want channel_id %s", end, hcChannel)
	}
	if e.c.Session() != nil {
		t.Fatal("still in the huddle after End huddle for all")
	}
}

// The emoji button opens HuddleQuickEmojiPicker above it: 360px wide, the
// Reactions tab over a 200px panel in the (compact) huddle window and
// 260px from the docked bar; a pick reacts; the close button closes it.
func TestQuickPickerLikeSlack(t *testing.T) {
	e, s := windowShotEnv(t, true, 2)
	var ops op.Ops
	e.w.layout(shotCtx(&ops, 589, 351))
	if e.w.reactBtn.Empty() {
		t.Fatal("no emoji button in the toolbar")
	}
	e.w.click("react").Click()
	e.w.layout(shotCtx(&ops, 589, 351))
	q := e.w.quick
	if q == nil || !q.compact {
		t.Fatalf("quick picker %+v, want it open in compact mode", q)
	}
	gtx := shotCtx(&ops, 589, 351)
	if got, want := q.size(gtx), image.Pt(360*shotScale, (qpTabMenuPad+qpTabH+qpPanelHC)*shotScale); got != want {
		t.Fatalf("card %v, want %v", got, want)
	}
	e.w.layout(shotCtx(&ops, 589, 351))
	// above the button, 5px off it (the popover's offsetY -5)
	if want := e.w.reactBtn.Min.Y + e.w.barTop - qpOffsetY*shotScale; q.card.Max.Y != want {
		t.Errorf("card %v ends at %d, want %d (button %v, bar at %d)", q.card, q.card.Max.Y, want, e.w.reactBtn, e.w.barTop)
	}
	strs := hudStrings(hudSurface{w: 589, h: 351, draw: func(gtx C) { e.w.layout(gtx) }})
	for _, want := range []string{"Reactions", "Search all emoji"} {
		found := false
		for _, s := range strs {
			found = found || s == want
		}
		if !found {
			t.Errorf("the quick picker didn't draw %q (drew %q)", want, strs)
		}
	}
	q.react("tada")
	waitState(t, s, "your reaction", func(st huddle.State) bool {
		for _, p := range st.Participants {
			if p.Self {
				return p.Reaction == "tada"
			}
		}
		return false
	})
	q.close.Click()
	e.w.layout(shotCtx(&ops, 589, 351))
	if e.w.quick != nil {
		t.Fatal("the close button didn't close the quick picker")
	}

	// the docked bar: the same card over the main window, 260px panel
	oh := &overlayHost{hcHost: *e.h}
	e.c.h = oh
	st := s.State()
	e.c.dockClick(shotCtx(&ops, 1000, 100), "react", s, st, image.Rect(500, 10, 540, 50))
	if len(oh.shown) != 1 {
		t.Fatalf("the dock's emoji button showed %d overlays", len(oh.shown))
	}
	dq, ok := oh.shown[0].(*quickPicker)
	if !ok || dq.compact {
		t.Fatalf("dock overlay %T compact=%v", oh.shown[0], ok && dq.compact)
	}
	if got := dq.size(gtx).Y; got != (qpTabMenuPad+qpTabH+qpPanelH)*shotScale {
		t.Errorf("dock card %d px tall, want the 260px panel", got)
	}
}
