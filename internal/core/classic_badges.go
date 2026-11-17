package core

// The badges without the activity inbox (Slack's "classic" Activity), as
// Slack's client keeps them (bundles cached 2026-11-15: client-boot
// bbd3a8c1f7bb6f10, boot-render ffcc7d4c4df423eb, boot-shared
// 54b29d3cf4bf04bc):
//
//   - client.counts' channel_badges (channels, dms, app_dms,
//     thread_mentions, thread_unreads: the server's highlight totals) go
//     into unreadCounts.channelBadges, merged over what was there (client-
//     boot h1EQ applyCountsToChannelsAndThreads → s7a6
//     updateChannelBadgesCounts), and its alerts (unread counts by user
//     alert type) replace userAlerts (3vGZ resetAllUserAlertsUnreadCounts,
//     only the types i2NO/rL0q lists). Without the inbox,
//     badge_counts_updated brings new ones too: its counts merged the same
//     way, its alerts one type at a time (boot-render VxbD
//     handleBadgeCountsUpdatedRTMEvent; under the inbox the event's
//     activity_v2 is all it reads).
//   - The Activity tab's count is 3x6A getActivityBadgeCount:
//     thread_mentions + app_dms + channels, thread_unreads when threads
//     count (below), and the unread alerts of the types it lists (G/NM P).
//     InjD draws a dot instead while that is 0 and threads have unreads
//     they don't count.
//   - The app badge (title, Dock, taskbar) is 3x6A getAppBadgeCount: ZVw+
//     getTotalUnreadHighlights, the unread DMs' and group DMs' highlights
//     except bot DMs (DUy0 G), plus channels + thread_mentions + app_dms
//     (and thread_unreads when threads count) from channel_badges: s7a6
//     calculateHighlightTotals' totalUnreadHighlightsWithoutDms(AndThread
//     Unreads). So an app's DM counts there only through the server's
//     app_dms, and the Slack system user's or Slackbot's not at all.
//
// Threads count (thread_unreads in both) under O2By Dn
// isThreadBadgingHomeSidebarEnabled (thread_badging_home_sidebar on,
// dual badging off) or the pref activity_include_threads_in_badge_count.
//
// Clack has everything both counts read. What it doesn't keep: E0VC g_,
// quip's sidebar badge count, which getAppBadgeCount adds (no quip
// sidebar here).

import "encoding/json"

// Slack's user alert types (i2NO/rL0q e), the keys of client.counts'
// alerts and badge_counts_updated's.
var userAlertTypes = []string{"unjoined_channel_mention", "external_dm_invite", "external_channel_invite",
	"shared_workspace_invite", "sales_home_notification", "lists_user_mentioned", "lists_record_assigned",
	"lists_record_edited", "lists_todo_notification", "lists_approval_request", "lists_approval_reviewed",
	"huddles_missed_huddle", "saved_reminder", "team_joiner_invite", "calendar_upcoming_event",
	"generic_system_alert", "prejoin_dm_welcome_party_alert", "quietly_added_to_channel", "code_review_requested"}

// classicBadges is unreadCounts.channelBadges and userAlerts.
type classicBadges struct {
	channels map[string]int // channel_badges, merged
	alerts   map[string]int // unread count by alert type
}

// mergeChannelBadges is updateChannelBadgesCounts: m over the badges
// held. It reports whether one moved.
func (a *App) mergeChannelBadges(m map[string]int) bool {
	if len(m) == 0 {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	moved := false
	cp := make(map[string]int, len(a.classic.channels)+len(m))
	for k, v := range a.classic.channels {
		cp[k] = v
	}
	for k, v := range m {
		if old, ok := cp[k]; !ok || old != v {
			moved = true
		}
		cp[k] = v
	}
	a.classic.channels = cp
	return moved
}

// resetAlerts is resetAllUserAlertsUnreadCounts: the known types m has,
// nothing else (h1EQ passes `alerts || {}`, so an answer without them
// clears them).
func (a *App) resetAlerts(m map[string]int) {
	cp := map[string]int{}
	for _, t := range userAlertTypes {
		if v, ok := m[t]; ok {
			cp[t] = v
		}
	}
	a.mu.Lock()
	a.classic.alerts = cp
	a.mu.Unlock()
}

// setAlerts is updateUserAlertsUnreadCount for each known type m has.
func (a *App) setAlerts(m map[string]int) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	moved := false
	cp := make(map[string]int, len(a.classic.alerts)+len(m))
	for k, v := range a.classic.alerts {
		cp[k] = v
	}
	for _, t := range userAlertTypes {
		if v, ok := m[t]; ok {
			if old, had := cp[t]; !had || old != v {
				moved = true
			}
			cp[t] = v
		}
	}
	a.classic.alerts = cp
	return moved
}

// onClassicBadges is handleBadgeCountsUpdatedRTMEvent without the inbox:
// the event's counts merged into channel_badges, its alerts set per type.
// It reports whether a badge moved.
func (a *App) onClassicBadges(raw json.RawMessage) bool {
	var e struct {
		Counts map[string]int `json:"counts"`
		Alerts map[string]int `json:"alerts"`
	}
	if json.Unmarshal(raw, &e) != nil {
		return false
	}
	moved := a.mergeChannelBadges(e.Counts)
	if e.Alerts != nil && a.setAlerts(e.Alerts) {
		moved = true
	}
	return moved
}

// threadsInBadges is whether thread_unreads count in both badges: O2By Dn
// or the pref (ZVw+ and getActivityBadgeCount's `n || !a`).
func (a *App) threadsInBadges() bool {
	dual := a.experiment("dual_badging_in_home_sidebar") == "on"
	switch a.experiment("sidebar_badging_variants_v1") { // O2By isSidebarBadgingVariant
	case "dot_hover_dual_icons", "dot_hover_total_count", "number_total_count":
		dual = true
	}
	threadBadging := !dual && a.experiment("thread_badging_home_sidebar") == "on"
	return threadBadging || a.PrefBool("activity_include_threads_in_badge_count", false)
}

// classicAlertTypes are the alert types getActivityBadgeCount sums: the
// to-do alerts with lists_monster_step (Hm0j Dq isListsNewTodoWorkflows
// Enabled), the approvals' with approvals_in_todos (Hm0j ci).
func (a *App) classicAlertTypes() []string {
	types := []string{"external_channel_invite", "external_dm_invite", "shared_workspace_invite", "lists_user_mentioned",
		"lists_record_assigned", "lists_record_edited", "unjoined_channel_mention"}
	if a.experiment("lists_monster_step") == "on" {
		types = append(types, "lists_todo_notification")
	}
	if a.experiment("approvals_in_todos") == "on" {
		types = append(types, "lists_approval_request", "lists_approval_reviewed")
	}
	return append(types, "prejoin_dm_welcome_party_alert")
}

// classicActivityBadge is getActivityBadgeCount without the inbox, and
// whether InjD draws the Activity tab's dot instead (count 0, threads
// unread but not counted).
func (a *App) classicActivityBadge() (count int, dot bool) {
	threads := a.threadsInBadges()
	types := a.classicAlertTypes()
	a.mu.Lock()
	b, al := a.classic.channels, a.classic.alerts
	a.mu.Unlock()
	count = b["thread_mentions"] + b["app_dms"] + b["channels"]
	if threads {
		count += b["thread_unreads"]
	}
	for _, t := range types {
		count += al[t]
	}
	return count, count == 0 && !threads && b["thread_unreads"] > 0
}

// ActivityDot reports InjD's dot on the Activity tab: without the inbox,
// while getActivityBadgeCount is 0 and threads have unreads the badges
// don't count.
func (a *App) ActivityDot() bool {
	if a.ActivityInbox() {
		return false
	}
	_, dot := a.classicActivityBadge()
	return dot
}

// channelBadgeHighlights is calculateHighlightTotals' highlights without
// the DMs, the part of getTotalUnreadHighlights channel_badges gives:
// channels + thread_mentions + app_dms, and thread_unreads when threads
// count.
func (a *App) channelBadgeHighlights() int {
	threads := a.threadsInBadges()
	a.mu.Lock()
	b := a.classic.channels
	a.mu.Unlock()
	n := b["channels"] + b["thread_mentions"] + b["app_dms"]
	if threads {
		n += b["thread_unreads"]
	}
	return n
}

// ChannelBadgeHighlights is what channel_badges adds to the app badge (0
// under the inbox, whose app badge doesn't read them): the UI redraws the
// badge when it moves.
func (a *App) ChannelBadgeHighlights() int {
	if a.ActivityInbox() {
		return 0
	}
	return a.channelBadgeHighlights()
}

// The experiments the classic badges read (TestExperimentReadsListed).
func init() {
	experimentReads["dual_badging_in_home_sidebar"] = on
	experimentReads["sidebar_badging_variants_v1"] = []string{"dot_hover_dual_icons", "dot_hover_total_count", "number_total_count"}
	experimentReads["thread_badging_home_sidebar"] = on
	experimentReads["lists_monster_step"] = on
	experimentReads["approvals_in_todos"] = on
	// the account's group (2026-11-13) before experiments.getByUser
	// answers; it isn't in the others (TestDefaultExperimentsMatchAccount)
	defaultExperimentGroups["lists_monster_step"] = "on"
}
