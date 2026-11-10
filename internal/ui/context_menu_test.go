package ui

import (
	"context"
	"image"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/op"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/ctxclick"
	"github.com/some-du6e/clack/internal/ui/popups"
)

// ctxShell is the whole window over an offline core (no plugins), driven
// through a real input.Router: svEnv's helpers without Slick.
func ctxShell(t *testing.T) *svEnv {
	t.Helper()
	a, ca := ttApp(t, t.TempDir())
	ca.S.PutUsers([]slack.User{{ID: svAlice, Name: "alice", RealName: "Alice Example", Profile: slack.Profile{DisplayName: "alice", RealName: "Alice Example"}}})
	ca.S.PutConv(slack.Conversation{ID: svRandom, Name: "random", IsChannel: true, IsMember: true, NumMembers: 3})
	ca.S.PutConv(slack.Conversation{ID: svDM, IsIM: true, User: svAlice, IsMember: true})
	b := newBackend(ca, "")
	b.app = a
	b.ctx, b.cancel = context.WithCancel(context.Background())
	t.Cleanup(b.cancel)
	for _, ch := range []string{ttGen, svRandom, svDM} {
		b.fetched[ch] = time.Now() // offline: no tabs/facepile fetches
	}
	a.data = b
	b.apply(a, ca.Snapshot())
	m := &b.m
	m.Sections = []sbSection{
		{ID: "Lchannels", Name: "Channels", Type: "channels", Rows: []string{ttGen, svRandom}},
		{ID: "Ldms", Name: "Direct messages", Type: "direct_messages", Rows: []string{svDM}},
	}
	m.version++
	e := &svEnv{t: t, a: a, ca: ca, ops: new(op.Ops), rt: new(input.Router), sz: image.Pt(1280, 860), trees: map[uint64][]byte{}}
	sbMenuTap = e.menu
	t.Cleanup(func() { sbMenuTap = nil })
	a.Navigate("c/" + ttGen)
	for i := 0; i < 5; i++ {
		e.frame()
	}
	return e
}

// Sidebar rows open their menu on a right-click, and on macOS on a
// Ctrl+click, which doesn't also open the conversation.
func TestSidebarRowContextMenu(t *testing.T) {
	e := ctxShell(t)
	e.rightClick(svRandom)
	if e.lastMenu(svRandom) == nil {
		t.Fatal("right-clicking #random's row opened no menu")
	}
	if got := e.a.router.Current(); got != Route("c/"+ttGen) {
		t.Errorf("the right-click navigated to %q", got)
	}

	e.a.overlays = nil // the open menu's scrim would take the next press
	e.frame()
	prev := ctxclick.Mac
	ctxclick.Mac = true
	t.Cleanup(func() { ctxclick.Mac = prev })
	p, ok := e.rowPos(svDM)
	if !ok {
		t.Fatal("no row for the DM")
	}
	e.clock += time.Second
	e.rt.Queue(
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: p, Time: e.clock},
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Modifiers: key.ModCtrl, Position: p, Time: e.clock + 10*time.Millisecond},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Modifiers: key.ModCtrl, Position: p, Time: e.clock + 60*time.Millisecond},
	)
	e.frame()
	e.frame()
	if e.lastMenu(svDM) == nil {
		t.Error("Ctrl+click on the DM's row (macOS) opened no menu")
	}
	if got := e.a.router.Current(); got != Route("c/"+ttGen) {
		t.Errorf("Ctrl+click (macOS) navigated to %q", got)
	}
}

// The channel header's title opens the channel menu at the pointer on a
// right-click (Slack's ChannelContextMenu around the title).
func TestHeaderTitleContextMenu(t *testing.T) {
	e := ctxShell(t)
	a := e.a
	conv := a.model().conv(ttGen)
	if conv == nil {
		t.Fatal("no #general")
	}
	var rt input.Router
	ops := new(op.Ops)
	frame := func() {
		ops.Reset()
		gtx := ttContext(ops, image.Pt(900, 49))
		gtx.Source = rt.Source()
		a.layoutChannelHeader(gtx, ttGen, conv)
		rt.Frame(ops)
	}
	frame()
	menus := func() int {
		n := 0
		for _, o := range a.overlays {
			if m, ok := o.(*popups.Menu); ok {
				for _, it := range m.Items() {
					if it.Label == "Copy" && len(it.Sub) > 0 {
						n++
						break
					}
				}
			}
		}
		return n
	}
	press := func(p f32.Point, b pointer.Buttons, mods key.Modifiers, at time.Duration) {
		rt.Queue(
			pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: p, Time: at},
			pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: b, Modifiers: mods, Position: p, Time: at + 10*time.Millisecond},
			pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Modifiers: mods, Position: p, Time: at + 60*time.Millisecond},
		)
		frame()
		frame()
	}
	title := f32.Pt(70, 24) // the title button starts at x 52
	press(title, pointer.ButtonSecondary, 0, time.Second)
	if menus() != 1 {
		t.Fatalf("right-clicking the header title opened %d channel menus", menus())
	}
	press(f32.Pt(25, 24), pointer.ButtonSecondary, 0, 2*time.Second) // the star button
	if menus() != 1 {
		t.Errorf("right-clicking the star opened a channel menu")
	}

	prev := ctxclick.Mac
	ctxclick.Mac = true
	t.Cleanup(func() { ctxclick.Mac = prev })
	press(title, pointer.ButtonPrimary, key.ModCtrl, 3*time.Second)
	if menus() != 2 {
		t.Errorf("Ctrl+click (macOS) on the header title opened %d more channel menus", menus()-1)
	}
}
