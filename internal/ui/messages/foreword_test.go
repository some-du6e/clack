package messages

// The DM foreword against Slack's BaseImForeword and IMForewordMemberEntity
// (boot-shared VHud, build 9f450b8ad3db; foreword.go) on synthetic members.

import (
	"context"
	"errors"
	"image"
	"image/png"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/media"
	"github.com/some-du6e/clack/internal/media/goaudio"
	"github.com/some-du6e/clack/internal/media/mediatest"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
	"github.com/some-du6e/clack/internal/ui/videoplayer"
)

const fwDM = "D0FWTEST01"

// fwHost records the profiles opened.
type fwHost struct {
	coreHost
	mu     sync.Mutex
	opened []string
	vids   *videoplayer.Controller
}

func (h *fwHost) OpenProfile(_ image.Point, id string) {
	h.mu.Lock()
	h.opened = append(h.opened, id)
	h.mu.Unlock()
}

func (h *fwHost) profiles() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.opened...)
}

// fwVideosHost is fwHost with the window's players.
type fwVideosHost struct{ *fwHost }

func (h fwVideosHost) Videos() *videoplayer.Controller { return h.vids }

// fwEnv is the DM fwDM with one member over a real input.Router, every
// string drawn collected.
type fwEnv struct {
	t     *testing.T
	h     *fwHost
	v     *ChannelView
	r     input.Router
	ops   op.Ops
	now   time.Time
	clock time.Duration
	mu    sync.Mutex
	drawn map[string]bool
}

// newFWEnv is the DM with partner in app (newTestApp's when nil).
func newFWEnv(t *testing.T, app *core.App, partner slack.User, host func(*fwHost) Host) *fwEnv {
	t.Helper()
	if app == nil {
		app, _ = newTestApp(t)
	}
	if partner.ID != app.Self().ID {
		app.S.PutUsers([]slack.User{partner})
	}
	app.S.PutConv(slack.Conversation{ID: fwDM, IsIM: true, User: partner.ID, IsMember: true})
	app.S.SetLatest(fwDM, []slack.Message{{Type: "message", TS: "1762500000.000100", User: partner.ID, Text: "a synthetic hello"}}, false)
	e := &fwEnv{t: t, h: &fwHost{coreHost: coreHost{app: app, th: theme.New(false)}}, now: time.Now(), drawn: map[string]bool{}}
	var hh Host = e.h
	if host != nil {
		hh = host(e.h)
	}
	theme.DrawHook = func(s string) {
		e.mu.Lock()
		e.drawn[s] = true
		e.mu.Unlock()
	}
	t.Cleanup(func() { theme.DrawHook = nil })
	e.v = NewChannelView(hh, fwDM)
	e.v.Preview = true
	until(t, 5*time.Second, "the foreword laid out", e.frame, func() bool { return e.top() >= 0 })
	for k := 0; k < 3; k++ {
		e.frame()
	}
	return e
}

func (e *fwEnv) frame() {
	e.ops.Reset()
	e.now = e.now.Add(20 * time.Millisecond)
	gtx := layout.Context{Ops: &e.ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(900, 700)), Now: e.now, Source: e.r.Source()}
	e.v.Layout(gtx)
	e.r.Frame(&e.ops)
}

// top is the intro's top in the view, -1 while it isn't laid out.
func (e *fwEnv) top() int {
	s := e.v.s
	if s == nil {
		return -1
	}
	for i := s.visible[0]; i < s.visible[1] && i < len(s.items); i++ {
		if s.items[i].kind == itIntro && s.heights[i] > 0 {
			return s.pos[i]
		}
	}
	return -1
}

func (e *fwEnv) has(s string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.drawn[s]
}

// move puts the pointer at p (view coordinates).
func (e *fwEnv) move(p f32.Point) {
	e.clock += time.Second
	e.r.Queue(pointer.Event{Kind: pointer.Move, Position: p, Source: pointer.Mouse, Time: e.clock})
	for k := 0; k < 3; k++ {
		e.frame()
	}
}

// click presses and releases at p.
func (e *fwEnv) click(p f32.Point) {
	e.clock += time.Second
	e.r.Queue(
		pointer.Event{Kind: pointer.Move, Position: p, Source: pointer.Mouse, Time: e.clock},
		pointer.Event{Kind: pointer.Press, Position: p, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Time: e.clock + 10*time.Millisecond},
		pointer.Event{Kind: pointer.Release, Position: p, Source: pointer.Mouse, Time: e.clock + 60*time.Millisecond},
	)
	for k := 0; k < 3; k++ {
		e.frame()
	}
}

func fwRobin() slack.User {
	return slack.User{ID: "UFWROBIN1", Name: "robin", RealName: "Robin Example", Presence: "active",
		Profile: slack.Profile{RealName: "Robin Example", Title: "Synthetic Fixture Wrangler", Pronouns: "they/them"}}
}

// A member's DM opens on IMForewordMemberEntity: the member's name
// (18px Black), their presence and title and pronouns beside the 102px
// avatar, then "This conversation is just between @Robin Example and
// you. Check out their profile to learn more about them." with the
// MemberSlug and the View Profile button. The avatar and the name are
// buttons, with the hand cursor over them, that open the member's
// profile (showMemberProfile). Before, the DM opened on the channel
// intro's big title and "This conversation is just between you and
// Robin Example."
func TestIMForewordMemberEntity(t *testing.T) {
	e := newFWEnv(t, nil, fwRobin(), nil)
	for _, s := range []string{"Robin Example", "Synthetic Fixture Wrangler", "they/them", "@Robin Example", "View Profile"} {
		if !e.has(s) {
			t.Errorf("the foreword drew no %q", s)
		}
	}
	top := float32(e.top())
	avatar := f32.Pt(20+51, top+48+51)   // 102px at (20, 48): margin 48px 20px
	name := f32.Pt(20+102+12+10, top+76) // the text column, 12px on: the name row (27px) over two 22px lines, centred
	e.move(avatar)
	if c := e.r.Cursor(); c != pointer.CursorPointer {
		t.Errorf("cursor over the avatar %v, want the hand", c)
	}
	e.move(name)
	if c := e.r.Cursor(); c != pointer.CursorPointer {
		t.Errorf("cursor over the name %v, want the hand", c)
	}
	e.click(avatar)
	e.click(name)
	if got := strings.Join(e.h.profiles(), ","); got != "UFWROBIN1,UFWROBIN1" {
		t.Errorf("profiles opened %q, want the member's from the avatar and the name", got)
	}
}

// The description's MemberSlug and View Profile open the profile too;
// the slug's hover is c-member_slug--link:hover.
func TestIMForewordSlugAndViewProfile(t *testing.T) {
	e := newFWEnv(t, nil, fwRobin(), nil)
	top := float32(e.top())
	r := e.v.s.fw.rects
	for _, k := range []string{"slug", "view"} {
		b, ok := r[k]
		if !ok || b.Empty() {
			t.Fatalf("no %s laid out: %v", k, r)
		}
		e.click(f32.Pt(float32(b.Min.X+b.Dx()/2), top+float32(b.Min.Y+b.Dy()/2)))
	}
	if got := strings.Join(e.h.profiles(), ","); got != "UFWROBIN1,UFWROBIN1" {
		t.Errorf("profiles opened %q, want two", got)
	}
	b := r["slug"]
	e.move(f32.Pt(float32(b.Min.X+2), top+float32(b.Min.Y+b.Dy()/2)))
	if !e.v.s.fw.slug.Hovered() {
		t.Error("the slug isn't hovered")
	}
}

// Your own DM: "(you)" after your name and "This is your space." (bold)
// before the rest; without EDIT_PROFILE_PHOTO no buttons (Slack's l is
// false, and renderViewProfileButton draws nothing for yourself). A bot's:
// the APP badge, "This is the very beginning of your direct message
// history with @…" and "How does … work?", which opens its About tab
// (openAboutAppInIm). The Slack system user's: its own description, no
// View Profile.
func TestIMForewordSelfBotAndSystem(t *testing.T) {
	app, _ := newTestApp(t)
	self := app.Self()
	e := newFWEnv(t, app, slack.User{ID: self.ID}, nil)
	for _, s := range []string{"(you)", "This", "space.", "Draft"} {
		if !e.has(s) {
			t.Errorf("self DM: no %q drawn", s)
		}
	}
	if e.has("View Profile") || e.has("Edit Profile") {
		t.Error("self DM without EDIT_PROFILE_PHOTO drew a button")
	}

	var about []string
	prev := popups.OpenAppAbout
	popups.OpenAppAbout = func(uid string) { about = append(about, uid) }
	t.Cleanup(func() { popups.OpenAppAbout = prev })
	bot := slack.User{ID: "UFWBOT001", Name: "fixturebot", RealName: "Fixture Bot", IsBot: true, Profile: slack.Profile{RealName: "Fixture Bot"}}
	e = newFWEnv(t, nil, bot, nil)
	for _, s := range []string{"APP", "@Fixture Bot", " How does Fixture Bot work?"} {
		if !e.has(s) {
			t.Errorf("bot DM: no %q drawn", s)
		}
	}
	if e.has("View Profile") {
		t.Error("bot DM drew View Profile")
	}
	top := float32(e.top())
	b := e.v.s.fw.rects["help"]
	e.click(f32.Pt(float32(b.Min.X+b.Dx()/2), top+float32(b.Min.Y+b.Dy()/2)))
	if len(about) != 1 || about[0] != "UFWBOT001" {
		t.Errorf("How does … work? opened %v, want the bot's About", about)
	}

	sys := slack.User{ID: "USLACK", Name: "Slack", RealName: "Slack", Profile: slack.Profile{RealName: "Slack"}}
	e = newFWEnv(t, nil, sys, nil)
	if !e.has("notifications") || e.has("View Profile") || e.has("APP") {
		t.Errorf("system DM: description %v, View Profile %v, APP %v", e.has("notifications"), e.has("View Profile"), e.has("APP"))
	}
}

// A member with a name recording (here your own DM, whose header section
// fakeslack lets the test write): the header's Name Recording plays from
// the 30px button after the name and the presence
// (NamePronunciationAudioPlayer), and its pronunciation text follows the
// pronouns.
func TestIMForewordNameRecording(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	c := slack.New(fs.Creds())
	c.EdgeURL = fs.URL + "edge/"
	s := store.New("")
	self := slack.User{ID: fakeslack.SelfID, TeamID: fakeslack.TeamID, Name: "me", Presence: "active", Profile: slack.Profile{DisplayName: "me", Pronouns: "they/them"}}
	s.PutUsers([]slack.User{self})
	app := core.New(c, s)
	app.S.Self = self
	app.ServeOffline()
	ctx := context.Background()
	if err := app.C.Call(ctx, "users.profile.setSections", map[string][]string{"user": {fakeslack.SelfID}, "section": {"PsHEADER"},
		"elements": {`[{"element_id":"PePRONOUNCE","text":{"text":"ME-me"}},{"element_id":"PeRECORD","file":{"fileId":"` + fakeslack.NameRecordingID + `"}}]`}}, nil); err != nil {
		t.Fatal(err)
	}
	_, opener := mediatest.Source(fakeslack.NameRecordingID, mediatest.TonesOpusFrag, false)
	noCard := func(func([]int16)) (io.Closer, error) { return nil, errors.New("no sound card (the test's)") }
	th := theme.New(false)
	ctl := videoplayer.New(videoplayer.Env{
		Manager: media.NewManager(media.NewChain(nil, &goaudio.Backend{Output: noCard, Logf: t.Logf})),
		Open: func(ctx context.Context, u string, off int64) (io.ReadCloser, media.Info, error) {
			return opener.Open(ctx, off)
		},
		Repaint: func() {},
		Theme:   func() *theme.Theme { return th },
	})
	t.Cleanup(ctl.CloseAll)
	e := newFWEnv(t, app, self, func(h *fwHost) Host { h.vids = ctl; return fwVideosHost{h} })
	until(t, 10*time.Second, "the name recording's button", e.frame, func() bool {
		_, ok := ctl.TileState("dm-foreword", "name", fakeslack.SelfID, fakeslack.NameRecordingID)
		return ok
	})
	until(t, 5*time.Second, "the pronunciation after the pronouns", e.frame, func() bool { return e.has("they/them · ME-me") })
}

// Screenshots of the member DM's foreword, light and dark (PROFILE_SHOTS_DIR,
// own-profile.yml), no tofu.
func TestIMForewordShots(t *testing.T) {
	dir := os.Getenv("PROFILE_SHOTS_DIR")
	if dir == "" {
		t.Skip("PROFILE_SHOTS_DIR unset (own-profile.yml sets it)")
	}
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		e := newFWEnv(t, nil, fwRobin(), nil)
		e.h.th.SetDark(dark)
		const sc = 2
		win := image.Pt(900*sc, 700*sc)
		var ops op.Ops
		frame := func() {
			ops.Reset()
			paint.Fill(&ops, colorsFor(e.h.th).Bg)
			gtx := layout.Context{Ops: &ops, Now: time.Now(), Metric: unit.Metric{PxPerDp: sc, PxPerSp: sc}, Constraints: layout.Exact(win)}
			e.v.Layout(gtx)
		}
		for i := 0; i < 6; i++ {
			frame()
			time.Sleep(20 * time.Millisecond)
		}
		stop := theme.StartTofuScan()
		frame()
		if st := stop(); st.Notdef > 0 {
			t.Errorf("%s: %d .notdef glyphs", mode, st.Notdef)
		}
		img := pixeltest.Render(t, win.X, win.Y, &ops)
		top := e.top() // the 2x frames' pixels already
		r := image.Rect(0, max(0, top), win.X, min(win.Y, top+360*sc))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		f, err := os.Create(dir + "/dm-foreword-" + mode + ".png")
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, img.SubImage(r)); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
}
