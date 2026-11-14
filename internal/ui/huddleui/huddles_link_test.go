package huddleui

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/huddle"
)

// The Huddles link's unread invites (Slack's clientHuddles.unreadInvites,
// YCI0) against fakeslack and the fake Chime: you're in a DM huddle (a
// real session on the fake Chime) when someone rings you in #huddle-test.
// The link names the inviting channel and, under huddles_link_v2, counts
// the invite; the Huddles page marks what it sees 600 ms on, once per
// call while it stays up; Decline, huddle_invite_cancel and starting the
// huddle drop the channel's invites.
func TestHuddleLinkUnreadInvites(t *testing.T) {
	e := newInviteEnv(t, nil)
	c := e.c
	e.app.S.Lock()
	e.app.S.TeamID = fakeslack.TeamID
	e.app.S.Unlock()
	e.app.SetExperimentGroup(ExpHuddlesLinkV2, "on")
	invite := func(ch, call string) {
		raw, _ := json.Marshal(map[string]any{"type": "huddle_invite", "channel_id": ch, "call_id": call,
			"sender_user_id": huddle.FakeRemoteUser, "team_id": fakeslack.TeamID})
		c.Handle(core.Event{Type: "huddle", Name: "huddle_invite", Raw: raw})
	}
	link := func(want HuddleLink, what string) {
		t.Helper()
		if got := c.Link(); got != want {
			t.Fatalf("%s: link %+v, want %+v", what, got, want)
		}
	}
	link(HuddleLink{}, "no huddle, no invite")

	// your huddle in the DM: the link's facepile is yours (eS has it, and
	// getChannelIdWithActiveHuddle picks your own)
	c.Start(dmChannel)
	waitFor(t, "the DM huddle", func() bool { s := c.Session(); return s != nil && s.State().Phase == huddle.Connected })
	t.Cleanup(c.Leave)
	link(HuddleLink{Channel: dmChannel}, "in the DM huddle")

	// rung in #huddle-test while in it: no sound (you're in a huddle), one
	// unread invite, the link bold with the inviting channel and the badge
	invite(hcChannel, inviteCall)
	e.shown("the invite")
	if n := c.UnreadInvites(); n != 1 {
		t.Fatalf("%d unread invites after one huddle_invite", n)
	}
	if len(e.h.ringed()) != 0 {
		t.Fatalf("an invite rang while you're in a huddle: %q", e.h.ringed())
	}
	link(HuddleLink{Unread: true, Channel: hcChannel, Badge: 1}, "invited, huddles_link_v2 on")
	e.app.SetExperimentGroup(ExpHuddlesLinkV2, "")
	link(HuddleLink{Unread: true, Channel: hcChannel}, "invited, huddles_link_v2 unassigned")
	// the page marks nothing with the experiment off
	c.HuddlesPage(true)
	time.Sleep(3 * markInvitesDelay / 2)
	c.HuddlesPage(true)
	if n := c.UnreadInvites(); n != 1 {
		t.Fatalf("huddles_link_v2 off: the page marked invites seen (%d left)", n)
	}
	c.HuddlesPage(false)
	e.app.SetExperimentGroup(ExpHuddlesLinkV2, "on")

	// the same invite again is ignored: still one
	invite(hcChannel, inviteCall)
	time.Sleep(50 * time.Millisecond)
	if n := c.UnreadInvites(); n != 1 {
		t.Fatalf("a repeated invite counted again: %d", n)
	}

	// the Huddles page: 600 ms on, what came in before is seen
	at := time.Now()
	c.HuddlesPage(true)
	if n := c.UnreadInvites(); n != 1 {
		t.Fatalf("marked at once: %d left", n)
	}
	waitFor(t, "the page's mark", func() bool { c.HuddlesPage(true); return c.UnreadInvites() == 0 })
	if d := time.Since(at); d < markInvitesDelay {
		t.Fatalf("marked after %v, before Slack's %v", d, markInvitesDelay)
	}
	link(HuddleLink{Unread: true, Channel: hcChannel}, "seen: the faces again")

	// a new call while the page is up is seen too
	invite(hcChannel, "R0INVITE02")
	waitFor(t, "the second invite", func() bool { return c.UnreadInvites() == 1 })
	waitFor(t, "the second mark", func() bool { c.HuddlesPage(true); return c.UnreadInvites() == 0 })

	// huddle_invite_cancel drops it; the same call ringing again while the
	// page stays up isn't new to it (useMarkHuddlesNotifications' ref), so
	// it stays unread until the page mounts again
	raw, _ := json.Marshal(map[string]any{"type": "huddle_invite_cancel", "channel_id": hcChannel})
	c.Handle(core.Event{Type: "huddle", Name: "huddle_invite_cancel", Raw: raw})
	if _, ok := c.Incoming(); ok {
		t.Fatal("huddle_invite_cancel left the invite")
	}
	link(HuddleLink{Channel: dmChannel}, "cancelled")
	invite(hcChannel, "R0INVITE02")
	e.shown("the invite again")
	for range 8 {
		c.HuddlesPage(true)
		time.Sleep(markInvitesDelay / 4)
	}
	if n := c.UnreadInvites(); n != 1 {
		t.Fatalf("a call the page had seen was marked again: %d left", n)
	}
	c.HuddlesPage(false)
	c.HuddlesPage(true)
	waitFor(t, "the remount's mark", func() bool { return c.UnreadInvites() == 0 })
	c.HuddlesPage(false)

	// off the page, invites pile up; Decline drops the channel's
	invite(hcChannel, "R0INVITE03")
	waitFor(t, "a third invite", func() bool { return c.UnreadInvites() == 1 })
	link(HuddleLink{Unread: true, Channel: hcChannel, Badge: 1}, "off the page")
	c.DeclineInvite()
	waitFor(t, "declined", func() bool { _, ok := c.Incoming(); return !ok && c.UnreadInvites() == 0 })
	link(HuddleLink{Channel: dmChannel}, "declined")

	// starting the huddle you're rung for clears it (the orchestrator's
	// start): leave the DM first, so the invite doesn't drop on the leave
	c.Leave()
	waitFor(t, "left", func() bool { return c.Session() == nil })
	invite(hcChannel, "R0INVITE04")
	waitFor(t, "a fourth invite", func() bool { return c.UnreadInvites() == 1 })
	c.Start(hcChannel)
	if n := c.UnreadInvites(); n != 0 {
		t.Fatalf("starting the huddle left %d unread invites", n)
	}
	if _, ok := c.Incoming(); ok {
		t.Fatal("starting the huddle left the invite up")
	}
	// connected before the cleanup's leave (a leave mid-offer fails the fake's negotiation)
	waitFor(t, "the channel huddle", func() bool {
		s := c.Session()
		return s != nil && s.Channel() == hcChannel && s.State().Phase == huddle.Connected
	})
}

// getChannelIdForHuddleLinkFacepile without an invite: the huddle with
// people in it that started first, among the conversations you can be
// rung for; nothing for a live room with nobody in it.
func TestHuddleLinkFacepileChannel(t *testing.T) {
	e := newInviteEnv(t, nil)
	c := e.c
	room := func(ch, id string, start int64, people ...string) {
		raw, _ := json.Marshal(map[string]any{"user": huddle.FakeRemoteUser, "room": map[string]any{"id": id, "channels": []string{ch},
			"participants": people, "date_start": start, "name": ""}})
		c.Handle(core.Event{Type: "huddle", Name: "sh_room_join", Channel: ch, Raw: raw})
	}
	room(hcChannel, "R0LATE", 2000, huddle.FakeRemoteUser)
	room(dmChannel, "R0EARLY", 1000, huddle.FakeRemoteUser)
	if got := c.Link().Channel; got != dmChannel {
		t.Fatalf("facepile channel %q, want the earlier huddle's %s", got, dmChannel)
	}
	room(dmChannel, "R0EARLY", 1000)
	if got := c.Link().Channel; got != hcChannel {
		t.Fatalf("facepile channel %q once the DM huddle emptied, want %s", got, hcChannel)
	}
}
