package fakeslack

// sidebarExperiments is experiments.getByUser's answer: the sidebar
// experiments as a real account has them (2026-11-08), and the groups
// later branches added for what they follow. Every group is the test
// account's (core's TestFakeslackExperimentsMatchAccount checks them
// against its recorded assignments); some experiments the account has
// are left out on purpose, so other tests' calls don't change.
var sidebarExperiments = map[string]string{
	"filter_dormant_channels": "on", "improve_mute_pref": "on", "dm_recency_sort_rollout": "without_edu",
	"reskin_custom_responses": "on", "temp_join_channel": "on",
	"sidebar_section_search": "on", "add_channels_channel_sections_slackathon": "on", "mute_channel_edu": "on",
	"session_channels": "on", "session_channel_views": "on", "code_channels_naming": "on",
	"session_channels_slug_peek": "on", // a slug to a tab with a peek shows the peek, not the channel card
	"code_channels_auto_close":   "on", "agent_member_channel_count": "on", "plan_block_coding_enhancements": "on",
	"agent_streaming_slimmer_task_cards": "on", "composer_smart_symbols": "on",
	"app_thread_nugget_fe": "on", "app_nugget_refinement": "on",
	"agents_in_dm_sidebar": "on", "agents_in_dm_sidebar_pref": "on",
	"suggested_sections_auto_sort_v2": "on",
	"lists_tabbing_navigation":        "on",
	// channel details' Agents & apps / Automations tabs and Settings'
	// tab permissions (views/details.go, details_more.go)
	"channel_details_agents_apps_tab": "on", "move_channel_tabs_settings": "on",
	// focus mode and the Availability section (see core's defaultExperimentGroups)
	"silent_sidebar": "on", "availability_prefs_section": "on",
	// search and the switcher (2026-11-10). desktop_search_recents_read
	// and _write (on for the account) stay unassigned here so other
	// tests' calls don't change; the recents tests force them.
	"omni_quick_message_results": "on", "search_autocomplete_promote_exact_name_matches": "treatment_5",
	"fe_nav_first_apply_post_processing_rules": "treatment", "omni_date_quick_filters": "on",
	"srp_card_explore": "on", "search_file_channel_messages": "on",
	"work_objects_message_search_xp_v2": "on",
	// work object cards (core/workobjects.go), as the account had them on 2026-11-10
	"work_objects_rearrangement": "on", "work_objects_refresh_timestamp": "on", "wo_comment_sync_client_xp": "on",
	"work_objects_embedded_preview": "on", "third_party_integrations_video_previews": "on",
	// message rows (see core's defaultExperimentGroups)
	"system_messages_redesign_m1": "on", "flat_message_heading_scale": "on", "portable_content_composer": "treatment",
	"message_actions_truncate": "on", "unread_thread_indication_in_replybar": "on", "agent_identity_overrides": "on",
	"forward_message_card": "on", "giphy_fixed_width": "on", "truncated_url_hover_card": "on",
	// the sidebar text filter and temporary mutes (core sidebar_filter.go, temp_mute.go)
	"sidebar_text_filter": "on", "sidebar_text_filter_m2": "on", "sidebar_text_filter_m3": "on",
	"temp_mute": "on", "manage_conversations_strings": "on", "thread_root_selected_state": "on",
	// the sidebar's organize mode gets Leave (ui/sidebar_organize.go)
	"bulk_leave_channels_from_sidebar": "on",
	// the thread pane (core's defaultExperimentGroups, live 2026-11-10)
	"nested_secondary_view": "on", "secondary_view_default_width": "on", "smaller_secondary_headers": "on",
	// views and navigation (core/views_experiments.go), as the account had them on 2026-11-10
	"desktop_history_menu": "on", "desktop_history_recents_read": "on", "keyboard_shortcuts_search": "on",
	"view_header_overflow_borders": "on", "lists_in_later": "on",
}

// AccountExperimentGroups returns the groups fakeslack's
// experiments.getByUser answers with (name -> group).
func AccountExperimentGroups() map[string]string {
	out := make(map[string]string, len(sidebarExperiments)+len(accountExperiments))
	for k, v := range sidebarExperiments {
		out[k] = v
	}
	for k, v := range accountExperiments {
		if _, has := out[k]; !has {
			out[k] = v
		}
	}
	return out
}

// accountExperiments are more of the test account's groups
// (experiments.getByUser, 2026-11-10, docs/EXPERIMENTS.md): the canvas
// and list experiments Clack follows. The fake's answer gets them on top
// of its sidebar set, so tests and shots run as the account does; tests
// flip one with core's SetExperimentGroup.
var accountExperiments = map[string]string{
	"canvas_table_of_contents":    "on",
	"canvas_blockquote_layout":    "on",
	"canvas_comment_only_mode":    "on",
	"slackified_checkbox":         "on",
	"lists_file_backed_templates": "on",
	"lists_export_enrichment":     "on",
	"lists_grid_todos":            "on",
}

// withAccountExperiments adds accountExperiments to an
// experiments.getByUser answer (keeping any group it already has).
func withAccountExperiments(out any) any {
	m, ok := out.(map[string]any)
	if !ok {
		return out
	}
	as, ok := m["assignments"].(map[string]any)
	if !ok {
		return out
	}
	for name, group := range accountExperiments {
		if _, has := as[name]; !has {
			as[name] = map[string]any{"group": group}
		}
	}
	return out
}
