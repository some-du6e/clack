package core

// The huddle sidebar's experiment checks (ui/huddleui/sidebar_suffix.go,
// Slack's pSxm selectors), registered apart from experimentReads' literal
// like experiment_reads_more.go's.
func init() {
	for name, groups := range map[string][]string{
		"huddle_section_indicator":          on, // pSxm I6: SectionHuddleIndicator on a collapsed section
		"increase_huddles_max_participants": on, // pSxm nC: getParticipantLimit 250, else 50
	} {
		experimentReads[name] = groups
	}
}
