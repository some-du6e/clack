package core

import (
	"github.com/some-du6e/clack/internal/slack"
)

// /remove (alias /kick) is one of Slack's client commands (Zrd3
// getCommands; slash-commands-local p8LL removeCommand, web client
// 66e72b57ade1, 2026-11-24): the server's /remove never reaches the slash
// command store (bulkAddServerCommands drops it, slash_store.go), so
// chat.command never carries it. removeCommand, after
// isSlashCommandEnabled, in order:
//
//   - the first argument must be a mention of someone who looks like a
//     member (looksLikeMemberId): else "Please specify someone to remove!";
//   - the Channel Expert agent (FbgX h: UCHANNELEXPERT): "*{agent_name}*
//     can be removed in Channel Settings.", its name when the store holds
//     it (getNameForInviteErrors) else "this agent"; with
//     isNativeChannelExpertAvailable (LpJ9 TB) the feedback carries a "Go
//     to Agents Tab" button too, which Clack's account never gets
//     (enable_native_channel_expert isn't the account's);
//   - a member the store calls non-existent (getMemberById, a miss asked
//     for): "*{input}* is not a recognized member name.", the input as
//     display text;
//   - the viewer: maybeLeaveChannel for the channel;
//   - canUserKickMemberFromChannelWithReason (cxgb ZM, kickMemberRefusal)
//     refuses with its reason's text;
//   - otherwise openRemoveMemberDialog (7TNs): the roster's Remove from
//     channel confirmation (popups.OpenRemoveMemberDialog), whose Remove
//     is conversations.kick.
//
// The names in its texts are getNameForInviteErrors' (VRXO sE with the
// "@"): "@" and the member's pref-compliant name ("@" alone for one the
// store doesn't hold).

// RemoveTarget is what /remove does once its checks pass.
type RemoveTarget struct {
	// Member is the person openRemoveMemberDialog asks about.
	Member string
	// Leave: the viewer named themselves; maybeLeaveChannel for the
	// channel.
	Leave bool
}

// Kick refusal reasons (canUserKickFromChannelWithReason's Tx+q CE,
// canUserKickMemberFromChannelWithReason's cxgb ev), and none.
const (
	kickOK                = ""
	kickInvalidParams     = "invalid_params"
	kickUserIsRestricted  = "user_is_restricted"
	kickArchivedChannel   = "archived_channel"
	kickInvalidDM         = "invalid_dm"
	kickRequiredChannel   = "required_channel"
	kickRestrictedPublic  = "restricted_public_channel"
	kickRestrictedPrivate = "restricted_private_channel"
	kickExternalMember    = "external_member"
	kickCantKickSelf      = "cant_kick_self"
	kickNotInChannel      = "not_in_channel"
)

// RemoveCommand is removeCommand for the parsed arguments in ch; input is
// deltaWithoutCmd as display text (convertContentsToStringForDisplay).
func (a *App) RemoveCommand(cmd, ch string, params []CommandParam, input string) (RemoveTarget, error) {
	id := ""
	if len(params) > 0 && params[0].Type == "mention" && LooksLikeMemberID(params[0].Value) {
		id = params[0].Value
	}
	if id == "" {
		return RemoveTarget{}, customErr(cmd, "Please specify someone to remove!")
	}
	state := a.MemberStateOf(id) // getMemberById: a miss is asked for
	if id == channelExpertID {
		name := "this agent"
		if state == MemberKnown {
			name = a.nameForInviteErrors(id)
		}
		return RemoveTarget{}, customErr(cmd, "*"+name+"* can be removed in Channel Settings.")
	}
	if state == MemberNonExistent {
		return RemoveTarget{}, customErr(cmd, "*"+input+"* is not a recognized member name.")
	}
	if state == MemberKnown && id == a.S.SelfID() { // isSelf: the member's is_self
		return RemoveTarget{Leave: true}, nil
	}
	c, _ := a.S.Conv(ch)
	switch a.kickMemberRefusal(c, id) {
	case kickOK:
		return RemoveTarget{Member: id}, nil
	case kickInvalidDM:
		return RemoveTarget{}, customErr(cmd, "You can’t remove someone from a direct message.")
	case kickRequiredChannel:
		return RemoveTarget{}, customErr(cmd, "All members of this workspace are meant to be in *"+channelInText(c)+"*, so you won’t be able to remove anyone.")
	case kickArchivedChannel:
		return RemoveTarget{}, customErr(cmd, "You can’t remove anyone from *"+channelInText(c)+"* while it is archived.")
	case kickUserIsRestricted:
		return RemoveTarget{}, customErr(cmd, "Removing someone from a channel is a restricted action.")
	case kickRestrictedPrivate:
		return RemoveTarget{}, customErr(cmd, "Removing someone from a private channel is a restricted action.")
	case kickRestrictedPublic:
		return RemoveTarget{}, customErr(cmd, "Removing members from public channels has been restricted by a Workspace Owner or Admin. Try contacting them for help!")
	case kickNotInChannel:
		kind := "channel"
		if privateChannel(c) {
			kind = "private channel"
		}
		return RemoveTarget{}, customErr(cmd, "*"+a.nameForInviteErrors(id)+"* is not a member of this "+kind+".")
	case kickExternalMember:
		return RemoveTarget{}, customErr(cmd, "You can’t remove "+a.nameForInviteErrors(id)+" from this channel, because they’re from another organization.")
	}
	return RemoveTarget{}, customErr(cmd, "Hmm, something went wrong. Try again?")
}

// nameForInviteErrors is getNameForInviteErrors (VRXO sE(state, member,
// false, true)): "@" and the member's getPrefCompliantMemberName, "@" for
// a member the store doesn't hold (the unknown stub has no name).
func (a *App) nameForInviteErrors(id string) string {
	n, _ := a.S.MemberName(id)
	return "@" + n
}

// privateChannel is zdO/ Zp isPrivateChannel.
func privateChannel(c *slack.Conversation) bool {
	return c != nil && (c.IsPrivate || c.IsGroup) && !c.IsMPIM
}

// publicChannel is zdO/ rW isPublicChannel.
func publicChannel(c *slack.Conversation) bool {
	return c != nil && c.IsChannel && !privateChannel(c) && !c.IsMPIM
}

// channelInText is getDisplayNameForChannel (hsKo SB) with its defaults
// for a channel: "#name" for a public one, a private one's bare name.
func channelInText(c *slack.Conversation) string {
	if c == nil {
		return ""
	}
	if publicChannel(c) {
		return "#" + c.Name
	}
	return c.Name
}

// kickRefusal is canUserKickFromChannelWithReason (Tx+q VJ) for c: a
// known conversation of a known type (zdO/ jq); not for a guest (HkVr
// sV); isChannelKickableWithReason (not archived, not a DM or group DM,
// not #general or org-mandatory, zdO/ gx); then the viewer manages the
// org's channels of that kind (getCanManage, RbKF: on Enterprise Grid
// MANAGE_PUBLIC_CHANNELS or MANAGE_PRIVATE_CHANNELS) or holds
// REMOVE_FROM_CHANNEL or REMOVE_FROM_PRIVATE_CHANNEL in it
// (getPermissionForChannel: a miss is asked for and reads as not
// allowed). getCanManage's two other paths (H7: a workspace admin on a
// Plus-or-higher plan or a Plus trial; tU: MANAGE_WORKSPACE_PRIVATE_CHANNELS
// on a plan with compliance exports) read plan details Clack doesn't load,
// as the roster's canKickFromChannel says.
func (a *App) kickRefusal(c *slack.Conversation) string {
	if !knownChannel(c) {
		return kickInvalidParams
	}
	if a.selfRestricted() {
		return kickUserIsRestricted
	}
	switch {
	case c.IsArchived:
		return kickArchivedChannel
	case c.IsIM || c.IsMPIM:
		return kickInvalidDM
	case c.IsGeneral || c.IsOrgMandatory:
		return kickRequiredChannel
	}
	private := !publicChannel(c)
	manage, perm := PermManagePublicChannels, PermRemoveFromChannel
	if private {
		manage, perm = PermManagePrivateChannels, PermRemoveFromPrivateChannel
	}
	if a.IsEnterprise() && a.TeamGrant(manage) || a.ChannelGrant(c.ID, perm) {
		return kickOK
	}
	if private {
		return kickRestrictedPrivate
	}
	return kickRestrictedPublic
}

// kickMemberRefusal is canUserKickMemberFromChannelWithReason (cxgb ZM)
// for member in c: someone who looks like a member, in a known
// conversation; kickRefusal; an external member of a Slack Connect
// channel only when, with sc_remove_full_access_user, the channel isn't
// hosted by another organization (yAjA b), or without it when the member
// is also external limited there and it isn't (fNOP, which Clack doesn't
// load: never); not the viewer; and a known member of the channel
// (getChannelMembershipInfoForUser, membership.go: a miss is asked for and
// reads as not a member).
func (a *App) kickMemberRefusal(c *slack.Conversation, member string) string {
	if !LooksLikeMemberID(member) || !knownChannel(c) {
		return kickInvalidParams
	}
	if r := a.kickRefusal(c); r != kickOK {
		return r
	}
	if u, ok := a.S.RawUser(member); ok && u != nil && a.userOf(u).External && c.IsExtShared {
		here := !a.hostedAway(c)
		if !(a.experiment("sc_remove_full_access_user") == "on" && here) {
			return kickExternalMember
		}
	}
	if member == a.S.SelfID() {
		return kickCantKickSelf
	}
	if m := a.membershipOf(c.ID, member); m.Known && m.Member {
		return kickOK
	}
	return kickNotInChannel
}
