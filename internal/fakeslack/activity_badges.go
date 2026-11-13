package fakeslack

// The Activity inbox's unread badge counts: client.counts answers
// activity_v2 by entry type once a test sets them, every type listed with
// 0 for none, as a real account's answer has them (2026-11-12, counts
// only). activity.markRead takes a single unread item's one off its type,
// as the server does before it sends badge_counts_updated.

// ActivityV2Types are the activity_v2 keys of a real client.counts answer,
// in its order.
var ActivityV2Types = []string{"at_user", "at_user_group", "at_channel", "at_everyone", "channel", "dm", "keyword",
	"message_reaction", "internal_channel_invite", "external_channel_invite", "external_dm_invite",
	"shared_workspace_invite", "team_joiner_invite", "unjoined_channel_mention", "list_record_assigned",
	"list_record_edited", "list_user_mentioned", "list_todo_notification", "list_approval_request",
	"list_approval_reviewed", "bot_dm_bundle", "thread_v2", "saved_reminder", "prejoin_dm_welcome_party_alert",
	"quietly_added_to_channel", "code_review_requested"}

// SetActivityBadges sets client.counts' activity_v2: the types in m with
// their counts, every other type 0. nil drops activity_v2 from the answer.
func (s *Server) SetActivityBadges(m map[string]int) {
	s.actBadgeMu.Lock()
	defer s.actBadgeMu.Unlock()
	if m == nil {
		s.actBadges = nil
		return
	}
	s.actBadges = make(map[string]int, len(ActivityV2Types))
	for _, k := range ActivityV2Types {
		s.actBadges[k] = 0
	}
	for k, v := range m {
		s.actBadges[k] = v
	}
}

// ActivityBadges is a copy of activity_v2 as client.counts answers it.
func (s *Server) ActivityBadges() map[string]int { return s.activityV2() }

func (s *Server) activityV2() map[string]int {
	s.actBadgeMu.Lock()
	defer s.actBadgeMu.Unlock()
	if s.actBadges == nil {
		return nil
	}
	cp := make(map[string]int, len(s.actBadges))
	for k, v := range s.actBadges {
		cp[k] = v
	}
	return cp
}

// readActivityBadge is activity.markRead {type, key} on the counts.
func (s *Server) readActivityBadge(typ string) {
	s.actBadgeMu.Lock()
	defer s.actBadgeMu.Unlock()
	if n := s.actBadges[typ]; n > 0 {
		s.actBadges[typ] = n - 1
	}
}
