package core

// Wave 10 (docs/EXPERIMENTS.md, "2026-11-15 wave 10"): the channel
// details rosters' member actions under agent_channel_header (client-boot
// M/qt Zc), from Slack's web client build c07ab895506f.
//
// The roster rows (ia-details-popover-members IADetailsPopoverMemberItem,
// both the Members tab and, under agents_in_channels_improvements, the
// Agents & apps tab) carry IaDetailsPopoverMemberItemActionsMenu, the "…"
// menu, for a member in the channel. Its "Remove from channel" item shows
// when tu's canRemoveMember holds: never for Slackbot (gmjA U); with Zc on,
// an app or bot member (qSRE isAppOrBot) is canUserRemoveAppFromChannel
// (boot-shared QvQO I), unless it is an Agentforce bot in a channel whose
// agent configuration is locked to channel managers (zrjN r); everyone
// else, and every member with Zc off, is canUserKickMemberFromChannel
// (8ZZ9 R6). The item opens RemoveMemberDialog (remove-member-dialog
// LUCM), whose Remove runs submitKickFromChannel (6ndL AM:
// conversations.kick {channel, user}).
//
// This file is the data half: the checks and the kick. The menu and the
// dialog are views/details_agents_menu.go and popups/remove_member.go;
// the menu's Channel Manager items are channel_managers_edit.go's.

import (
	"context"
	"encoding/json"
	"net/url"

	"github.com/some-du6e/clack/internal/slack"
)

// ExpAgentChannelHeader is M/qt Zc's experiment.
const ExpAgentChannelHeader = "agent_channel_header"

// AgentChannelHeader is M/qt Zc: agent_channel_header is on.
func (a *App) AgentChannelHeader() bool { return a.experiment(ExpAgentChannelHeader) == "on" }

// The kick grants (QxkB's permission enum keys): REMOVE_FROM_CHANNEL and
// REMOVE_FROM_PRIVATE_CHANNEL in the channel (QaSy WB), and the org-level
// channel management grants (9WQJ LI and Po: QaSy tA on Enterprise Grid).
const (
	PermRemoveFromChannel        = "REMOVE_FROM_CHANNEL"
	PermRemoveFromPrivateChannel = "REMOVE_FROM_PRIVATE_CHANNEL"
	PermManagePublicChannels     = "MANAGE_PUBLIC_CHANNELS"
	PermManagePrivateChannels    = "MANAGE_PRIVATE_CHANNELS"
)

// ExpNativeChannelExpert is kzB8 r's experiment (zrjN r's before): a
// channel's agent configuration can be locked to channel managers. The
// check read ncea_allow_channel_managers_to_lock_configuration until Slack
// renamed its gate to enable_native_channel_expert (build 8f19be21ed7e,
// the 2026-11-21 drift; about-channel-modal's
// isChannelAgentConfigRestrictedToManagers reads the same).
const ExpNativeChannelExpert = "enable_native_channel_expert"

// removeConv is the stored conversation ch (the viewer's, or one read
// from conversations.info).
func (a *App) removeConv(ch string) (*slack.Conversation, bool) {
	if c, ok := a.S.Conv(ch); ok {
		return c, true
	}
	return a.S.ExtConv(ch)
}

// canKickFromChannel is canUserKickFromChannel (+8Lm Vj): a known
// conversation; not for a guest (QLH5 sV); not an archived one, a DM or
// group DM, #general or an org-mandatory channel (isChannelKickable);
// then the viewer manages the org's channels of that kind (getCanManage:
// 9WQJ LI MANAGE_PUBLIC_CHANNELS or Po MANAGE_PRIVATE_CHANNELS, on
// Enterprise Grid) or holds REMOVE_FROM_CHANNEL (public) or
// REMOVE_FROM_PRIVATE_CHANNEL (private) in it. 9WQJ's other two paths
// (H7: a workspace admin on a Plus-or-higher plan; tU: the compliance
// export plan's MANAGE_WORKSPACE_PRIVATE_CHANNELS) read plan details
// Clack doesn't load; a workspace admin's grants come back from
// permissions/info all the same.
func (a *App) canKickFromChannel(c *slack.Conversation) bool {
	if c == nil || a.viewerRestricted() {
		return false
	}
	if c.IsArchived || c.IsIM || c.IsMPIM || c.IsGeneral || c.IsOrgMandatory {
		return false
	}
	private := c.IsPrivate || c.IsGroup
	if a.IsEnterprise() {
		manage := PermManagePublicChannels
		if private {
			manage = PermManagePrivateChannels
		}
		if a.TeamGrant(manage) {
			return true
		}
	}
	perm := PermRemoveFromChannel
	if private {
		perm = PermRemoveFromPrivateChannel
	}
	return a.ChannelGrant(c.ID, perm)
}

// canKickMemberFromChannel is canUserKickMemberFromChannelWithReason
// (8ZZ9 ZM).ok: canKickFromChannel; an external member of a Slack
// Connect channel only when, with sc_remove_full_access_user (MG+t mL),
// the channel is hosted by the viewer's org or workspace (2ES1 b reads
// conversation_host_id), or without it when the member is also external
// limited there (zOfV, which Clack doesn't load: never); not the viewer;
// and a known member of the channel (sBq1 Sv: the roster lists members).
func (a *App) canKickMemberFromChannel(c *slack.Conversation, member string, mem *slack.User) bool {
	if member == "" || !a.canKickFromChannel(c) {
		return false
	}
	if mem != nil && a.userOf(mem).External && c.IsExtShared {
		host := c.ConversationHostID
		hostedHere := host == "" || (a.C != nil && (host == a.C.Creds.EnterpriseID || host == a.C.Creds.Team))
		if a.experiment("sc_remove_full_access_user") != "on" || !hostedHere {
			return false
		}
	}
	return member != a.S.SelfID()
}

// canRemoveAppFromChannel is canUserRemoveAppFromChannel (QvQO I): not
// for a guest; a channel the viewer can post in (o/IU c), not archived,
// not an external member, and not a DM or group DM unless the member is
// an app user (nMbN $S); then, with channel_details_agents_apps_tab or
// agent_channel_header on, canKickFromChannel too.
func (a *App) canRemoveAppFromChannel(c *slack.Conversation, mem *slack.User) bool {
	if c == nil || mem == nil || a.viewerRestricted() {
		return false
	}
	if !a.PostingFor(c).CanPost() || c.IsArchived || a.userOf(mem).External {
		return false
	}
	if !mem.IsAppUser && (c.IsIM || c.IsMPIM) {
		return false
	}
	if a.experiment("channel_details_agents_apps_tab") == "on" || a.AgentChannelHeader() {
		return a.canKickFromChannel(c)
	}
	return true
}

// agentConfigLocked is kzB8 r: enable_native_channel_expert on and the
// channel's who_can_manage_channel_agent is channel managers,
// for a viewer who can't manage the channel (hWVz C: a channel manager
// here, or an org admin).
func (a *App) agentConfigLocked(c *slack.Conversation) bool {
	if a.experiment(ExpNativeChannelExpert) != "on" {
		return false
	}
	var p struct {
		Who string `json:"who_can_manage_channel_agent"`
	}
	if len(c.Properties) > 0 {
		_ = json.Unmarshal(c.Properties, &p)
	}
	if p.Who != "channel_managers" {
		return false
	}
	return !a.isChannelManager(c.ID) && !a.selfIsEnterpriseAdmin()
}

// selfIsEnterpriseAdmin is Xfgd FQ for the viewer.
func (a *App) selfIsEnterpriseAdmin() bool {
	u, ok := a.S.User(a.S.SelfID())
	return ok && a.userOf(u).EnterpriseAdmin
}

// CanRemoveMemberFromChannel is the roster row's canRemoveMember (+VDd
// tu's l) for member in ch.
func (a *App) CanRemoveMemberFromChannel(ch, member string) bool {
	if member == SlackbotUserID {
		return false
	}
	c, ok := a.removeConv(ch)
	if !ok {
		return false
	}
	mem, _ := a.S.User(member)
	if mem != nil && mem.IsSlackbot {
		return false
	}
	if a.AgentChannelHeader() && mem != nil && isAppOrBotUser(mem) {
		return a.canRemoveAppFromChannel(c, mem) && !(mem.IsAgentforceBot && a.agentConfigLocked(c))
	}
	return a.canKickMemberFromChannel(c, member, mem)
}

// isAppOrBotUser is qSRE isAppOrBot: is_app_user, is_bot, or "USLACK"
// (o6sH _).
func isAppOrBotUser(u *slack.User) bool {
	return u != nil && (u.IsAppUser || u.IsBot || u.ID == "USLACK")
}

// KickFromChannel is submitKickFromChannel (6ndL AM): conversations.kick
// {channel, user}. The channel's member counts are read again after it.
func (a *App) KickFromChannel(ctx context.Context, ch, member string) error {
	if err := a.C.Call(ctx, "conversations.kick", url.Values{"channel": {ch}, "user": {member}}, nil); err != nil {
		return err
	}
	a.mu.Lock()
	delete(a.memberN, ch)
	delete(a.memberG, ch)
	delete(a.memberA, ch)
	a.mu.Unlock()
	return nil
}

// RemoveMemberFacts are the props RemoveMemberDialog's connect reads
// (LUCM P) for member in ch.
type RemoveMemberFacts struct {
	Private       bool // aK5v Zp: a private channel
	SfChannel     bool // aK5v rI: a record channel
	External      bool // 9XL8 g: the member is external
	ScRemoveFull  bool // MG+t mL: sc_remove_full_access_user
	AgentHeader   bool // M/qt Zc: agent_channel_header
	ExtShared     bool // the conversation is Slack Connect (RemoveMemberDialogContent's note)
	ChannelExists bool
}

// RemoveMemberFacts reads them. isExternalLimited (zOfV) and
// isChannelVisitor (9PA9 Oh) need Slack Connect team data and channel
// visitors, which Clack doesn't load: both false; so is
// isAgentforceChannelExpertAgent (0CyJ: the Agentforce channel expert's
// app, which Clack doesn't read either).
func (a *App) RemoveMemberFacts(ch, member string) RemoveMemberFacts {
	c, ok := a.removeConv(ch)
	f := RemoveMemberFacts{ChannelExists: ok,
		ScRemoveFull: a.experiment("sc_remove_full_access_user") == "on", AgentHeader: a.AgentChannelHeader()}
	if ok {
		f.Private = (c.IsPrivate || c.IsGroup) && !c.IsIM && !c.IsMPIM
		f.SfChannel = convRecordChannel(c)
		f.ExtShared = c.IsExtShared
	}
	if u, ok := a.S.User(member); ok {
		f.External = a.userOf(u).External
	}
	return f
}
