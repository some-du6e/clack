package ui

// A click on a person opens their profile in the secondary pane, as in
// Slack (Karim: "clicking on a mention shows up the popup instead of the
// side bar thingy", "also applies for profiles"): the sender name and
// avatar call openProfile (client-boot rLTI), mentions navigate to
// toProfile (boot-shared ld4x), and the Profile view goes in the
// secondary container (client-boot +zKh aK, L1Ms). The card is the hover
// card only (HoverCardTrigger 0oEe: openOnClick off). From inside the
// thread pane the profile keeps the thread as its back stack (view.prev,
// client-boot IoL+) and FlexpaneBackButton returns to it. A bot user
// keeps Clack's app card (Slack's App details view isn't in Clack).
// Against internal/fakeslack (synthetic; nothing talks to Slack). With
// HOVER_SHOTS_DIR set (the hover-card workflow) each state is saved.

import (
	"context"
	"image"
	"os"
	"testing"
	"time"

	"gioui.org/f32"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/hovercard"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// profileCards counts the profile cards open as overlays.
func (e *perfEnv) profileCards() int {
	n := 0
	for _, o := range e.a.overlays {
		if _, ok := o.(*popups.ProfileCard); ok && !o.Dismissed() {
			n++
		}
	}
	return n
}

// personAt sweeps x0..x1 (window px, from the bottom up) until a profile
// trigger (a sender's name or avatar, a mention) reports under the
// pointer, and returns that point.
func (e *perfEnv) personAt(x0, x1 float32, top int) (f32.Point, hovercard.Request, bool) {
	ctl := &e.a.hover.ctl
	for y := float32(e.size.Y - 120); y > float32(top); y -= 4 {
		for x := x0; x < x1; x += 8 {
			e.move(f32.Pt(x, y))
			if r, ok := ctl.Pending(); ok && r.UserID != "" {
				return f32.Pt(x, y), r, true
			}
		}
	}
	return f32.Point{}, hovercard.Request{}, false
}

func TestPersonClickOpensProfilePane(t *testing.T) {
	sz := fakeslack.Small
	sz.ThreadHeader = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	e := startEnv(t, fs, t.TempDir())
	if os.Getenv("HOVER_SHOTS_DIR") != "" {
		w, err := pixeltest.NewWindow(e.size.X, e.size.Y)
		if err != nil {
			t.Fatalf("headless window: %v", err)
		}
		e.gpu = w
	}
	e.waitReady()
	ch := fakeslack.ThreadPublicID
	e.openChannel(ch)
	e.settle(800 * time.Millisecond)
	var clk time.Duration
	click := func(p f32.Point) {
		clk += time.Second // apart: never a double click
		e.clickAt(image.Pt(int(p.X), int(p.Y)), clk)
		e.frame()
		e.frame()
	}

	// the channel: a sender's name or avatar opens the profile pane
	x0 := float32(e.a.primaryOrigin.X)
	at, req, ok := e.personAt(x0+20, x0+100, 150)
	if !ok {
		t.Fatal("no message author reported to the hover card in the channel")
	}
	click(at)
	if s := e.a.secondary; s.kind != secProfile || s.user != req.UserID {
		t.Fatalf("clicking %s in the channel: secondary pane %d %q, want their profile", req.UserID, s.kind, s.user)
	}
	if n := e.profileCards(); n != 0 {
		t.Errorf("the click opened %d profile card(s)", n)
	}
	if e.a.SecondaryBack() {
		t.Error("a profile opened from the channel has a back stack")
	}
	if _, _, open := e.a.hover.ctl.Open(); open {
		t.Error("the hover card stayed open after the click")
	}
	e.settle(600 * time.Millisecond)
	hcShot(t, e, "profile-click-channel")

	// the thread pane: a name there opens the profile over the thread,
	// which stays as its back stack
	e.a.OpenThread(ch, fakeslack.ThreadRootTS)
	e.settle(1500 * time.Millisecond)
	if e.a.secondary.kind != secThread {
		t.Fatalf("the thread didn't open (%d)", e.a.secondary.kind)
	}
	po := e.a.secondaryOrigin
	px := float32(po.X)
	at, req, ok = e.personAt(px+20, px+100, po.Y+60)
	if !ok {
		t.Fatal("no author reported to the hover card in the thread pane")
	}
	click(at)
	s := e.a.secondary
	if s.kind != secProfile || s.user != req.UserID {
		t.Fatalf("clicking %s in the thread: secondary pane %d %q, want their profile", req.UserID, s.kind, s.user)
	}
	if !e.a.SecondaryBack() || s.prev == nil || s.prev.kind != secThread || s.prev.ts != fakeslack.ThreadRootTS {
		t.Fatalf("the profile opened from the thread has no thread to go back to (%+v)", s.prev)
	}
	if n := e.profileCards(); n != 0 {
		t.Errorf("the click opened %d profile card(s)", n)
	}
	e.settle(600 * time.Millisecond)
	hcShot(t, e, "profile-click-thread")
	// FlexpaneBackButton, 16px into the pane's header (49 tall)
	po = e.a.secondaryOrigin
	click(f32.Pt(float32(po.X+16+14), float32(po.Y+49/2)))
	if s := e.a.secondary; s.kind != secThread || s.channel != ch || s.ts != fakeslack.ThreadRootTS || e.a.SecondaryBack() {
		t.Fatalf("the back button: secondary pane %d %q/%q (back %v), want the thread again", s.kind, s.channel, s.ts, e.a.SecondaryBack())
	}
	if r := e.a.router.Current(); r.Thread() != fakeslack.ThreadRootTS {
		t.Errorf("back in the thread, the route is %q", r)
	}
	e.settle(600 * time.Millisecond)
	hcShot(t, e, "profile-back-to-thread")

	// a bot user keeps the app card
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	e.a.data.core.EnsureUsers(ctx, []string{fakeslack.AppUserID})
	e.a.OpenProfile(image.Pt(int(x0)+200, 300), fakeslack.AppUserID)
	e.frame()
	if n := e.profileCards(); n != 1 {
		t.Errorf("a bot user: %d profile cards, want its app card", n)
	}
	if e.a.secondary.kind == secProfile && e.a.secondary.user == fakeslack.AppUserID {
		t.Error("a bot user's profile opened in the pane")
	}
}
