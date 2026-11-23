package messages

// The DM foreword against Slack's BaseImForeword and IMForewordMemberEntity
// (boot-shared VHud, build 9f450b8ad3db; foreword.go) on synthetic members.

import (
	"image"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
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
