package huddleui

import (
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/huddle"
)

// The invite's notification as Slack's desktopHuddleNotificationOptions
// hands it to the shell: the conversation, whose picture, the call, and
// actionButtons (Dismiss and Join; Decline too under
// desktop_notification_actions, with no URI: on Windows only a dismiss).
// The missed one's: your own picture, Dismiss and View.
func TestInviteNotificationButtons(t *testing.T) {
	e := newInviteEnv(t, nil)
	team := e.c.teamID()
	e.invite(dmChannel)
	e.shown("the invite")
	waitFor(t, "the notification", func() bool { return len(e.h.notifications()) == 1 })
	n := e.h.notifications()[0]
	if n.Channel != dmChannel || n.User != huddle.FakeRemoteUser || n.Call != inviteCall || n.Missed {
		t.Errorf("the invite's payload: %+v", n)
	}
	keys := func(bs []NotifyAction) string {
		var out []string
		for _, b := range bs {
			out = append(out, b.Key+"="+b.Label+"|"+b.URI)
		}
		return strings.Join(out, ", ")
	}
	want := "Dismiss=Dismiss|, Join=Join|slack://huddle?id=" + dmChannel + "&team=" + team
	if got := keys(n.Buttons); got != want {
		t.Errorf("buttons %s, want %s", got, want)
	}
	if n.Buttons[1].Do == nil || n.Buttons[0].Do != nil {
		t.Errorf("Join acts and Dismiss doesn't: %+v", n.Buttons)
	}
	if len(n.Actions) != 2 || n.Actions[0].Key != "join" || n.Actions[1].Key != "decline" {
		t.Errorf("the Linux buttons changed: %+v", n.Actions)
	}
	// sharedHuddleNotificationOptions' subtitle (macOS shows it): a DM's
	// partner, by the name Slack shows (core's display name)
	if who := e.c.displayName(huddle.FakeRemoteUser); n.Subtitle != who {
		t.Errorf("the DM invite's subtitle %q, want the partner's name %q", n.Subtitle, who)
	}

	e.app.SetExperimentGroup(expNotificationActions, "on")
	e.app.SetExperimentGroup(ExpMissedNotification, "on")
	e.c.clearInvite(dmChannel)
	e.c.MissedAfter = 150 * time.Millisecond
	e.invite(hcChannel)
	e.shown("the second invite")
	waitFor(t, "its notification", func() bool { return len(e.h.notifications()) == 2 })
	n = e.h.notifications()[1]
	want = "Dismiss=Dismiss|, Join=Join|slack://huddle?id=" + hcChannel + "&team=" + team + ", Decline=Decline|"
	if got := keys(n.Buttons); got != want {
		t.Errorf("under desktop_notification_actions: %s, want %s", got, want)
	}
	// a press of Decline (macOS: handleNotificationAction) declines
	if n.Buttons[2].Do == nil || n.Subtitle != "#huddle-test" {
		t.Errorf("Decline acts: %t; the channel invite's subtitle %q, want #huddle-test", n.Buttons[2].Do != nil, n.Subtitle)
	}
	waitFor(t, "the missed notice", func() bool { return len(e.h.notifications()) == 3 })
	m := e.h.notifications()[2]
	want = "Dismiss=Dismiss|, View=View|slack://channel?team=" + team + "&id=" + hcChannel
	if got := keys(m.Buttons); got != want || !m.Missed || m.User != huddle.FakeSelfUser || m.Channel != hcChannel || m.Subtitle != "" {
		t.Errorf("missed: %+v buttons %s, want %s and no subtitle", m, got, want)
	}
}
