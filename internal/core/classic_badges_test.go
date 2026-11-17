package core

import (
	"encoding/json"
	"testing"

	"github.com/some-du6e/clack/internal/slack"
)

// The badges without the activity inbox (classic_badges.go), against
// Slack's 3x6A getActivityBadgeCount, getAppBadgeCount and ZVw+
// getTotalUnreadHighlights. Synthetic counts only.

// classicApp is testApp without the activity inbox (core's default
// groups, defaultExperimentGroups, have it on, as the account does).
func classicApp(t *testing.T) *App {
	t.Helper()
	a, _ := testApp(t)
	a.SetExperimentGroup("activity_inbox", "off")
	if a.ActivityInbox() {
		t.Fatal("the activity inbox is still on")
	}
	return a
}

func TestClassicActivityBadgeLikeSlack(t *testing.T) {
	a := classicApp(t)
	a.SetExperimentGroup("lists_monster_step", "") // not assigned (the account's default is on): the to-dos come below
	a.mergeChannelBadges(map[string]int{"channels": 5, "thread_mentions": 1, "app_dms": 2, "thread_unreads": 4, "dms": 9})
	a.resetAlerts(map[string]int{"external_channel_invite": 1, "quietly_added_to_channel": 3, "lists_todo_notification": 2,
		"lists_approval_request": 1, "prejoin_dm_welcome_party_alert": 1, "not_an_alert_type": 7})
	check := func(name string, count int, dot bool) {
		t.Helper()
		if n := a.ActivityBadgeCount(); n != count {
			t.Errorf("%s: Activity badge %d, want %d", name, n, count)
		}
		if d := a.ActivityDot(); d != dot {
			t.Errorf("%s: Activity dot %v, want %v", name, d, dot)
		}
	}
	// thread_mentions + app_dms + channels, the listed alerts (not
	// quietly_added_to_channel; to-dos and approvals behind experiments);
	// not dms (activity_dms only with the inbox) or thread_unreads
	check("classic", 1+2+5+1+1, false)
	a.SetExperimentGroup("lists_monster_step", "on") // Hm0j Dq: the to-do alerts
	check("to-dos", 1+2+5+1+1+2, false)
	a.SetExperimentGroup("approvals_in_todos", "on") // Hm0j ci: the approvals'
	check("approvals", 1+2+5+1+1+2+1, false)
	a.SetExperimentGroup("thread_badging_home_sidebar", "on") // O2By Dn: thread_unreads count
	check("thread badging", 1+2+5+1+1+2+1+4, false)
	a.SetExperimentGroup("dual_badging_in_home_sidebar", "on") // Dn needs dual badging off
	check("dual badging", 1+2+5+1+1+2+1, false)
	a.SetExperimentGroup("dual_badging_in_home_sidebar", "off")
	a.SetExperimentGroup("sidebar_badging_variants_v1", "number_total_count") // isSidebarBadgingVariant: dual too
	check("badging variant", 1+2+5+1+1+2+1, false)
	a.SetExperimentGroup("thread_badging_home_sidebar", "off")
	a.SetExperimentGroup("sidebar_badging_variants_v1", "")
	a.installPrefs(map[string]json.RawMessage{"activity_include_threads_in_badge_count": json.RawMessage("true")})
	check("the pref", 1+2+5+1+1+2+1+4, false)

	// only thread unreads the badges don't count: InjD's dot
	a.installPrefs(map[string]json.RawMessage{})
	a.mergeChannelBadges(map[string]int{"channels": 0, "thread_mentions": 0, "app_dms": 0, "thread_unreads": 3})
	a.resetAlerts(nil) // an answer without alerts clears them (h1EQ `alerts || {}`)
	check("thread unreads only", 0, true)
	a.installPrefs(map[string]json.RawMessage{"activity_include_threads_in_badge_count": json.RawMessage("true")})
	check("thread unreads counted", 3, false)

	// the inbox reads activity_v2 instead, and draws no such dot
	a.installPrefs(map[string]json.RawMessage{})
	a.SetExperimentGroup("activity_inbox", "on")
	a.setActivityBadges(map[string]int{"at_user": 2})
	check("inbox", 2, false)
}

func TestClassicAppBadgeLikeSlack(t *testing.T) {
	a := classicApp(t)
	// before client.counts: the DMs' own highlights only
	if n := a.AppBadgeCount(3); n != 3 {
		t.Errorf("no channel_badges yet: app badge %d, want the DMs' 3", n)
	}
	a.mergeChannelBadges(map[string]int{"channels": 5, "thread_mentions": 1, "app_dms": 2, "thread_unreads": 4, "dms": 9})
	// ZVw+: the DMs' highlights (the caller's, bot DMs left out) plus
	// channels + thread_mentions + app_dms; not channel_badges' dms (the
	// client sums those) or thread_unreads
	if n := a.AppBadgeCount(3); n != 3+5+1+2 {
		t.Errorf("classic app badge %d, want %d", n, 3+5+1+2)
	}
	if n := a.ChannelBadgeHighlights(); n != 5+1+2 {
		t.Errorf("ChannelBadgeHighlights %d, want %d", n, 5+1+2)
	}
	a.installPrefs(map[string]json.RawMessage{"activity_include_threads_in_badge_count": json.RawMessage("true")})
	if n := a.AppBadgeCount(3); n != 3+5+1+2+4 {
		t.Errorf("threads counted: app badge %d, want %d (totalUnreadHighlightsWithoutDms)", n, 3+5+1+2+4)
	}
	// the inbox's app badge is the Activity badge's: channel_badges aren't in it
	a.SetExperimentGroup("activity_inbox", "on")
	a.setActivityBadges(map[string]int{"at_user": 2})
	if n := a.AppBadgeCount(3); n != 2 {
		t.Errorf("inbox: app badge %d, want the Activity badge's 2", n)
	}
	if n := a.ChannelBadgeHighlights(); n != 0 {
		t.Errorf("inbox: ChannelBadgeHighlights %d, want 0", n)
	}
}

// client.counts' channel_badges merge over what's held; its alerts replace
// them (only known types). Without the inbox badge_counts_updated brings
// both, the way handleBadgeCountsUpdatedRTMEvent applies them, and not
// its activity_v2; under the inbox only its activity_v2.
func TestClassicBadgesUpdatesLikeSlack(t *testing.T) {
	a := classicApp(t)
	a.installCounts(&slack.Counts{ChannelBadges: map[string]int{"channels": 2, "app_dms": 1},
		Alerts: map[string]int{"external_dm_invite": 1, "not_an_alert_type": 4}}, 0)
	if n := a.ActivityBadgeCount(); n != 2+1+1 {
		t.Fatalf("after client.counts: Activity badge %d, want %d", n, 2+1+1)
	}
	ev, _ := json.Marshal(map[string]any{"type": "badge_counts_updated", "counts": map[string]int{"channels": 4},
		"alerts": map[string]int{"external_dm_invite": 0, "unjoined_channel_mention": 2}, "activity_v2": map[string]int{"at_user": 9}})
	a.onActivityBadges(ev)
	// channels 4 over app_dms 1 kept; external_dm_invite 0, unjoined 2
	if n := a.ActivityBadgeCount(); n != 4+1+2 {
		t.Errorf("after badge_counts_updated: Activity badge %d, want %d", n, 4+1+2)
	}
	if c, ok := a.ActivityTabCounts(); ok {
		t.Errorf("classic badge_counts_updated applied activity_v2 %v: Slack reads it only under the inbox", c)
	}
	// an answer without alerts clears them; channel_badges merge again
	a.installCounts(&slack.Counts{ChannelBadges: map[string]int{"app_dms": 0}}, 0)
	if n := a.ActivityBadgeCount(); n != 4 {
		t.Errorf("after an answer without alerts: Activity badge %d, want channels' 4", n)
	}
	// under the inbox the event's counts and alerts aren't read
	a.SetExperimentGroup("activity_inbox", "on")
	ev, _ = json.Marshal(map[string]any{"type": "badge_counts_updated", "counts": map[string]int{"channels": 40},
		"activity_v2": map[string]int{"at_user": 3}})
	a.onActivityBadges(ev)
	if n := a.ActivityBadgeCount(); n != 3 {
		t.Errorf("inbox: Activity badge %d, want activity_v2's 3", n)
	}
	a.SetExperimentGroup("activity_inbox", "off")
	if n := a.ActivityBadgeCount(); n != 4 {
		t.Errorf("the inbox's event changed channel_badges: classic badge %d, want 4", n)
	}
}

// BotDM is DUy0 G isBotDm alone (ZVw+ leaves those out of the app
// badge); the search user's and the security bot's IMs are no bot DMs,
// though the DMs tab skips them.
func TestBotDMIsDUy0G(t *testing.T) {
	for _, c := range []struct {
		name   string
		user   string
		member *slack.User
		want   bool
	}{
		{"a person", "U1", &slack.User{ID: "U1"}, false},
		{"Slackbot's IM, member unknown", "USLACKBOT", nil, true},
		{"the Slack system user, member unknown", "USLACK", nil, true},
		{"an app's bot user", "UB", &slack.User{ID: "UB", IsBot: true}, true},
		{"an app's bot user the store doesn't hold", "UB", nil, false},
		{"is_slackbot", "UX", &slack.User{ID: "UX", IsSlackbot: true}, true},
		{"the search user", "USEARCH", &slack.User{ID: "USEARCH"}, false},
		{"the security bot", "USLACKSECURITY", &slack.User{ID: "USLACKSECURITY"}, false},
	} {
		if got := isBotDM(c.user, c.member); got != c.want {
			t.Errorf("%s: isBotDM %v, want %v", c.name, got, c.want)
		}
	}
	a, _ := testApp(t)
	a.S.PutUsers([]slack.User{{ID: "USLACK", Name: "slack"}, {ID: "USEARCH", Name: "search"}})
	for _, c := range []slack.Conversation{
		{ID: "DSLACK", IsIM: true, User: "USLACK"}, {ID: "DSEARCH", IsIM: true, User: "USEARCH"}, {ID: "DBOT", IsIM: true, User: "UB"},
	} {
		a.S.PutConv(c)
	}
	close(a.ready)
	want := map[string][3]bool{"D1": {false, false, false}, "DSLACK": {true, true, true}, "DSEARCH": {true, false, false},
		"DBOT": {true, true, true}}
	for _, c := range a.Snapshot().Convs {
		w, ok := want[c.ID]
		if !ok {
			continue
		}
		if got := [3]bool{c.NoDMsBadge, c.NoDMsCount, c.BotDM}; got != w {
			t.Errorf("%s: NoDMsBadge, NoDMsCount, BotDM %v, want %v", c.ID, got, w)
		}
	}
}
