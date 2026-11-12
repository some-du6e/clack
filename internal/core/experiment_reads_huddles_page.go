package core

// The Huddles page's experiment checks (ui/views/huddles*.go, Slack's
// Igs2 and ZvF8 through pSxm's selectors), registered apart from
// experimentReads' literal like experiment_reads_more.go's.
func init() {
	for name, groups := range map[string][]string{
		"huddle_page_enhancements":      on, // pSxm cc: day groups, the pill filters, artifact buttons, HUDDLE_HISTORY_ROW dates
		"hide_suggested_huddles":        on, // pSxm kg: no SuggestedHuddles cards, the active section instead of the DM suggestions
		"huddles_history_files_section": on, // pSxm jh: a row's "N files" link
	} {
		experimentReads[name] = groups
	}
}
