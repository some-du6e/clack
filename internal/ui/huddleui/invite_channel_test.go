package huddleui

import (
	"net/url"
	"strings"
	"sync"
	"testing"

	"gioui.org/op"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/huddle"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// A huddle invite to a private channel named it by its raw id and with a
// "#" ("Remote Example in #C0THPRIVAT1"), as Karim saw in notifications.
// Slack's invite window (HuddleInviteWindowText), sidebar card
// (useHuddleInviteProps) and preview title (useGetTitlebarText) draw the
// channel as InlineChannelEntity: lock-filled, then the bare name; the
// notification (sharedHuddleNotificationOptions) names a channel with
// getDisplayNameForChannel, only one that exists. A channel core can't
// name yet is looked up, and the surfaces name it once it's in
// (channel_entity.go). Synthetic channels only: fakeslack's
// conversations.info serves the private th-secret-plans.

// privInviteEnv is an invite env over fakeslack's thread-header
// conversations; conversations.info for the private channel waits until
// release is called (a lookup still running).
func privInviteEnv(t *testing.T) (e *inviteEnv, release func()) {
	t.Helper()
	sz := fakeslack.Small
	sz.ThreadHeader = true
	e = newInviteEnvSize(t, sz, nil)
	hold := make(chan struct{})
	var once sync.Once
	release = func() { once.Do(func() { close(hold) }) }
	t.Cleanup(release)
	e.fs.SetHook(func(method string, form url.Values) (any, bool) {
		if method == "conversations.info" && form.Get("channel") == fakeslack.ThreadPrivateID {
			<-hold
		}
		return nil, false
	})
	return e, release
}

// inviteView is the active invite as the window and the card draw it.
func (e *inviteEnv) inviteView() inviteView {
	e.t.Helper()
	e.c.mu.Lock()
	inv := e.c.inv
	e.c.mu.Unlock()
	if inv == nil {
		e.t.Fatal("no invite")
	}
	return e.c.view(inv)
}

// lineText is spans as text, and the channel's entity among them.
func lineText(ss []span) (string, *span) {
	var b strings.Builder
	var ent *span
	for i := range ss {
		b.WriteString(ss[i].Text)
		if ss[i].Ent != nil {
			ent = &ss[i]
		}
	}
	return b.String(), ent
}

// checkPrivate fails unless v names the private channel th-secret-plans
// the way Slack's InlineChannelEntity does: the lock, the bare name.
func checkPrivate(t *testing.T, v inviteView, who string) {
	t.Helper()
	const id, name = fakeslack.ThreadPrivateID, "th-secret-plans"
	if want := who + " in " + name; v.Text != want {
		t.Errorf("window text %q, Slack %q (lock-filled before the name)", v.Text, want)
	}
	if txt, ent := lineText(v.Line); txt != v.Text || ent == nil || ent.Ent.Icon != "lock-filled" || ent.Ent.Text != name || !ent.Bold {
		t.Errorf("window line %q (entity %+v), want the bold lock-filled %s", txt, ent, name)
	}
	txt, ent := lineText(v.CardText)
	if want := who + " is inviting you to a huddle in " + name; txt != want {
		t.Errorf("card %q, Slack %q", txt, want)
	}
	if ent == nil || ent.Ent.Icon != "lock-filled" || !ent.Bold {
		t.Errorf("card's channel %+v, want the bold lock-filled entity", ent)
	}
	for _, s := range []string{v.Text, txt} {
		if strings.Contains(s, id) || strings.Contains(s, "#"+name) {
			t.Errorf("%q shows the id or a # before a private channel", s)
		}
	}
}

// drawInvite lays out the invite window and the card with v (the entity's
// states draw).
func drawInvite(t *testing.T, c *Controller, v inviteView) {
	t.Helper()
	th := theme.New(false)
	var dec, soon, join widget.Clickable
	var card cardState
	var ops op.Ops
	for range 2 {
		ops.Reset()
		layoutInviteWindow(shotCtx(&ops, 380, 280), th, c, v, &dec, &soon, &join)
		ops.Reset()
		gtx := shotCtx(&ops, 260, 240)
		layoutCard(gtx, th, c, v, &card, gtx.Constraints.Max.X, false)
	}
}

func TestInvitePrivateChannelTitle(t *testing.T) {
	const priv = fakeslack.ThreadPrivateID
	t.Run("known to core", func(t *testing.T) {
		e, release := privInviteEnv(t)
		release()
		e.app.S.PutConv(slack.Conversation{ID: priv, Name: "th-secret-plans", IsGroup: true, IsPrivate: true, IsMember: true})
		e.invite(priv)
		e.shown("the invite")
		who := e.c.displayName(huddle.FakeRemoteUser)
		checkPrivate(t, e.inviteView(), who)
		drawInvite(t, e.c, e.inviteView())
		waitFor(t, "the notification", func() bool { return len(e.h.notifications()) == 1 })
		if n := e.h.notifications()[0]; n.Body != who+" is inviting you to a huddle in th-secret-plans" {
			t.Errorf("notification %q, Slack's getDisplayNameForChannel: the bare name", n.Body)
		}
		// the preview's title and the huddle button's tip
		title, _, _ := e.c.previewTitle(priv)
		if txt, ent := lineText(title); txt != "Start huddle in th-secret-plans" || ent == nil || ent.Ent.Icon != "lock-filled" {
			t.Errorf("preview %q (%+v), Slack: Start huddle in [lock] th-secret-plans", txt, ent)
		}
		if tip := e.c.headerTip(priv, false, []string{"U1", "U2"}); tip != "Join huddle in th-secret-plans\n2 in the huddle" {
			t.Errorf("tip %q", tip)
		}
	})
	// a stub named by its id (a payload without the name): the
	// notification leaves the place out (Slack's stub doesn't exist yet),
	// the window and the card show the loading bar, then the name once
	// conversations.info answers
	t.Run("a stub named by its id", func(t *testing.T) {
		e, release := privInviteEnv(t)
		e.app.S.PutConv(slack.Conversation{ID: priv, Name: priv, IsGroup: true, IsPrivate: true, IsMember: true})
		e.invite(priv)
		e.shown("the invite")
		who := e.c.displayName(huddle.FakeRemoteUser)
		waitFor(t, "the notification", func() bool { return len(e.h.notifications()) == 1 })
		if n := e.h.notifications()[0]; n.Body != who+" is inviting you to a huddle" || strings.Contains(n.Body, priv) {
			t.Errorf("notification %q, Slack %q (no channel until it exists)", n.Body, who+" is inviting you to a huddle")
		}
		v := e.inviteView()
		if _, ent := lineText(v.Line); ent == nil || !ent.Ent.Loading() || strings.Contains(v.Text, priv) {
			t.Fatalf("while the lookup runs: %q %+v, want the loading bar, never the id", v.Text, ent)
		}
		drawInvite(t, e.c, v)
		release()
		waitFor(t, "the channel's name", func() bool { return e.inviteView().Text == who+" in th-secret-plans" })
		checkPrivate(t, e.inviteView(), who)
	})
	t.Run("not in the store", func(t *testing.T) {
		e, release := privInviteEnv(t)
		e.invite(priv)
		e.shown("the invite")
		who := e.c.displayName(huddle.FakeRemoteUser)
		if v := e.inviteView(); strings.Contains(v.Text, priv) || strings.Contains(v.Text, "#") {
			t.Errorf("before the lookup: %q", v.Text)
		}
		release()
		waitFor(t, "the channel's name", func() bool { return e.inviteView().Text == who+" in th-secret-plans" })
		checkPrivate(t, e.inviteView(), who)
	})
	// a channel conversations.info doesn't know: BaseMissingChannel's
	// Private channel chip
	t.Run("a channel the viewer can't see", func(t *testing.T) {
		e, _ := privInviteEnv(t)
		const gone = "C0NOPE00001"
		e.invite(gone)
		e.shown("the invite")
		who := e.c.displayName(huddle.FakeRemoteUser)
		waitFor(t, "the lookup", func() bool { return e.inviteView().Text == who+" in Private channel" })
		v := e.inviteView()
		if _, ent := lineText(v.Line); ent == nil || !ent.Ent.Chip || ent.Ent.Icon != "lock" {
			t.Errorf("window %+v, want the Private channel chip", ent)
		}
		if txt, _ := lineText(v.CardText); txt != who+" is inviting you to a huddle in Private channel" {
			t.Errorf("card %q", txt)
		}
		drawInvite(t, e.c, v)
	})
}
