package core

// Activity's live events and badges as Slack's (manage.go, activity_badges.go):
// the V1 allow-list without the activity inbox, is_breakthrough, and the
// All badge's kinds. Synthetic data on internal/fakeslack.

import (
	"testing"
	"time"
)

// rsDMEntry is a dm bundle's activity_updated entry for ch, and
// rsMentionEntry an at_user one; breakthrough sets is_breakthrough.
func rsDMEntry(ch, ts string, breakthrough bool) map[string]any {
	return map[string]any{"type": "activity", "subtype": "activity_updated", "entry": map[string]any{
		"key": "dm-" + ch, "feed_ts": ts, "is_unread": true, "is_breakthrough": breakthrough,
		"item": map[string]any{"type": "dm", "bundle_info": map[string]any{"payload": map[string]any{
			"dm_entry": map[string]any{"channel_id": ch, "latest_message": map[string]any{"ts": ts, "channel": ch}}}}}}}
}

func rsMentionEntry(ch, ts string, breakthrough bool) map[string]any {
	return map[string]any{"type": "activity", "subtype": "activity_updated", "entry": map[string]any{
		"key": "at_user-" + ch + "-" + ts, "feed_ts": ts, "is_unread": true, "is_breakthrough": breakthrough,
		"item": map[string]any{"type": "at_user", "message": map[string]any{"ts": ts, "channel": ch, "author_user_id": "U0FAKE00001"}}}}
}

// heard reports whether the log has an activity event for key.
func heard(evs []Event, key string) bool {
	for _, ev := range evs {
		if ev.Type == "activity" && ev.ID == key {
			return true
		}
	}
	return false
}

// Without the activity inbox, activity_updated goes through
// activityUpdatedV1's list, which has no dm, channel or mpdm: a DM's
// bundle is dropped, a mention isn't. With the inbox (V2) both reach the
// stores.
func TestActivityV1AllowList(t *testing.T) {
	a, fs, l := rtApp(t)
	ch := fs.ChannelIDs()[1]
	now := time.Now().Unix()
	a.SetExperimentGroup("activity_inbox", "off")
	fs.Push(rsDMEntry(ch, fmtTS(now-60), false))
	fs.Push(rsMentionEntry(ch, fmtTS(now-50), false))
	l.wait(t, "the mention's entry", func(evs []Event) bool { return heard(evs, "at_user-"+ch+"-"+fmtTS(now-50)) })
	if heard(l.all(), "dm-"+ch) {
		t.Error("without the activity inbox a dm entry reached the stores (activityUpdatedV1's list has none)")
	}
	a.SetExperimentGroup("activity_inbox", "on")
	fs.Push(rsDMEntry(ch, fmtTS(now-40), false))
	l.wait(t, "the dm entry with the inbox", func(evs []Event) bool { return heard(evs, "dm-"+ch) })
}

// An entry with is_breakthrough tells the UI its conversation broke
// focus mode (addDndOverrideChannel), one the allow-list drops too.
func TestActivityBreakthrough(t *testing.T) {
	a, fs, l := rtApp(t)
	ch := fs.ChannelIDs()[1]
	now := time.Now().Unix()
	broke := func(evs []Event) bool {
		for _, ev := range evs {
			if ev.Type == "activity_breakthrough" && ev.Channel == ch {
				return true
			}
		}
		return false
	}
	fs.Push(rsMentionEntry(ch, fmtTS(now-30), true))
	l.wait(t, "the mention's breakthrough", broke)
	// dropped by V1's list, its breakthrough still counts
	a.SetExperimentGroup("activity_inbox", "off")
	n := len(l.all())
	fs.Push(rsDMEntry(ch, fmtTS(now-20), true))
	l.wait(t, "the dropped dm entry's breakthrough", func(evs []Event) bool { return broke(evs[n:]) })
}

// The All badge's kinds (9OWn j): to-dos with lists_monster_step, the
// approvals with approvals_in_todos, system alerts with
// generic_system_user_alert; message reactions never.
func TestActivityBadgeKinds(t *testing.T) {
	a, _, _ := rtApp(t)
	a.SetExperimentGroup("activity_inbox", "on")
	a.mu.Lock()
	a.actBadges = map[string]int{"at_user": 1, "generic_system_alert": 2, "list_approval_request": 4, "list_approval_reviewed": 8,
		"list_todo_notification": 16, "message_reaction": 32}
	a.mu.Unlock()
	for _, tc := range []struct {
		name          string
		system, apprv string
		todo          string
		want          int
	}{
		{"none of the experiments", "off", "off", "off", 1},
		{"generic_system_user_alert", "on", "off", "off", 1 + 2},
		{"approvals_in_todos", "off", "on", "off", 1 + 4 + 8},
		{"lists_monster_step", "off", "off", "on", 1 + 16},
		{"all three", "on", "on", "on", 1 + 2 + 4 + 8 + 16},
	} {
		a.SetExperimentGroup("generic_system_user_alert", tc.system)
		a.SetExperimentGroup("approvals_in_todos", tc.apprv)
		a.SetExperimentGroup("lists_monster_step", tc.todo)
		c, ok := a.ActivityTabCounts()
		if !ok || c[0] != tc.want {
			t.Errorf("%s: the All badge counts %d, want %d", tc.name, c[0], tc.want)
		}
		if got := a.ActivityBadgeCount(); got != tc.want {
			t.Errorf("%s: the rail's badge is %d, want %d", tc.name, got, tc.want)
		}
	}
}
