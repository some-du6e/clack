package core

// Reads that reached ui without an experimentReads entry (activity,
// channel details, drafts, lists, composer, threads, workflows and
// secondary view branches). They are added here
// only where the literal has none, so a branch that lists one in
// experimentReads later merges without a duplicate key.
func init() {
	for name, groups := range map[string][]string{
		"activity_bulk_unread_tool":                         on,
		"ai_channel_exclusions":                             on,
		"draft_management_in_composer":                      {"treatment"},
		"enable_new_channel_settings":                       on,
		"lists_grid_todos":                                  on,
		"mcp_ai_channel_exclusions":                         on,
		"move_channel_tabs_settings":                        on,
		"mpdm_default_exclude_history_clients_webapp":       on,
		"rich_text_headers_and_dividers":                    on,
		"sidebar_draft_indicator_menu_and_thread_inclusion": on,
		"updated_scheduled_sends_desktop":                   on,
		"channel_details_agents_apps_tab":                   on,
		"enable_wfb_ai_answer_step":                         on,
		"nested_secondary_view":                             on,
		"secondary_view_default_width":                      on,
		"unread_thread_indication_in_replybar":              on,
		"workflow_execution_window":                         on,
	} {
		if _, ok := experimentReads[name]; !ok {
			experimentReads[name] = groups
		}
	}
}
