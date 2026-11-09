package huddleui

import (
	"context"
	"image"
	"image/color"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/op"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/fakeslack"
	"github.com/some-du6e/yoloslackgoclient/internal/huddle"
	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/store"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/popups"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme/pixeltest"
)

// The huddle menus against the fake Chime and fakeslack: Slack's
// HuddleButtonMenu per state, Copy huddle link through rooms.getLink, and
// a huddle started from the profile card's menu.

// recHost records what the menus do with the ui root (clipboard, toasts,
// the schedule modal) and is a popups.Host so menus can be drawn.
type recHost struct {
	hcHost
	mu       sync.Mutex
	copied   []string
	toasts   []string
	schedule []string
}

func (h *recHost) CopyText(s string) { h.mu.Lock(); h.copied = append(h.copied, s); h.mu.Unlock() }
func (h *recHost) ScheduleHuddle(c string) {
	h.mu.Lock()
	h.schedule = append(h.schedule, c)
	h.mu.Unlock()
}
func (h *recHost) Toast(s string)  { h.mu.Lock(); h.toasts = append(h.toasts, s); h.mu.Unlock() }
func (h *recHost) CloseSecondary() {}
func (h *recHost) ShowOverlay(popups.Overlay) {
}
func (h *recHost) OpenEmojiPicker(image.Point, func(string)) {}
func (h *recHost) OpenProfile(image.Point, string)           {}
func (h *recHost) OpenMessageMenu(image.Point, core.Message) {}
func (h *recHost) OpenLightbox(richtext.Image, core.Message) {}

func (h *recHost) snapshot() (copied, toasts []string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.copied...), append([]string(nil), h.toasts...)
}

var _ popups.Host = (*recHost)(nil)

const dmChannel = "D0TESTDM01"

type menuEnv struct {
	*shotEnv
	mh *recHost
}

// newMenuEnv is newShotEnv with the recording host, a DM with the remote
// person, and Slack's scheduled_huddles experiment set to sched.
func newMenuEnv(t *testing.T, dark, sched bool) menuEnv {
	t.Helper()
	e := newShotEnv(t, dark)
	e.h.app.S.PutConv(slack.Conversation{ID: dmChannel, IsIM: true, User: huddle.FakeRemoteUser})
	if sched {
		e.h.app.SetExperimentGroup("scheduled_huddles", "on")
	} else {
		e.h.app.SetExperimentGroup("scheduled_huddles", "off")
	}
	mh := &recHost{hcHost: *e.h}
	e.c.h = mh
	e.c.NoWindow = true
	return menuEnv{e, mh}
}

// testClip is the fake's H.264 test pattern.
func testClip(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "huddle", "testdata", "testsrc-320x180.h264"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// labels flattens a menu: "# heading", "---", or the label.
func labels(items []popups.MenuItem) string {
	var out []string
	for _, it := range items {
		switch {
		case it.Separator:
			out = append(out, "---")
		case it.Header != "":
			out = append(out, "# "+it.Header)
		default:
			out = append(out, it.Label)
		}
	}
	return strings.Join(out, " | ")
}

func (e menuEnv) setLive(ch string, people ...string) {
	e.c.mu.Lock()
	e.c.live[ch] = liveRoom{ID: "R1", Channel: ch, Participants: people}
	e.c.mu.Unlock()
}

// Slack's HuddleButtonMenu (uw9r) in each state.
func TestHeaderMenuStates(t *testing.T) {
	const sched = "# Huddle | %s | Copy huddle link | --- | # Schedule for later | Schedule Huddle"
	flat := newMenuEnv(t, false, false)
	if got := labels(flat.c.MenuItems(hcChannel)); got != "Start Huddle | Copy huddle link" {
		t.Errorf("no scheduled_huddles, idle: %q", got)
	}

	e := newMenuEnv(t, false, true)
	if got, want := labels(e.c.MenuItems(hcChannel)), strings.ReplaceAll(sched, "%s", "Start Huddle"); got != want {
		t.Errorf("idle: %q, want %q", got, want)
	}
	// a DM: the same menu (Schedule Huddle invites everyone in it)
	if got, want := labels(e.c.MenuItems(dmChannel)), strings.ReplaceAll(sched, "%s", "Start Huddle"); got != want {
		t.Errorf("DM: %q, want %q", got, want)
	}
	// live, you're not in it
	e.setLive(hcChannel, huddle.FakeRemoteUser)
	if got, want := labels(e.c.MenuItems(hcChannel)), strings.ReplaceAll(sched, "%s", "Join Huddle"); got != want {
		t.Errorf("live: %q, want %q", got, want)
	}
	// you're in it on another device: still Join Huddle
	e.setLive(hcChannel, huddle.FakeRemoteUser, huddle.FakeSelfUser)
	if got, want := labels(e.c.MenuItems(hcChannel)), strings.ReplaceAll(sched, "%s", "Join Huddle"); got != want {
		t.Errorf("other device: %q, want %q", got, want)
	}
	// shared with another organization: no Schedule for later
	e.h.app.S.PutConv(slack.Conversation{ID: "C0SHARED", Name: "shared", IsChannel: true, IsMember: true, IsExtShared: true})
	if got := labels(e.c.MenuItems("C0SHARED")); got != "# Huddle | Start Huddle | Copy huddle link" {
		t.Errorf("Slack Connect channel: %q", got)
	}

	// in it on this device: no huddle item (the button itself leaves)
	e.join(nil, nil)
	if got, want := labels(e.c.MenuItems(hcChannel)), "# Huddle | Copy huddle link | --- | # Schedule for later | Schedule Huddle"; got != want {
		t.Errorf("in it: %q, want %q", got, want)
	}

	// Schedule Huddle opens the schedule modal for the channel
	for _, it := range e.c.MenuItems(hcChannel) {
		if it.Label == "Schedule Huddle" {
			it.Do()
		}
	}
	if e.mh.schedule == nil || e.mh.schedule[0] != hcChannel {
		t.Errorf("Schedule Huddle opened %v", e.mh.schedule)
	}

	// the profile card's call menu
	if got := labels(e.c.MemberMenuItems(huddle.FakeRemoteUser)); got != "Start Huddle | Copy huddle link" {
		t.Errorf("card, DM exists: %q", got)
	}
	if got := labels(e.c.MemberMenuItems("U0NODM0001")); got != "Start Huddle" {
		t.Errorf("card, no DM: %q", got)
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

type failCaller struct{}

func (failCaller) Call(context.Context, string, url.Values, any) error {
	return &slack.Error{Code: "channel_not_found"}
}

// Copy huddle link: rooms.getLink's link on the clipboard and Slack's
// "Huddle link copied"; a failed fetch copies nothing and says so.
func TestCopyHuddleLink(t *testing.T) {
	e := newMenuEnv(t, false, true)
	fk := huddle.NewFake(testClip(t), t.Errorf)
	t.Cleanup(fk.Close)
	e.c.Client = fk.Caller()
	var copy popups.MenuItem
	for _, it := range e.c.MenuItems(hcChannel) {
		if it.Label == "Copy huddle link" {
			copy = it
		}
	}
	if copy.Do == nil || copy.Icon != "link" {
		t.Fatalf("no Copy huddle link item: %+v", copy)
	}
	copy.Do()
	waitFor(t, "the copy toast", func() bool { _, ts := e.mh.snapshot(); return len(ts) > 0 })
	copied, toasts := e.mh.snapshot()
	if want := huddle.FakeLinkBase + hcChannel; len(copied) != 1 || copied[0] != want {
		t.Errorf("copied %q, want %q", copied, want)
	}
	if toasts[0] != "Huddle link copied" {
		t.Errorf("toast %q", toasts[0])
	}

	e.c.Client = failCaller{}
	e.c.CopyLink(hcChannel)
	waitFor(t, "the failure toast", func() bool { _, ts := e.mh.snapshot(); return len(ts) > 1 })
	copied, toasts = e.mh.snapshot()
	if len(copied) != 1 || toasts[1] != "Couldn’t copy huddle link. Try again." {
		t.Errorf("after a failed fetch: copied %q, toasts %q", copied, toasts)
	}
}

// The profile card's Huddle → Start Huddle with someone you have no DM
// with: conversations.open on fakeslack, then rooms.join on the fake
// Chime in the new DM.
func TestStartHuddleFromCard(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	st := store.New("")
	st.PutUsers([]slack.User{
		{ID: huddle.FakeSelfUser, Name: "me", RealName: "Test Self"},
		{ID: huddle.FakeRemoteUser, Name: "remote", RealName: "Remote Example"},
	})
	app := core.New(slack.New(fs.Creds()), st)
	if u, ok := st.User(huddle.FakeSelfUser); ok {
		app.S.Self = *u
	}
	mh := &recHost{hcHost: hcHost{app: app, th: theme.New(false)}}
	c := New(mh)
	c.SelfID = huddle.FakeSelfUser
	c.NoWindow = true
	fk := huddle.NewFake(testClip(t), t.Errorf)
	t.Cleanup(fk.Close)
	c.Client = fk.Caller()
	c.Tweak = func(o *huddle.Options) { o.Device = huddle.NewToneDevice(440) }

	items := c.MemberMenuItems(huddle.FakeRemoteUser)
	if got := labels(items); got != "Start Huddle" {
		t.Fatalf("card menu %q", got)
	}
	items[0].Do()
	waitFor(t, "the huddle in the new DM", func() bool { return c.Session() != nil })
	s := c.Session()
	t.Cleanup(s.Leave)
	dm := c.dmWith(huddle.FakeRemoteUser)
	if dm == "" || s.Channel() != dm {
		t.Fatalf("huddle in %q, DM %q", s.Channel(), dm)
	}
	if got := fs.OpenedDMs(); len(got) != 1 || got[0] != huddle.FakeRemoteUser {
		t.Errorf("conversations.open users %q", got)
	}
	waitState(t, s, "connected", func(st huddle.State) bool { return st.Phase == huddle.Connected })
	// now the card's menu leaves, and the DM has a link to copy
	if got := labels(c.MemberMenuItems(huddle.FakeRemoteUser)); got != "Leave Huddle | Copy huddle link" {
		t.Errorf("card menu in the huddle: %q", got)
	}
	// a second start with the same person reuses the DM
	c.StartWith(huddle.FakeRemoteUser)
	time.Sleep(200 * time.Millisecond)
	if got := fs.OpenedDMs(); len(got) != 1 {
		t.Errorf("conversations.open again: %q", got)
	}
}

// Pictures of the menus (CLACK_SHOTS): the caret menu idle (grouped and
// flat), live and in the huddle, in light and dark, and the card's menu.
func TestHeaderMenuShots(t *testing.T) {
	type shot struct {
		name  string
		sched bool
		state string // idle | live | in | card
	}
	shots := []shot{{"idle", true, "idle"}, {"idle-flat", false, "idle"}, {"live", true, "live"}, {"in", true, "in"}, {"card", true, "card"}}
	for _, dark := range []bool{false, true} {
		suffix := "light"
		page := color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
		if dark {
			suffix, page = "dark", color.NRGBA{R: 0x1a, G: 0x1d, B: 0x21, A: 0xff}
		}
		for _, sh := range shots {
			e := newMenuEnv(t, dark, sh.sched)
			switch sh.state {
			case "live":
				e.setLive(hcChannel, huddle.FakeRemoteUser, "U0THIRD")
			case "in":
				e.join(nil, nil)
			}
			var items []popups.MenuItem
			if sh.state == "card" {
				items = e.c.MemberMenuItems(huddle.FakeRemoteUser)
			} else {
				items = e.c.MenuItems(hcChannel)
			}
			const W, H = 360, 300
			var m *popups.Menu
			img := render(t, "header-menu-"+sh.name+"-"+suffix, W, H, func(gtx C) {
				theme.Fill(gtx, image.Rectangle{Max: gtx.Constraints.Max}, page)
				bx, by := gtx.Dp(150), gtx.Dp(12)
				btn := gtx.Dp(28)
				var caret image.Rectangle
				if sh.state == "live" || sh.state == "in" {
					off := op.Offset(image.Pt(bx, by)).Push(gtx.Ops)
					e.c.HeaderPill(gtx, e.h.th, hcChannel, btn)
					off.Pop()
					cx := bx + e.c.HeaderCaretX(gtx, e.h.th, hcChannel)
					caret = image.Rect(cx, by, cx+gtx.Dp(hdrCaret), by+btn)
				} else {
					caret = image.Rect(bx+gtx.Dp(74), by, bx+gtx.Dp(94), by+btn)
				}
				if m == nil {
					w := MenuWidth(gtx, e.h.th, items)
					a := MenuAnchor(gtx, caret, w)
					if sh.state == "card" { // CallMenu: 305 wide, 10px under the button
						w = 305
						x := caret.Min.X + (caret.Dx()-gtx.Dp(305))/2
						a = image.Rect(x, caret.Min.Y, x+gtx.Dp(305), caret.Max.Y+gtx.Dp(10))
					}
					m = popups.NewMenuOpts(e.mh, items, popups.MenuOpts{Width: w, Anchor: a, Side: popups.Below})
				}
				m.Layout(gtx)
			})
			bg := popups.ColorsOf(e.h.th).Bg
			if n := pixeltest.Count(img, img.Bounds(), bg, 4); n < 150*28*shotScale*shotScale {
				t.Errorf("%s-%s: no menu card (%d px of %v)", sh.name, suffix, n, bg)
			}
		}
	}
}
