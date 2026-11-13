package ui

// Hover versus click on a message's mentions, as Slack splits them
// (Karim 2026-11-13: "clicking a mention brings up the hover popup
// thing"). Hover: a person or an app opens the hover card after 350ms
// (ProfileHoverTrigger), a #channel its channel card (BaseMrkdwnChannel's
// HoverCardTrigger), a user group nothing. Click: HoverCardTrigger's
// handleClick closes the card and the trigger stays quiet until the
// pointer leaves it; the click goes on to the mention's own handler:
// MemberSlug (boot-shared ld4x) opens a person's profile in the
// secondary pane and an app's App details (toAppDetails), a channel
// navigates to it, a user group opens the User Group pane. Against
// internal/fakeslack (synthetic; nothing talks to Slack).

import (
	"fmt"
	"image"
	"testing"
	"time"

	"gioui.org/f32"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/hovercard"
	"github.com/some-du6e/clack/internal/ui/messages"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/richview"
)

const mcPerson = "U0FAKE00001"

// mcLinks is the open conversation list's link boxes (channel or thread).
func (e *perfEnv) mcLinks(ch, ts string) []messages.LinkRect {
	c := e.a.convos[ch+"/"+ts]
	if c == nil {
		return nil
	}
	if l, ok := c.list.(interface{ LinkRects() []messages.LinkRect }); ok {
		return l.LinkRects()
	}
	return nil
}

// mcBox waits for the visible link of kind k to id in conversation ch/ts
// and returns the union of its line boxes.
func (e *perfEnv) mcBox(t *testing.T, ch, ts string, k richview.HitKind, id, msgTS string) image.Rectangle {
	t.Helper()
	var r image.Rectangle
	e.until(fmt.Sprintf("link %d %s in %s/%s", k, id, ch, ts), 10*time.Second, func() bool {
		r = image.Rectangle{}
		for _, lr := range e.mcLinks(ch, ts) {
			if lr.Kind == k && lr.ID == id && (msgTS == "" || lr.TS == msgTS) {
				r = r.Union(lr.R)
			}
		}
		return !r.Empty()
	})
	return r
}

// mcState describes what's open, for failures.
func (e *perfEnv) mcState() string {
	req, _, open := e.a.hover.ctl.Open()
	pend, hasPend := e.a.hover.ctl.Pending()
	s := e.a.secondary
	return fmt.Sprintf("hover open=%v %+v pending=%v %+v; overlays %d (profile cards %d); secondary kind %d user %q view %T; route %q",
		open, req, hasPend, pend, len(e.a.overlays), e.profileCards(), s.kind, s.user, s.view, e.a.router.Current())
}

func TestMentionHoverAndClick(t *testing.T) {
	sz := fakeslack.Small
	sz.ThreadHeader = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	conv := fakeslack.ThreadPublicID // one fixture message: the posted ones are newest and in view
	text := "Ask <@" + mcPerson + "> or <@" + fakeslack.AppUserID + ">, notes in <#" + fakeslack.ThreadSharedID +
		">, cc <!subteam^" + fakeslack.WorkspaceGroupID + "|@snackcrew> today"
	msgTS, _ := fs.PostQuiet(conv, fakeslack.ProfileID, text)
	e := startEnv(t, fs, t.TempDir())
	e.waitReady()
	e.openChannel(conv)
	e.settle(800 * time.Millisecond)
	ctl := &e.a.hover.ctl
	away := f32.Pt(float32(e.a.primaryOrigin.X)-60, float32(e.size.Y)/2) // the sidebar: no hover cards
	var clk time.Duration
	reset := func() {
		e.move(away)
		time.Sleep(hovercard.HideDelay + 50*time.Millisecond)
		e.frame()
		for e.a.dismissTop() {
		}
		e.a.CloseSecondary()
		if e.a.router.Current().Channel() != conv {
			e.openChannel(conv)
		}
		e.settle(400 * time.Millisecond)
	}

	type kase struct {
		name   string
		kind   richview.HitKind
		id     string
		card   func(hovercard.Request) bool // the hover card it opens (nil: none)
		opened func() bool                  // what the click opened
	}
	cases := []kase{
		{"person", richview.HitUser, mcPerson,
			func(r hovercard.Request) bool { return r.UserID == mcPerson && r.ChannelID == "" },
			func() bool {
				s := e.a.secondary
				return s.kind == secProfile && s.user == mcPerson && e.profileCards() == 0
			}},
		{"app", richview.HitUser, fakeslack.AppUserID,
			func(r hovercard.Request) bool { return r.UserID == fakeslack.AppUserID },
			func() bool { return e.appDetailsOpen(fakeslack.AppUserID) && e.profileCards() == 0 }},
		{"channel", richview.HitChannel, fakeslack.ThreadSharedID,
			func(r hovercard.Request) bool { return r.ChannelID == fakeslack.ThreadSharedID },
			func() bool {
				return e.a.router.Current().Channel() == fakeslack.ThreadSharedID && e.profileCards() == 0
			}},
		{"user group", richview.HitUsergroup, fakeslack.WorkspaceGroupID, nil,
			func() bool {
				_, ok := e.a.secondary.view.(*popups.UserGroupView)
				return ok && e.a.secondary.kind == secProfile && e.profileCards() == 0
			}},
	}
	for _, c := range cases {
		r := e.mcBox(t, conv, "", c.kind, c.id, msgTS)
		mid := f32.Pt(float32(r.Min.X+r.Dx()/2), float32(r.Min.Y+r.Dy()/2))

		// hover: the card after Slack's delay, never at once
		e.move(mid)
		if _, _, open := ctl.Open(); open {
			t.Errorf("%s: a card opened without the delay: %s", c.name, e.mcState())
		}
		time.Sleep(hovercard.ShowDelay + 50*time.Millisecond)
		e.frame()
		req, _, open := ctl.Open()
		switch {
		case c.card == nil && open:
			t.Errorf("%s: hovering opened a card: %s", c.name, e.mcState())
		case c.card != nil && (!open || !c.card(req)):
			t.Errorf("%s: hovering %v: %s", c.name, r, e.mcState())
		}
		if n := e.profileCards(); n != 0 {
			t.Errorf("%s: hovering opened %d click card(s)", c.name, n)
		}
		e.settle(300 * time.Millisecond) // the card's own loads (users.info, apps.profile.get)

		// click, the way a hand does: a pixel of jitter, then press and
		// release
		e.move(mid.Add(f32.Pt(1, 0)))
		clk += time.Second
		e.clickAt(image.Pt(int(mid.X), int(mid.Y)), clk)
		e.frame()
		e.frame()
		if _, _, open := ctl.Open(); open {
			t.Errorf("%s: the hover card stayed open after the click: %s", c.name, e.mcState())
		}
		e.until(c.name+" click target", 5*time.Second, c.opened)
		// resting on the mention after the click opens nothing again
		// (the trigger is spent until the pointer leaves it), even as
		// the pointer jitters and the pane's layout settles
		for i := 0; i < 4; i++ {
			if c.kind == richview.HitChannel {
				break // the click left the conversation
			}
			r2 := e.mcBox(t, conv, "", c.kind, c.id, msgTS)
			p := f32.Pt(float32(r2.Min.X+r2.Dx()/2), float32(r2.Min.Y+r2.Dy()/2))
			if !image.Pt(int(p.X), int(p.Y)).In(r) {
				break // the narrower list moved it: that's a leave
			}
			e.move(p.Add(f32.Pt(float32(i%2), 0)))
			time.Sleep(hovercard.ShowDelay / 2)
			e.frame()
		}
		time.Sleep(hovercard.ShowDelay + 50*time.Millisecond)
		e.frame()
		if req, _, open := ctl.Open(); open {
			t.Errorf("%s: a card opened after the click: %+v (%s)", c.name, req, e.mcState())
		}
		if !c.opened() {
			t.Errorf("%s: what the click opened went away: %s", c.name, e.mcState())
		}
		reset()
	}

	// the thread pane: the same split for a reply's mention
	replyTS := fs.PostReplyQuiet(conv, msgTS, fakeslack.ProfileID, "thanks <@"+mcPerson+">")
	e.a.OpenThread(conv, msgTS)
	e.settle(1500 * time.Millisecond)
	r := e.mcBox(t, conv, msgTS, richview.HitUser, mcPerson, replyTS)
	mid := f32.Pt(float32(r.Min.X+r.Dx()/2), float32(r.Min.Y+r.Dy()/2))
	e.move(mid)
	time.Sleep(hovercard.ShowDelay + 50*time.Millisecond)
	e.frame()
	if req, _, open := ctl.Open(); !open || req.UserID != mcPerson {
		t.Errorf("thread: hovering the mention: %s", e.mcState())
	}
	clk += time.Second
	e.clickAt(image.Pt(int(mid.X), int(mid.Y)), clk)
	e.frame()
	e.frame()
	e.until("the thread mention's profile", 5*time.Second, func() bool {
		s := e.a.secondary
		return s.kind == secProfile && s.user == mcPerson && e.profileCards() == 0
	})
	time.Sleep(hovercard.ShowDelay + 50*time.Millisecond)
	e.frame()
	if _, _, open := ctl.Open(); open {
		t.Errorf("thread: a card is open after the click: %s", e.mcState())
	}
}
