package core

// The huddle sidebar's experiment checks (ui/huddleui/sidebar_suffix.go
// and huddles_link.go, Slack's pSxm selectors), registered apart from
// experimentReads' literal like experiment_reads_more.go's.
func init() {
	for name, groups := range map[string][]string{
		"huddle_section_indicator":          on, // pSxm I6: SectionHuddleIndicator on a collapsed section
		"increase_huddles_max_participants": on, // pSxm nC: getParticipantLimit 250, else 50
		"huddles_link_v2":                   on, // pSxm $A: the Huddles link's unread-invite badge (huddles_link.go)
	} {
		experimentReads[name] = groups
	}
}
