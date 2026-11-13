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

// onActivityBadges takes badge_counts_updated's activity_v2. Under
// activity_inbox Slack applies it as it is and stops there (render 37DU
// handleBadgeCountsUpdatedRTMEvent: updateActivityV2BadgeCounts, no
// client.counts), so the rail's badge moves with the event; the UI hears
// "activity_badges".
func (a *App) onActivityBadges(raw json.RawMessage) {
	var e struct {
		ActivityV2 map[string]int `json:"activity_v2"`
	}
	if json.Unmarshal(raw, &e) == nil && e.ActivityV2 != nil {
		a.setActivityBadges(e.ActivityV2)
		a.emit(Event{Type: "activity_badges"})
	}
}

// decActivityBadges is P0K2 decrementBadgeCounts after
// markItemsReadAcrossLists: each unread item marked read takes one off
// its type's count (never below 0), before the server's
// badge_counts_updated confirms it.
func (a *App) decActivityBadges(items []ActivityItem) {
	a.mu.Lock()
	changed := false
	if a.actBadges != nil {
		cp := make(map[string]int, len(a.actBadges))
		for k, v := range a.actBadges {
			cp[k] = v
		}
		for _, it := range items {
			if !it.Unread || it.Type == "" {
				continue
			}
			if n := cp[it.Type]; n > 0 {
				cp[it.Type] = n - 1
				changed = true
			}
		}
		if changed {
			a.actBadges = cp
		}
	}
	a.mu.Unlock()
	if changed {
		a.emit(Event{Type: "activity_badges"})
	}
}

// ActivityBadgeCount is the rail's Activity badge under activity_inbox:
// 3x6A getActivityBadgeCount, which with the inbox on (5Ns/ Lh) is H, the
// activity_v2 counts of the badged entry types summed without
// message_reaction (the All tab's count). yEMG hands it to the rail's
// Activity tab (ActivityTabContent, InjD useGetMentions); 0 before
// client.counts answered (unreadActivityV2BadgeCounts starts {}).
func (a *App) ActivityBadgeCount() int {
	c, _ := a.ActivityTabCounts()
	return c[0]
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
	if a.experiment("code_review_requested_in_activity") == "on" {
		t["code_review_requested"] = true // ysZt f
	}
	if a.experiment("inviter_joiner_first_dm_v2") == "treatment" {
		t["team_joiner_invite"] = true // vydz wy inviterJoinerFirstDmIsTreatment
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
	types := a.activityBadgeTypes()
	for k, n := range m {
		if k != "message_reaction" && types[k] {
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
