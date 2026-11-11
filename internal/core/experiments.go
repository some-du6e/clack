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
// (expServer), the forced groups (expForced: SetExperimentGroup, which
// the Experiments plugins drive through Taut's experiments slice), and
// the groups every check reads (expGroups: the server's, or the test
// account's defaults before experiments.getByUser answers, under the
// forced ones). A change emits Event{Type: "experiments"}, so the sidebar
// is rebuilt and every check reads the new group on its next frame.

// setExperimentAssignments installs experiments.getByUser's answer.
func (a *App) setExperimentAssignments(as map[string]slack.ExperimentAssignment) {
	a.mu.Lock()
	a.expServer = as
	if a.expServer == nil {
		a.expServer = map[string]slack.ExperimentAssignment{}
	}
	a.recomputeExperimentsLocked()
	a.mu.Unlock()
	a.emit(Event{Type: "experiments"})
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
// assigned (Slack's null); before (or without) experiments.getByUser it
// falls back to what the test account had on 2026-11-08
// (defaultExperimentGroups).
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
	"suggested_sections_auto_sort_v2": on, "suggested_sections_auto_sort_killswitch": on, "meeting_assistant": on,
	"mpdm_naming": on, "sc_mpdm_naming": on, "mpdm_notifications_advanced_option": on, "silent_sidebar": on,
	"channel_ia_header_refinements": on,
	// rail (rail.go)
	"unified_global_triage": on, "activity_in_home_prototype": on, "activity_inbox": on, "activity_with_sidebar": on,
	"agentforce_deprecation": on, "agent_tasks": on, "later_todos": on, "agents_landing_page": on, "agentic_ia": on,
	// views
	"activity_global_settings": on, "slackbot_unread_v2": on, "unified_files_3p_primary_custom_sections": on,
	"lists_tabbing_navigation": on, "miles_davis_themes": on, "temp_join_channel": on,
	"availability_prefs_section": on, "status_improvements": on, "activity_bulk_unread_tool": on,
	"lists_grid_todos": on,
	// channel header, tabs and details (channel.go, views/details*.go)
	"ai_channel_exclusions": on, "mcp_ai_channel_exclusions": on, "enable_new_channel_settings": on,
	"move_channel_tabs_settings": on, "mpdm_default_exclude_history_clients_webapp": on,
	"channel_topic_purpose_setting_restrict_to_channel_managers": on, "channel_details_agents_apps_tab": on,
	"list_channel_tab_show_view_name_fe": on, "lists_channel_tabs_default_view": on,
	// secondary views, messages and workflows (merged from their branches)
	"nested_secondary_view": on, "secondary_view_default_width": on, "unread_thread_indication_in_replybar": on,
	"workflow_execution_window": on, "enable_wfb_ai_answer_step": on,
	// drafts (backend.go: the sidebar's draft pencil)
	"draft_management_in_composer": {"treatment"}, "sidebar_draft_indicator_menu_and_thread_inclusion": on,
	// messages and senders
	"reskin_custom_responses": on, "show_as_agent": on, "agent_badge": on, "support_lots_of_agents": on,
	// composer and autocomplete
	"composer_smart_symbols": on, "slash_command_failed_dedupe": on, "autocomplete_frequently_used_emojis_frecency": on,
	"rich_text_headers_and_dividers": on, "updated_scheduled_sends_desktop": on,
	"autocomplete_flannel_fuzzy_match_passthrough":             {"treatment"},
	"autocomplete_demote_flannel_passthrough_rltv_exact_match": {"on", "treatment"},
	"ml_fuzzy_match_beyond_word_boundaries_v2":                 {"on", "treatment"},
	"emoji_exact_match_boost":                                  on,
	// code channels
	"session_channels": on, "session_channels_code_tab_pr": on, "session_channels_gh_wo": on,
	"session_channels_inline_diff": on, "code_channels_naming": on, "session_channel_views": on,
	"code_channels_auto_close": on,
	// agents (agent.go; plan and task blocks: ui/blockkit/agent.go)
	"agent_member_channel_count": on, "plan_block_coding_enhancements": on, "agent_streaming_slimmer_task_cards": on,
	"app_thread_nugget_fe": on, "app_nugget_refinement": on,
	// huddles
	"scheduled_huddles": on, "huddles_raised_hand_queue": on,
	// search and the switcher (search*.go, popups/search_*)
	"desktop_search_recents_read": on, "desktop_search_recents_write": on, "omni_quick_message_results": on,
	"search_autocomplete_promote_exact_name_matches": {"treatment_3", "treatment_5"},
	"search_explore_freshness_signals":               on,
}
