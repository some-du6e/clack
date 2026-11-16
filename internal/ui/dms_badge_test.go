package ui

import (
	"encoding/json"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/views"
)

// Karim, 2026-11-16: "i had two ghost pings in the dms tab/pill thingy, i
// only figured out what they were via going to the activity tab and
// selecting dms and it turns out it was a slack dm". The real account has
// an open DM with the Slack system user (USLACK, "Slack", served is_bot
// false). Clack's rail summed every unread DM into the DMs tab's badge;
// its DMs page, like Slack's, lists none of the bot DMs (core
// dmsTabShows, DUy0 isBotDm). Slack's tab shows a count only while
// boot-shared rHEx dmsBadgeCount, which skips them, is above 0, and under
// the activity inbox the count is client-boot 6Z4k getTotalUnreadDmsMpims,
// the unread conversations without the bots' (Slackbot's own IM stays
// in). The fakeslack DMs (Size.DMBadges) are synthetic.

// waitDMsBadge waits for the DMs tab's badge to read want, failing with
// what it read last. Slack's draws a count or nothing there, never a dot
// (InjD useGetMentions, outside the compact rail).
func waitDMsBadge(t *testing.T, e *perfEnv, what string, want int, d time.Duration) {
	t.Helper()
	dl := time.Now().Add(d)
	for {
		e.frame()
		n, dot := e.a.railBadge("dms")
		if n == want && !dot {
			return
		}
		if time.Now().After(dl) {
			t.Fatalf("%s: the DMs tab's badge reads %d (dot %v), want %d", what, n, dot, want)
		}
		time.Sleep(time.Millisecond)
	}
}

// dmsPage is the DMs page's rows once it has answered client.dms.
func dmsPage(e *perfEnv) (ids, unread []string) {
	var d *views.DMs
	e.until("the DMs page", 15*time.Second, func() bool {
		if d = pageView[*views.DMs](e, "dms"); d == nil {
			return false
		}
		_, _, ok := d.Listed()
		return ok
	})
	ids, unread, _ = d.Listed()
	return ids, unread
}

func TestDMsBadgeLikeSlack(t *testing.T) {
	sz := fakeslack.Small
	sz.DMsTab, sz.DMBadges = true, true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	e := startEnv(t, fs, t.TempDir())
	e.waitReady()
	e.a.Core().SetExperimentGroup("activity_inbox", "on") // the account's (the fake doesn't assign it)
	e.until("the inbox rail", 5*time.Second, func() bool {
		r := e.a.model().Rail
		return r.ActivityInbox && r.DMsInRail
	})
	e.a.Navigate("dms")

	bots := []string{fakeslack.DMBadgeSlackbotID, fakeslack.DMBadgeSlackID, fakeslack.DMBadgeAppID}
	person := fakeslack.DMBadgePersonID
	e.until("the bot DMs unread, their members known", 20*time.Second, func() bool {
		m := e.a.model()
		for _, id := range bots {
			if c := m.conv(id); c == nil || !c.Unread || c.Muted {
				return false
			}
		}
		app, sys := m.conv(fakeslack.DMBadgeAppID), m.conv(fakeslack.DMBadgeSlackID)
		return app.Kind == "app" && sys.Name == "Slack" // users/info answered for both
	})
	if c := e.a.model().conv(person); c == nil || c.Unread || c.Mentions != 0 {
		t.Fatalf("Robin's DM %+v, want it read to start with", c)
	}

	// Slackbot's, the Slack system user's and the app's DMs are unread
	// (five messages): Slack's tab shows nothing, and the DMs page lists
	// none of them
	waitDMsBadge(t, e, "only the bot DMs unread", 0, 10*time.Second)
	e.settle(300 * time.Millisecond)
	waitDMsBadge(t, e, "only the bot DMs unread, settled", 0, 0)
	ids, unread := dmsPage(e)
	for _, id := range bots {
		if slices.Contains(ids, id) {
			t.Errorf("the DMs page lists %s (a bot DM: DUy0 isBotDm)", id)
		}
	}
	if !slices.Contains(ids, person) {
		t.Errorf("the DMs page doesn't list Robin's DM %s: %v", person, ids)
	}
	if len(unread) != 0 {
		t.Errorf("the DMs page draws %v unread with no person's DM unread", unread)
	}

	// Robin writes: under the inbox the tab counts the unread DMs, Robin's
	// and Slackbot's (6Z4k Sm keeps Slackbot's own IM), not Slack's or the
	// app's. The DMs page draws Robin's row unread.
	ts, ev := fs.PostQuiet(person, fakeslack.ProfileID, "a synthetic question")
	raw, _ := json.Marshal(ev)
	e.a.Core().Inject(slack.Event{Type: "message", Channel: json.RawMessage(strconv.Quote(person)), Raw: raw})
	e.until("Robin's DM unread", 10*time.Second, func() bool {
		c := e.a.model().conv(person)
		return c != nil && c.Unread && c.Mentions == 1
	})
	waitDMsBadge(t, e, "Robin's DM unread too, under the inbox", 2, 5*time.Second)
	e.until("Robin's row unread", 5*time.Second, func() bool {
		_, unread = dmsPage(e)
		return len(unread) > 0
	})
	if len(unread) != 1 || unread[0] != person {
		t.Errorf("the DMs page draws %v unread, want Robin's %s only", unread, person)
	}

	// without the inbox the count is the highlights: Robin's one message
	e.a.Core().SetExperimentGroup("activity_inbox", "off")
	e.until("the classic rail", 5*time.Second, func() bool { return !e.a.model().Rail.ActivityInbox })
	waitDMsBadge(t, e, "Robin's DM unread too, without the inbox", 1, 5*time.Second)
	e.a.Core().SetExperimentGroup("activity_inbox", "on")
	e.until("the inbox rail again", 5*time.Second, func() bool { return e.a.model().Rail.ActivityInbox })

	// opening Robin's DM reads it: the badge goes while the bot DMs stay
	// unread, and Robin's row draws read
	e.openChannel(person)
	e.until("Robin's DM marked read", 20*time.Second, func() bool { return fs.Marked(person) == ts })
	waitDMsBadge(t, e, "Robin's DM read", 0, 10*time.Second)
	for _, id := range bots {
		if c := e.a.model().conv(id); c == nil || !c.Unread {
			t.Errorf("%s read too: the test wants the bot DMs still unread", id)
		}
	}
	e.a.Navigate("dms")
	e.until("Robin's row read", 10*time.Second, func() bool {
		_, unread = dmsPage(e)
		return len(unread) == 0
	})
	e.settle(300 * time.Millisecond)
	waitDMsBadge(t, e, "Robin's DM read, back on the DMs page", 0, 0)
}
