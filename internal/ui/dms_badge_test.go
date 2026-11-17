package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
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
// in). Without the inbox the app badge (3x6A getAppBadgeCount, ZVw+) and
// the Activity tab (getActivityBadgeCount) read the server's
// channel_badges and alerts, and the app badge skips the bot DMs too
// (core classic_badges.go). The fakeslack DMs (Size.DMBadges) are
// synthetic.

// waitDMsBadge waits for the DMs tab's badge to read want, failing with
// what it read last. Slack's draws a count or nothing there, never a dot
// (InjD useGetMentions, outside the compact rail).
func waitDMsBadge(t *testing.T, e *perfEnv, what string, want int, d time.Duration) {
	t.Helper()
	waitRailBadge(t, e, "dms", what, want, false, d)
}

// waitRailBadge waits for rail tab key's badge to read (want, dot).
func waitRailBadge(t *testing.T, e *perfEnv, key, what string, want int, dot bool, d time.Duration) {
	t.Helper()
	dl := time.Now().Add(d)
	for {
		e.frame()
		n, gotDot := e.a.railBadge(key)
		if n == want && gotDot == dot {
			return
		}
		if time.Now().After(dl) {
			t.Fatalf("%s: the %s tab's badge reads %d (dot %v), want %d (dot %v)", what, key, n, gotDot, want, dot)
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

// dmBadgesEnv boots Small with the DMs page and the DM badges' fixtures;
// set runs on the fake before the client starts.
func dmBadgesEnv(t *testing.T, set func(fs *fakeslack.Server)) (*perfEnv, *fakeslack.Server) {
	t.Helper()
	sz := fakeslack.Small
	sz.DMsTab, sz.DMBadges = true, true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	if set != nil {
		set(fs)
	}
	e := startEnv(t, fs, t.TempDir())
	e.waitReady()
	return e, fs
}

// The bot DMs the badges leave out.
var dmBadgeBots = []string{fakeslack.DMBadgeSlackbotID, fakeslack.DMBadgeSlackID, fakeslack.DMBadgeAppID}

// waitDMBadgeFixtures waits for the fixtures' counts and members: the bot
// DMs and the muted DM unread, users/info answered for the app's and the
// Slack system user's members.
func waitDMBadgeFixtures(t *testing.T, e *perfEnv) {
	t.Helper()
	e.until("the bot DMs unread, their members known", 20*time.Second, func() bool {
		m := e.a.model()
		for _, id := range append(slices.Clone(dmBadgeBots), fakeslack.DMBadgeMutedID) {
			if c := m.conv(id); c == nil || !c.Unread {
				return false
			}
		}
		app, sys := m.conv(fakeslack.DMBadgeAppID), m.conv(fakeslack.DMBadgeSlackID)
		return app.Kind == "app" && sys.Name == "Slack"
	})
	m := e.a.model()
	if c := m.conv(fakeslack.DMBadgeMutedID); !c.Muted {
		t.Fatalf("the muted DM %+v isn't muted", c)
	}
	for _, id := range []string{fakeslack.DMBadgePersonID, fakeslack.DMBadgeGroupID} {
		if c := m.conv(id); c == nil || c.Unread || c.Mentions != 0 {
			t.Fatalf("%s %+v, want it read to start with", id, c)
		}
	}
}

// dmPost adds a message from user to DM ch and delivers it as the socket
// would (the UI tests run without one); it returns the message's ts.
func dmPost(e *perfEnv, ch, user, text string) string {
	ts, ev := e.fs.PostQuiet(ch, user, text)
	raw, _ := json.Marshal(ev)
	e.a.Core().Inject(slack.Event{Type: "message", Channel: json.RawMessage(strconv.Quote(ch)), Raw: raw})
	return ts
}

// postRobinAndGroup has Robin write three messages in their DM and Avery
// one in the group DM, and waits for both counts.
func postRobinAndGroup(e *perfEnv) (robin, group string) {
	for i := range 3 {
		robin = dmPost(e, fakeslack.DMBadgePersonID, fakeslack.ProfileID, fmt.Sprintf("a synthetic question %d", i+1))
	}
	group = dmPost(e, fakeslack.DMBadgeGroupID, fakeslack.AdminID, "a synthetic group note")
	e.until("Robin's and the group's DMs unread", 10*time.Second, func() bool {
		m := e.a.model()
		p, g := m.conv(fakeslack.DMBadgePersonID), m.conv(fakeslack.DMBadgeGroupID)
		return p != nil && p.Mentions == 3 && g != nil && g.Mentions == 1
	})
	return robin, group
}

func TestDMsBadgeLikeSlack(t *testing.T) {
	e, fs := dmBadgesEnv(t, nil)
	e.a.Core().SetExperimentGroup("activity_inbox", "on") // the account's (the fake doesn't assign it)
	e.until("the inbox rail", 5*time.Second, func() bool {
		r := e.a.model().Rail
		return r.ActivityInbox && r.DMsInRail
	})
	e.a.Navigate("dms")
	waitDMBadgeFixtures(t, e)
	person, group, muted := fakeslack.DMBadgePersonID, fakeslack.DMBadgeGroupID, fakeslack.DMBadgeMutedID

	// Slackbot's, the Slack system user's and the app's DMs are unread
	// (five messages), and a muted person's (two): Slack's tab shows
	// nothing. The DMs page lists none of the bot DMs; it draws the muted
	// DM unread, as Slack's does (its rows read the raw unread count,
	// JDfs), though a muted DM never badges (siMV)
	waitDMsBadge(t, e, "only bot and muted DMs unread", 0, 10*time.Second)
	e.settle(300 * time.Millisecond)
	waitDMsBadge(t, e, "only bot and muted DMs unread, settled", 0, 0)
	ids, unread := dmsPage(e)
	for _, id := range dmBadgeBots {
		if slices.Contains(ids, id) {
			t.Errorf("the DMs page lists %s (a bot DM: DUy0 isBotDm)", id)
		}
	}
	for _, id := range []string{person, group, muted} {
		if !slices.Contains(ids, id) {
			t.Errorf("the DMs page doesn't list %s: %v", id, ids)
		}
	}
	if !slices.Equal(unread, []string{muted}) {
		t.Errorf("the DMs page draws %v unread, want only the muted DM %s", unread, muted)
	}

	// Robin writes three messages and Avery one in the group DM. Under the
	// inbox the tab counts unread conversations (6Z4k Sm): Robin's, the
	// group's and Slackbot's (Sm keeps Slackbot's own IM), not the Slack
	// system user's, the app's or the muted one: 3, where the highlights
	// would be 4. The DMs page draws Robin's and the group's rows unread.
	robinTS, groupTS := postRobinAndGroup(e)
	waitDMsBadge(t, e, "Robin's and the group's DMs unread too, under the inbox", 3, 5*time.Second)
	e.until("their rows unread", 5*time.Second, func() bool {
		_, unread = dmsPage(e)
		return len(unread) == 3
	})
	for _, id := range []string{person, group, muted} {
		if !slices.Contains(unread, id) {
			t.Errorf("the DMs page draws %v unread, want %s among them", unread, id)
		}
	}

	// opening Robin's DM reads it: the group's and Slackbot's still count;
	// opening the group DM too leaves only DMs the badge leaves out unread
	e.openChannel(person)
	e.until("Robin's DM marked read", 20*time.Second, func() bool { return fs.Marked(person) == robinTS })
	waitDMsBadge(t, e, "Robin's DM read", 2, 10*time.Second)
	e.openChannel(group)
	e.until("the group DM marked read", 20*time.Second, func() bool { return fs.Marked(group) == groupTS })
	waitDMsBadge(t, e, "Robin's and the group's DMs read", 0, 10*time.Second)
	for _, id := range append(slices.Clone(dmBadgeBots), muted) {
		if c := e.a.model().conv(id); c == nil || !c.Unread {
			t.Errorf("%s read too: the test wants it still unread", id)
		}
	}
	e.a.Navigate("dms")
	e.until("Robin's and the group's rows read", 10*time.Second, func() bool {
		_, unread = dmsPage(e)
		return slices.Equal(unread, []string{muted})
	})
	e.settle(300 * time.Millisecond)
	waitDMsBadge(t, e, "Robin's and the group's DMs read, back on the DMs page", 0, 0)
}

// Without the activity inbox: the DMs tab, Home and More count the DMs'
// highlights (messages, not conversations), and the Activity tab and the
// app badge read the server's channel_badges and alerts (3x6A
// getActivityBadgeCount, getAppBadgeCount over ZVw+), the app badge
// without the bot DMs.
func TestDMBadgesClassicLikeSlack(t *testing.T) {
	badges := map[string]int{"channels": 7, "thread_mentions": 2, "app_dms": 1, "thread_unreads": 3, "dms": 11}
	e, fs := dmBadgesEnv(t, func(fs *fakeslack.Server) {
		fs.SetChannelBadges(badges)
		fs.SetAlerts(map[string]int{"external_channel_invite": 1, "quietly_added_to_channel": 1})
	})
	e.a.Core().SetExperimentGroup("activity_inbox", "off") // Slack's classic Activity
	e.until("the classic rail", 5*time.Second, func() bool { return !e.a.model().Rail.ActivityInbox })
	waitDMBadgeFixtures(t, e)
	postRobinAndGroup(e)

	// the DMs tab: the highlights (rHEx dmsBadgeCount), Robin's three
	// messages and the group's one; Sm would count 3 conversations
	waitDMsBadge(t, e, "classic", 3+1, 5*time.Second)
	// Activity: thread_mentions + app_dms + channels and the listed alerts
	// (not quietly_added_to_channel), not the channels' own mentions
	activity := 2 + 1 + 7 + 1
	waitRailBadge(t, e, "activity", "the classic Activity badge", activity, false, 10*time.Second)
	// the app badge: the DMs' highlights but the bot DMs' (ZVw+ skips DUy0
	// G's), plus channel_badges' channels + thread_mentions + app_dms; not
	// thread_unreads (threads don't count here) or channel_badges' dms
	app := 3 + 1 + 7 + 2 + 1
	e.until("the classic app badge", 10*time.Second, func() bool { return e.a.desktopUnreads().Count == app })
	if title := e.a.windowTitle(); !strings.Contains(title, " - "+strconv.Itoa(app)+" new items - ") {
		t.Errorf("window title %q, want %d new items", title, app)
	}

	// Home with the DMs tab off the rail counts the DMs' highlights too
	// (InjD n[DMs])
	if err := e.a.Core().SetPref(e.a.data.ctx, "tab_rail_customization", `{"dms":false}`); err != nil {
		t.Fatal(err)
	}
	e.until("the DMs tab off the rail", 5*time.Second, func() bool { return !e.a.model().Rail.DMsInRail })
	waitRailBadge(t, e, "home", "Home with the DMs tab off the rail", 3+1, false, 5*time.Second)

	// in the rail again, but under More for the height: More adds the DMs'
	// highlights (yEMG) and, under it too, Activity's count
	if err := e.a.Core().SetPref(e.a.data.ctx, "tab_rail_customization", `{"dms":true}`); err != nil {
		t.Fatal(err)
	}
	e.until("the DMs tab back in the rail", 5*time.Second, func() bool { return e.a.model().Rail.DMsInRail })
	e.size = image.Pt(1440, 360)
	e.until("the DMs tab under More", 5*time.Second, func() bool { return slices.Contains(e.a.railOverflow, core.TabDMs) })
	more := 3 + 1
	if slices.Contains(e.a.railOverflow, core.TabActivity) {
		more += activity
	}
	waitRailBadge(t, e, "browse", "the DMs tab under More", more, false, 5*time.Second)
	e.size = image.Pt(1440, 900)

	// only thread unreads the badges don't count: Activity draws a dot
	// (InjD), from badge_counts_updated's counts and alerts
	zero := map[string]int{"channels": 0, "thread_mentions": 0, "app_dms": 0, "thread_unreads": 3, "dms": 0}
	fs.SetChannelBadges(zero)
	fs.SetAlerts(map[string]int{})
	raw, _ := json.Marshal(map[string]any{"type": "badge_counts_updated", "counts": zero,
		"alerts": map[string]int{"external_channel_invite": 0, "quietly_added_to_channel": 0}})
	e.a.Core().Inject(slack.Event{Type: "badge_counts_updated", Raw: raw})
	waitRailBadge(t, e, "activity", "only thread unreads", 0, true, 10*time.Second)
	e.until("the app badge without channel_badges", 10*time.Second, func() bool { return e.a.desktopUnreads().Count == 3+1 })
}

// An app's DM nothing lists (closed and read) gets its member asked for
// once it's unread, the way Slack's badge selectors fetch it (DUy0 G, 6Z4k
// Sm getMemberById). Here another client marks it unread: no new message,
// so it doesn't reopen, and only a client.counts answer tells of it. While
// users/info is out the badges count it as a person's DM, as Slack's do
// with the unknown member's stub; once it loads, it badges nothing. (The
// sidebar's next build lists a closed DM with unreads and asks for its
// member as well, so this passes without the badges' own lookups too: the
// core tests in dm_badge_members_test.go are the ones that need them.)
func TestDMBadgeMemberLoadsLate(t *testing.T) {
	release := make(chan struct{})
	var once sync.Once
	var asked atomic.Bool
	e, fs := dmBadgesEnv(t, func(fs *fakeslack.Server) {
		fs.SetEdgeHook(func(method string, body map[string]json.RawMessage) (any, bool) {
			if method == "edge/users/info" && strings.Contains(string(body["updated_ids"]), fakeslack.DMBadgeLateBotID) {
				asked.Store(true)
				<-release
			}
			return nil, false
		})
	})
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	e.a.Core().SetExperimentGroup("activity_inbox", "on")
	e.until("the inbox rail", 5*time.Second, func() bool { return e.a.model().Rail.ActivityInbox })
	waitDMBadgeFixtures(t, e)
	late := fakeslack.DMBadgeLateAppID
	if c := e.a.model().conv(late); c == nil || c.Unread {
		t.Fatalf("the late app's DM %+v, want it read to start with", c)
	}
	waitDMsBadge(t, e, "the late app's DM read", 0, 10*time.Second)
	if asked.Load() {
		t.Fatal("users/info was asked for the closed, read DM's member: the test wants nothing to have")
	}

	// "Mark unread" on another client, which only a client.counts answer
	// tells of here
	e.settle(time.Second) // boot's own lookups and snapshots are done
	if err := slack.New(fs.Creds()).Call(context.Background(), "conversations.mark",
		url.Values{"channel": {late}, "ts": {"1700000000.000000"}}, nil); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"type": "thread_marked"})
	e.a.Core().Inject(slack.Event{Type: "thread_marked", Raw: raw}) // client.counts again (refreshCountsSoon)
	e.until("the late app's DM unread", 10*time.Second, func() bool {
		c := e.a.model().conv(late)
		return c != nil && c.Unread
	})
	e.until("users/info asked for the late app's bot user", 10*time.Second, func() bool { return asked.Load() })
	// the member still loading: a person's DM to the badges, so the inbox
	// counts it with Slackbot's
	waitDMsBadge(t, e, "the late app's member still loading", 2, 5*time.Second)
	once.Do(func() { close(release) })
	e.until("the late app's member known", 10*time.Second, func() bool {
		c := e.a.model().conv(late)
		return c != nil && c.Kind == "app"
	})
	waitDMsBadge(t, e, "the late app's member loaded", 0, 10*time.Second)
}
