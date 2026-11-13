package core

import (
	"context"
	"regexp"
	"slices"
)

// Channels with agents and apps in them, from Slack's web client (CDN
// build 274f7c7f1b69, 2026-11-11):
//
//   - channel_join_experience_with_agents (client-boot M/qt v_): the
//     preview's JoinChannelPane (boot-shared UY14) is the revamped card:
//     a facepile of the channel's people and its active agents, "Join
//     #name" (18px black), the purpose, "… are here." / "… are also here."
//     captions, and only the Join split button (no Details). Its data hook
//     reads, for a public channel only: membershipCounts' member_count
//     and app_count, fetchChannelPreviewAgentsApps (edge users/list,
//     filter "apps", count 20), the last 3 relevant members
//     (cYPn getLastNRelevantMembersInChannel) and the agents and apps
//     seen active in the last 28 messages (+z2S
//     getMembersSeenActiveInChannel).
//   - channel_header_with_agents (M/qt wX): the channel header's actions
//     (boot-shared g2K5) gain "agent_count" after the avatar stack, for a
//     channel (not a DM, group DM or app), not archived, whose agents and
//     apps count (RW6b Li) isn't 0, unless channel_resources_remove_tabs
//     shows the channel's resources instead. It is view-header Ms5K
//     AgentCountButton: the ai-agents glyph and the count ("–" before the
//     counts load), "View all agents & apps of this channel" with
//     "Includes {agents}" (the first three, fetched on hover) as its
//     tooltip, and a click opens the details on the Agents & apps tab.
//
// Both are unassigned on the test account (2026-11-11), so they read null
// there and stay off, the way Slack's "on"===g checks do.

// ExpChannelJoinWithAgents and ExpChannelHeaderWithAgents are the
// experiments' names (TestExperimentReadsListed).
const (
	ExpChannelJoinWithAgents   = "channel_join_experience_with_agents"
	ExpChannelHeaderWithAgents = "channel_header_with_agents"
)

// ChannelJoinWithAgents is M/qt v_: the revamped join card.
func (a *App) ChannelJoinWithAgents() bool { return a.experiment(ExpChannelJoinWithAgents) == "on" }

// ChannelHeaderWithAgents is M/qt wX: the header's agent count.
func (a *App) ChannelHeaderWithAgents() bool {
	return a.experiment(ExpChannelHeaderWithAgents) == "on"
}

// ExpAgentsInChannelsImprovements is M/qt Io (on for the test account
// since 2026-11-13; off before): it decides whether AgentCountButton's
// labels go through I18n, drops Agents sections from the sidebar
// (client-boot Q; app.go sections), and reworks channel details
// (views/details_agents.go): the Members roster's people-only scope in
// +VDd (e5) with KcBz's filter minus "Agents & apps", Vmvo
// ReworkedAgentsAppsTab, and ANc2's channel invite modal scoped to agents
// and apps.
const ExpAgentsInChannelsImprovements = "agents_in_channels_improvements"

// AgentsInChannelsImprovements is M/qt Io.
func (a *App) AgentsInChannelsImprovements() bool {
	return a.experiment(ExpAgentsInChannelsImprovements) == "on"
}

// ViewerRestricted is QLH5 DM isCurrentUserRestricted: a guest.
func (a *App) ViewerRestricted() bool { return a.viewerRestricted() }

// AgentAppCount is RW6b Li, the channel's agents and apps: membershipCounts'
// app_count (edge users/counts `apps`), plus one for an active native
// channel expert and one for Slackbot in the channel (Clack keeps neither
// channel_agent_status nor is_slackbot_channel_enabled, so it adds
// nothing). ok is false until the counts have loaded (Slack's undefined:
// the header draws "–").
func (a *App) AgentAppCount(ch string) (n int, ok bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok = a.memberN[ch]; !ok {
		return 0, false
	}
	return a.memberA[ch], true
}

// PeopleCount is membershipCounts' member_count (edge users/counts
// `people`), ok false before it loads.
func (a *App) PeopleCount(ch string) (int, bool) { return a.MemberCountCached(ch) }

// ChannelAgentApps is fetchChannelPreviewAgentsApps (UY14) and
// AgentCountButton's fetcher (Ms5K): edge users/list with filter "apps"
// and count 20, partitioned like Wv58 partitionRosterMemberIds (Slackbot,
// agents and bot users are the agents and apps). The ids keep the API's
// order; the users go into the store for names and avatars.
func (a *App) ChannelAgentApps(ctx context.Context, ch string) ([]string, error) {
	us, _, err := a.C.ChannelApps(ctx, ch, "", 20)
	if err != nil {
		return nil, err
	}
	a.S.PutUsers(us)
	ids := make([]string, 0, len(us))
	for i := range us {
		u := a.userOf(&us[i])
		if u.ID == SlackbotUserID || u.IsBot || u.Agent || u.Agentforce || u.AppUser {
			ids = append(ids, u.ID)
		}
	}
	return ids, nil
}

// ChannelAgentsAppsPage is a page of the reworked Agents & apps roster
// (+VDd under e7: fetchAndUpsertMembersForChannel with filter "apps" and
// count 50, then the next marker). The users go into the store.
func (a *App) ChannelAgentsAppsPage(ctx context.Context, ch, marker string) ([]User, string, error) {
	us, next, err := a.C.ChannelApps(ctx, ch, marker, 50)
	if err != nil {
		return nil, "", err
	}
	a.S.PutUsers(us)
	out := make([]User, 0, len(us))
	for i := range us {
		out = append(out, a.userOf(&us[i]))
	}
	return out, next, nil
}

// SlackbotUserID is Slackbot's user id (s8g3 Q$).
const SlackbotUserID = "USLACKBOT"

// seenActiveWindow is QJKZ determineDefaultHistoryLimit: the last 28
// messages (3 in two Slack-internal channels).
const seenActiveWindow = 28

var mentionRE = regexp.MustCompile(`<@([UW][A-Z0-9]+)(?:\|[^>]*)?>`)

// MembersSeenActive is +z2S getMembersSeenActiveInChannel: which of ids
// posted, replied in a thread or were mentioned in ch's last 28 loaded
// messages, newest first. Messages of an automated subtype (KC8h, joins,
// topics…) and ephemeral ones don't count. loaded is B0mF
// channelHasAnyHistory: without history the card names no agents and
// draws none (UY14's `null!==c&&h`).
func (a *App) MembersSeenActive(ch string, ids []string) (active []string, loaded bool) {
	if a.S == nil {
		return nil, false
	}
	tl := a.S.Timeline(ch)
	if tl == nil || len(tl.Msgs) == 0 {
		return nil, false
	}
	if len(ids) == 0 {
		return nil, true
	}
	msgs := tl.Msgs
	if len(msgs) > seenActiveWindow {
		msgs = msgs[len(msgs)-seenActiveWindow:]
	}
	var out []string
	for i := len(msgs) - 1; i >= 0 && len(out) < len(ids); i-- {
		m := &msgs[i]
		if m.IsEphemeral || automatedSubtypes[m.Subtype] {
			continue
		}
		credited := append(append([]string{}, m.ReplyUsers...), m.User)
		for _, mm := range mentionRE.FindAllStringSubmatch(m.Text, -1) {
			credited = append(credited, mm[1])
		}
		for _, id := range credited {
			if id != "" && slices.Contains(ids, id) && !slices.Contains(out, id) {
				out = append(out, id)
			}
		}
	}
	return out, true
}

// automatedSubtypes is KC8h isAutomatedSubtype's list.
var automatedSubtypes = func() map[string]bool {
	m := map[string]bool{}
	for _, s := range []string{"agent_view_added", "code_channel_review_submitted", "code_channel_lifecycle", "channel_join",
		"channel_leave", "channel_topic", "channel_purpose", "channel_archive", "channel_unarchive", "channel_convert_to_private",
		"channel_convert_to_public", "channel_convert_to_linked_private", "channel_convert_to_linked_private_read",
		"channel_convert_to_linked_private_edit", "channel_convert_to_linked_private_transfer", "channel_convert_shared",
		"group_join", "group_leave", "group_topic", "group_purpose", "group_archive", "group_unarchive", "group_name",
		"channel_name", "play_sound", "pinned_item", "unpinned_item", "sh_room_shared", "sh_room_created", "bot_enable",
		"bot_disable", "bot_add", "bot_remove", "bot_updated", "reminder_add", "reminder_delete", "reply_broadcast",
		"app_conversation_join", "app_conversation_leave", "app_conversation_created", "channel_posting_permissions",
		"retention_threshold", "joiner_notification", "joiner_notification_for_inviter",
		"prejoin_dm_welcome_party_notification", "slack_connect_dm_acceptance", "canvas", "mpdm_move",
		"canvas_open_by_default", "canvas_editing_permissions_changed", "channel_canvas_updated",
		"channel_canvas_updated_unread_ignore", "tabbed_canvas_updated", "tabbed_canvas_updated_unread_ignore",
		"automatic_ai_huddle_notes_enabled", "automatic_ai_huddle_notes_disabled", "workflow_channel_opt_in_enabled",
		"workflow_channel_opt_in_disabled", "welcome_party"} {
		m[s] = true
	}
	return m
}()
