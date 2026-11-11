package core

// The Activity inbox's unread badge counts, as Slack's client keeps them
// (client-boot 3x6A, build 4d0c78085232): client.counts answers
// activity_v2, the unread count by entry type (h1EQ
// applyCountsToChannelsAndThreads → P0K2 updateActivityV2BadgeCounts),
// and badge_counts_updated brings new ones (zuQH
// handleBadgeCountsUpdatedRTMEvent). The tabs read them (cR).

import "encoding/json"

func (a *App) setActivityBadges(m map[string]int) {
	if m == nil {
		return
	}
	cp := make(map[string]int, len(m))
	for k, v := range m {
		cp[k] = v
	}
	a.mu.Lock()
	a.actBadges = cp
	a.mu.Unlock()
}

// onActivityBadges takes badge_counts_updated's activity_v2.
func (a *App) onActivityBadges(raw json.RawMessage) {
	var e struct {
		ActivityV2 map[string]int `json:"activity_v2"`
	}
	if json.Unmarshal(raw, &e) == nil && e.ActivityV2 != nil {
		a.setActivityBadges(e.ActivityV2)
	}
}

// activityBadgeTypes is 3x6A H's set: the entry types the All tab's count
// sums (message_reaction is in the set but never summed), with the ones
// behind a setting or an experiment as Clack has them.
func (a *App) activityBadgeTypes() map[string]bool {
	t := map[string]bool{}
	for _, k := range []string{"thread_v2", "channel", "internal_channel_invite", "list_record_edited", "bot_dm_bundle",
		"at_user", "at_user_group", "at_channel", "at_everyone", "keyword", "list_record_assigned", "list_user_mentioned",
		"external_channel_invite", "external_dm_invite", "shared_workspace_invite", "unjoined_channel_mention",
		"quietly_added_to_channel", "prejoin_dm_welcome_party_alert", "list_todo_notification"} {
		t[k] = true
	}
	if a.ActivityInbox() && a.GlobalNotifyBool("global_reminders_in_activity", true) {
		t["saved_reminder"] = true // 5Ns/ ah
	}
	if a.ActivityInbox() && !a.PrefBool("activity_dms_opt_out", false) {
		t["dm"] = true // 5Ns/ Vn
	}
	return t
}

// activityMentionTypes is 3x6A V: the Mentions tab's sum.
var activityMentionTypes = []string{"at_user", "at_channel", "at_everyone", "at_user_group", "keyword", "unjoined_channel_mention",
	"code_review_requested", "list_record_assigned", "list_user_mentioned", "list_todo_notification", "list_approval_request",
	"list_approval_reviewed"}

// ActivityTabCounts is 3x6A cR: the unread counts of the All, DMs,
// Mentions and Threads tabs, from activity_v2; ok is false before
// client.counts answered one.
func (a *App) ActivityTabCounts() (counts [4]int, ok bool) {
	a.mu.Lock()
	m := a.actBadges
	a.mu.Unlock()
	if m == nil {
		return counts, false
	}
	for k, n := range m {
		if k != "message_reaction" && a.activityBadgeTypes()[k] {
			counts[0] += n
		}
	}
	counts[1] = m["dm"]
	for _, k := range activityMentionTypes {
		counts[2] += m[k]
	}
	counts[3] = m["thread_v2"]
	return counts, true
}
