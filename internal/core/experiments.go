package core

import (
	"encoding/json"
	"sort"

	"github.com/some-du6e/clack/internal/slack"
)

// Slack's experiments, as its web client keeps them (build 4d0c78085232,
// 2026-11-10; docs/EXPERIMENTS.md). The `experiments` store (client-boot
// RIWm) holds experiments.getByUser's assignments, whole; every check
// reads one group through getGroupForUser (HWuB `_Z`, or `ze` without
// logging the exposure), which answers null for an experiment the viewer
// isn't assigned to. Slack's own overrides (overrideExperimentAssignments,
// RIWm `gf`, dispatched by its dev-only variants menu and ?force_<name>)
// replace a group over what the server sent.
//
// Clack keeps the same three things: the server's assignments
// (expServer: the call's answer, or before it the last answer the disk
// cache kept, as Slack persists its store), the forced groups (expForced:
// SetExperimentGroup, which the Experiments plugins drive through Taut's
// experiments slice), and the groups every check reads (expGroups: the
// server's, or with neither an answer nor a cache the test account's
// defaults, under the forced ones). The server's groups always win over
// the defaults: an answer replaces them whole, so an experiment the
// account isn't in reads as not assigned even where a default had it. A change emits Event{Type: "experiments"}, so the sidebar
// is rebuilt and every check reads the new group on its next frame.

// setExperimentAssignments installs experiments.getByUser's answer, whole
// (Slack's bulkAddExperiments replaces the store), and keeps its groups
// for the disk cache.
func (a *App) setExperimentAssignments(as map[string]slack.ExperimentAssignment) {
	groups := make(map[string]string, len(as))
	for k, v := range as {
		groups[k] = v.Group
	}
	a.S.SetExperimentGroups(groups)
	a.mu.Lock()
	a.expServer = as
	if a.expServer == nil {
		a.expServer = map[string]slack.ExperimentAssignment{}
	}
	a.expLive = true
	a.recomputeExperimentsLocked()
	a.mu.Unlock()
	a.emit(Event{Type: "experiments"})
	a.refreshTempMutes() // temp_mute decides what a temporary mute does
}

// installCachedExperiments reads the groups the last experiments.getByUser
// answer left in the disk cache, unless the call has answered already.
// Slack's experiments store is persisted (client-boot 5fie,
// persistenceEnabled) and replaced whole when the call answers, so until
// then every check reads the account's last known groups; the test
// account's defaults (defaultExperimentGroups) are only for a first start,
// or a start whose call fails, with no cache.
func (a *App) installCachedExperiments() {
	g := a.S.CachedExperimentGroups()
	if g == nil {
		return
	}
	as := make(map[string]slack.ExperimentAssignment, len(g))
	for k, v := range g {
		as[k] = slack.ExperimentAssignment{Group: v}
	}
	a.mu.Lock()
	if a.expLive || a.expServer != nil {
		a.mu.Unlock()
		return
	}
	a.expServer = as
	a.recomputeExperimentsLocked()
	a.mu.Unlock()
	a.emit(Event{Type: "experiments"})
	a.refreshTempMutes()
}

// setExperiments installs server groups without their records (tests).
func (a *App) setExperiments(groups map[string]string) {
	as := make(map[string]slack.ExperimentAssignment, len(groups))
	for k, g := range groups {
		as[k] = slack.ExperimentAssignment{Group: g}
	}
	a.setExperimentAssignments(as)
}

// recomputeExperimentsLocked rebuilds expGroups and the sidebar's
// experiments from the server's groups (or the defaults) and the forced
// ones. a.mu is held.
func (a *App) recomputeExperimentsLocked() {
	if a.expServer == nil && len(a.expForced) == 0 {
		a.expGroups, a.exp = nil, nil // defaults (sidebarExperiments, experiment)
		return
	}
	g := make(map[string]string, len(a.expServer)+len(defaultExperimentGroups)+len(a.expForced))
	if a.expServer == nil {
		for k, v := range defaultExperimentGroups {
			g[k] = v
		}
	} else {
		for k, v := range a.expServer {
			g[k] = v.Group
		}
	}
	for k, v := range a.expForced {
		if v == "" {
			delete(g, k) // forced to "not assigned"
		} else {
			g[k] = v
		}
	}
	r := g["dm_recency_sort_rollout"]
	a.exp = &sidebarExp{
		filterDormant: g["filter_dormant_channels"] == "on",
		improveMute:   g["improve_mute_pref"] == "on",
		dmRecency:     r == "without_edu" || r == "with_edu",
		dmHistory:     g["programmable_sidebar_dm_history"] == "on",
	}
	a.expGroups = g
}

// experiment returns the viewer's group for an experiment, "" when not
// assigned (Slack's null). With neither an experiments.getByUser answer
// nor the disk cache's copy of the last one, it is the test account's
// group (defaultExperimentGroups, TestDefaultExperimentsMatchAccount).
func (a *App) experiment(name string) string {
	a.mu.Lock()
	g := a.expGroups
	a.mu.Unlock()
	if g == nil {
		return defaultExperimentGroups[name]
	}
	return g[name]
}

// Experiment returns the viewer's group for a Slack experiment
// (experiments.getByUser, under any forced group), "" when not assigned.
func (a *App) Experiment(name string) string { return a.experiment(name) }

// SetExperimentGroup forces one experiment's group over what
// experiments.getByUser answered, as Slack's overrideExperimentAssignments
// does; "" makes it read as not assigned. Every check sees it at once.
func (a *App) SetExperimentGroup(name, group string) {
	a.SetExperimentGroups(map[string]*string{name: &group})
}

// ClearExperimentGroup drops a forced group: the experiment reads as
// experiments.getByUser had it again.
func (a *App) ClearExperimentGroup(name string) {
	a.SetExperimentGroups(map[string]*string{name: nil})
}

// SetExperimentGroups forces (non-nil) or clears (nil) several groups at
// once, with one "experiments" event when anything changed. The Taut
// host calls it with what the plugins' patches of the experiments slice
// changed.
func (a *App) SetExperimentGroups(groups map[string]*string) {
	changed := false
	a.mu.Lock()
	for name, g := range groups {
		old, had := a.expForced[name]
		switch {
		case g == nil && had:
			delete(a.expForced, name)
			changed = true
		case g != nil && (!had || old != *g):
			if a.expForced == nil {
				a.expForced = map[string]string{}
			}
			a.expForced[name] = *g
			changed = true
		}
	}
	if changed {
		a.recomputeExperimentsLocked()
	}
	a.mu.Unlock()
	if changed {
		a.emit(Event{Type: "experiments"})
		a.recentsAfterExperiments()
		a.refreshTempMutes()
	}
}

// ForcedExperiments returns the forced groups ("" = not assigned).
func (a *App) ForcedExperiments() map[string]string {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make(map[string]string, len(a.expForced))
	for k, v := range a.expForced {
		out[k] = v
	}
	return out
}

// ExperimentAssignments returns experiments.getByUser's records, the way
// Slack's experiments store holds them (forced groups not applied), or
// nil before it answered. A record Clack has no raw copy of (setExperiments)
// is just its group.
func (a *App) ExperimentAssignments() map[string]json.RawMessage {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.expServer == nil {
		return nil
	}
	out := make(map[string]json.RawMessage, len(a.expServer))
	for k, v := range a.expServer {
		if len(v.Raw) > 0 {
			out[k] = v.Raw
			continue
		}
		b, _ := json.Marshal(map[string]string{"group": v.Group})
		out[k] = b
	}
	return out
}

// ExperimentRead is one experiment check Clack makes and the groups it
// compares the viewer's group with.
type ExperimentRead struct {
	Name   string
	Groups []string
}

// ExperimentReads lists Clack's experiment checks, sorted by name
// (TestExperimentReadsListed keeps it in step with the code). Taut's host
// shows them to plugins as a module's source, the way Slack's bundle shows
// its own checks, so the Experiments plugins find which groups Clack
// reacts to.
func ExperimentReads() []ExperimentRead {
	out := make([]ExperimentRead, 0, len(experimentReads))
	for name, groups := range experimentReads {
		out = append(out, ExperimentRead{Name: name, Groups: groups})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

var on = []string{"on"}

// experimentReads: every experiment Clack's code reads, with the groups it
// compares against ("on" unless noted).
var experimentReads = map[string][]string{
	// sidebar (sidebar_rules.go, section menus, prefs)
	"filter_dormant_channels": on, "improve_mute_pref": on, "dm_recency_sort_rollout": {"without_edu", "with_edu"},
	"programmable_sidebar_dm_history": on, "vip_section_pref": on, "agents_in_dm_sidebar": on, "agents_in_dm_sidebar_pref": on,
	"sidebar_section_search": on, "add_channels_channel_sections_slackathon": on, "mute_channel_edu": on,
	"sidebar_text_filter": on, "sidebar_text_filter_m2": on, "sidebar_text_filter_m3": on, "temp_mute": on, "manage_conversations_strings": on,
	"thread_root_selected_state": on, "suggested_sections_auto_sort_v2": on, "suggested_sections_auto_sort_killswitch": on, "meeting_assistant": on,
	"mpdm_naming": on, "sc_mpdm_naming": on, "mpdm_notifications_advanced_option": on, "silent_sidebar": on,
	"channel_ia_header_refinements": on,
	// bookmark folders' add menu (views/folder_tab_more.go: THeG, sUmX)
	"unified_file_picker": on, "surfaces_skills_unipicker": on,
	// rail (rail.go)
	"unified_global_triage": on, "activity_in_home_prototype": on, "activity_inbox": on, "activity_with_sidebar": on,
	// the Activity badge's entry types (activity_badges.go)
	"code_review_requested_in_activity": on, "inviter_joiner_first_dm_v2": {"treatment"},
	"agentforce_deprecation": on, "agent_tasks": on, "later_todos": on, "agents_landing_page": on, "agentic_ia": on,
	// the Agents & tools peek's session menu (rail_peek_agents_menu.go: M/qt Kw, HLQ9 GL)
	"agent_ux_august2026_release_translations": on, "agent_channel_thread_naming": on,
	// activity (activity_agent.go: 5Ns/, M/qt ji; nested_secondary_view is
	// the secondary views' below)
	"activity_entry_layout_revisions_2": on, "activity_inbox_remove_persistent_detail_panel": on,
	"activity_bot_item_type_reads": on, "activity_automations_only": on, "exclude_automations_filter": on,
	"agent_status_in_activity": on,
	// views
	"activity_global_settings": on, "slackbot_unread_v2": on, "unified_files_3p_primary_custom_sections": on,
	"lists_tabbing_navigation": on, "miles_davis_themes": on,
	"availability_prefs_section": on, "status_improvements": on, "activity_bulk_unread_tool": on,
	"lists_grid_todos": on,
	// channel header, tabs and details (channel.go, views/details*.go)
	"ai_channel_exclusions": on, "mcp_ai_channel_exclusions": on, "enable_new_channel_settings": on,
	"move_channel_tabs_settings": on, "mpdm_default_exclude_history_clients_webapp": on,
	"channel_topic_purpose_setting_restrict_to_channel_managers": on, "channel_details_agents_apps_tab": on,
	"list_channel_tab_show_view_name_fe": on, "lists_channel_tabs_default_view": on,
	ExpChannelJoinWithAgents: on, ExpChannelHeaderWithAgents: on, ExpAgentsInChannelsImprovements: on, // the join card and header count (channel_agents.go)
	// secondary views, messages and workflows (merged from their branches)
	"nested_secondary_view": on, "secondary_view_default_width": on, "unread_thread_indication_in_replybar": on,
	"workflow_execution_window": on, "enable_wfb_ai_answer_step": on,
	// drafts (backend.go: the sidebar's draft pencil)
	"draft_management_in_composer": {"treatment"}, "sidebar_draft_indicator_menu_and_thread_inclusion": on,
	// messages and senders
	"show_as_agent": on, "agent_badge": on, "support_lots_of_agents": on,
	"system_messages_redesign_m1": on, "flat_message_heading_scale": on, "portable_content_composer": {"treatment"},
	"message_actions_truncate": on, "agent_identity_overrides": on,
	// composer and autocomplete
	"composer_smart_symbols": on, "slash_command_failed_dedupe": on, "autocomplete_frequently_used_emojis_frecency": on,
	"rich_text_headers_and_dividers": on, "updated_scheduled_sends_desktop": on, "giphy_fixed_width": on,
	"autocomplete_demote_flannel_passthrough_rltv_exact_match": {"on", "treatment"},
	"ml_fuzzy_match_beyond_word_boundaries_v2":                 {"on", "treatment"},
	"emoji_exact_match_boost":                                  on,
	"autocomplete_affinity_ml_model":                           on,
	"semantic_emoji_matching":                                  on,
	"invite_slash_command_in_threads":                          on,
	"ia4_slash_command_fe":                                     on,
	"unarchive_channel_banner_polish":                          on,
	// code channels
	"session_channels": on, "session_channels_code_tab_pr": on, "session_channels_gh_wo": on,
	"session_channels_inline_diff": on, "code_channels_naming": on, "session_channel_views": on,
	"code_channels_auto_close": on, "session_channels_slug_peek": on, // channel_card.go ChannelTabSlugCard
	// agents (agent.go; plan and task blocks: ui/blockkit/agent.go)
	"agent_member_channel_count": on, "plan_block_coding_enhancements": on, "agent_streaming_slimmer_task_cards": on,
	"app_thread_nugget_fe": on, "app_nugget_refinement": on,
	// huddles (ui/huddleui/experiments.go)
	"scheduled_huddles": on, "huddles_raised_hand_queue": on,
	"huddle_join_from_preview": on, "huddles_preview_v2": on, "huddles_alone_timeout": on, "huddles_alone_timeout_shortened": on,
	"huddles_av_button_permission_handling": on, "huddles_awareness_timestamp": on, "huddles_miles_davis": on,
	"huddles_missed_notification": on, "huddles_multi_device": {"treatment"}, "huddles_screenshare_popout": on,
	"huddles_switch_microphone_helper": on, "huddles_noise_suppression_awareness": on, "huddles_screenshare_share_audio": on,
	"huddles_new_grids":    on,
	"huddles_status_ultra": on, // the rail's status emoji drops the huddle's headphones (dnd.go SelfStatusEmoji)
	// search and the switcher (search*.go, popups/search_*)
	"desktop_search_recents_read": on, "desktop_search_recents_write": on, "omni_quick_message_results": on,
	"search_autocomplete_promote_exact_name_matches": {"treatment_3", "treatment_5"},
	"search_explore_freshness_signals":               on,
	// the search box's completers (popups/search_filter_*): the date
	// modifiers go to the date picker; type: lists Surfaces
	"omni_date_quick_filters": on, "surfaces_synth_view": on,
	// prefs, status, focus, notifications, emoji picker
	"emoji_skin_tone_picker_improvement": on, "focus_mode_custom_duration": {"treatment"},
	"defer_notification_3p_streaming": on,
	// desktop notifications (ui/notify.go, huddleui/invite.go): Slack's
	// notification ids and groups, translated buttons, an invite's Decline
	"desktop_notification_actions": on,
	// files
	"file_trash_updates": on,
	// Edit profile's photo panes (ui/popups/profile_edit.go epPhotoComp):
	// any assigned group picks "Add a profile photo"
	"profile_photo_comp": {"on", "off", "control"},
	// menus, selects and buttons (ui/popups/experiments.go): the v2 Button,
	// and two session toggles (enabled for treatment or on)
	"slack_kit_v2_button":                        on,
	"a11y_no_auto_highlight_search_multi_select": {"on", "treatment"},
	"basic_select_placeholder_option":            {"on", "treatment"},
	"custom_file_sections":                       on, // the Files sidebar's starred file collection (views/files.go)
	// message actions (ui/popups actions_forward.go, actions_flag.go): the
	// share dialog's schedule caret and Save Draft; the flag row's custom
	// group DM names and its cross-org rules
	"schedule_send_forwards": on, "forwarding_save_as_draft": on,
	"flag_custom_mpdm_names": on, "flag_xorg_refinement": {"treatment"}, "flag_xorg_external_team_check": on,
}
