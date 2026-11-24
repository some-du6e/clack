package core

import (
	"context"
	"errors"
	"strings"

	"github.com/some-du6e/clack/internal/slack"
)

// maybeLeaveChannel ("Kick off the intent to leave a channel", boot-shared
// AdX+ ee, web client 66e72b57ade1, 2026-11-24): what leaving a channel
// does, the channel menus' Leave and /leave alike. It decides an action:
//
//   - canUserLeaveChannelWithReason (yZl2 Ef) refuses: LeaveChannelError's
//     alert for the reason;
//   - a channel that isn't private is left at once, unless it is an
//     agent's record channel the viewer may archive (zdO/ $n, 2Uru Jz);
//   - an archived one is closed (closeChannelApi);
//   - a Slack Connect one asks first (the LeaveChannel confirmation);
//   - otherwise the channel's membership counts are read again
//     (fetchAndUpsertMembershipCountsForChannel): the last member gets
//     the archive dialog with "Leave and archive"
//     (openArchiveDialog({andLeave: true})); a private channel asks first;
//     the agent's channel is left.
//
// Leaving is leaveConversationApi (conversations.leave), and its failure
// with an error code is LeaveChannelError's alert for that code. None of
// it navigates: leaveChannel marks the channel left (is_member false), and
// channel_left drops it from the sidebar.

// maybeLeaveChannel's actions (AdX+ B, U, W, H).
const (
	LeaveNow            = "leave"
	LeaveClose          = "close"
	LeaveConfirm        = "confirm_and_leave"
	LeaveConfirmArchive = "confirm_leave_and_archive"
)

// canUserLeaveChannelWithReason's reasons (yZl2 JH), LeaveChannelError's
// cases; an API error code is one too.
const (
	LeaveChannelNotFound       = "channel_not_found"
	LeaveCantLeaveGeneral      = "cant_leave_general"
	LeaveCantLeaveMandatory    = "cant_leave_mandatory_shared_channel"
	LeaveUserIsRestricted      = "user_is_restricted"
	LeaveRestrictedLastChannel = "user_is_restricted_last_channel"
	LeaveUltraRestricted       = "user_is_ultra_restricted"
	LeaveUserIsFrontline       = "user_is_frontline"
	LeaveLastMember            = "last_member"
)

// LeaveRefusal is canUserLeaveChannelWithReason (yZl2 Ef) for ch: "" when
// the viewer may leave it. A conversation the store doesn't hold may be
// left (the check's unknown channel), a file's conversation can't.
func (a *App) LeaveRefusal(ch string) string {
	if ch == "" {
		return LeaveChannelNotFound
	}
	c, _ := a.S.Conv(ch)
	if c != nil && c.IsFile { // gxWy K3 isFileChannel
		return LeaveChannelNotFound
	}
	self, _ := a.S.User(a.S.SelfID())
	im := c != nil && (c.IsIM || c.IsMPIM)
	if self != nil && self.IsUltraRestricted && !im { // HkVr AB
		return LeaveUltraRestricted
	}
	if c == nil {
		return ""
	}
	switch {
	case c.IsGeneral: // 8SLZ N
		return LeaveCantLeaveGeneral
	case c.IsOrgMandatory: // zdO/ gx
		return LeaveCantLeaveMandatory
	}
	if self == nil || !self.IsRestricted { // HkVr sV
		return ""
	}
	channel := publicChannel(c) || privateChannel(c)
	switch {
	case self.RestrictedUserType == "frontline_worker" && channel: // stIt q
		return LeaveUserIsFrontline
	case publicChannel(c):
		return LeaveUserIsRestricted
	}
	// a guest keeps one channel: another public or private one they're in,
	// not archived (EmXR DM)
	a.S.RLock()
	defer a.S.RUnlock()
	for id, o := range a.S.Convs {
		if id != ch && (publicChannel(o) || privateChannel(o)) && o.IsMember && !o.IsArchived {
			return ""
		}
	}
	return LeaveRestrictedLastChannel
}

// errNoLeaveChannel is maybeLeaveChannel's "Non-existent channel ID".
var errNoLeaveChannel = errors.New("core: non-existent channel to leave")

// LeaveChannelAction is maybeLeaveChannel's decision for ch: an action
// above, or "" with the refusal's reason. It may read the channel's
// membership counts (ctx bounds that).
func (a *App) LeaveChannelAction(ctx context.Context, ch string) (action, reason string, err error) {
	c, ok := a.S.Conv(ch) // getChannelById without the fetch
	if !ok || c == nil {
		return "", "", errNoLeaveChannel
	}
	if r := a.LeaveRefusal(ch); r != "" {
		return "", r, nil
	}
	grant := func(p string) bool { return a.ChannelGrant(ch, p) }
	agent := agentRecordChannel(c) && !c.IsArchived && a.canArchive(c, grant)
	switch {
	case !privateChannel(c) && !agent:
		return LeaveNow, "", nil
	case c.IsArchived:
		return LeaveClose, "", nil
	case c.IsExtShared:
		return LeaveConfirm, "", nil
	}
	if err := a.loadMemberCountNow(ctx, ch); err == nil {
		a.mu.Lock()
		n := a.memberMC[ch].People // membershipCounts' member_count
		a.mu.Unlock()
		if n == 1 {
			return LeaveConfirmArchive, "", nil
		}
	}
	if privateChannel(c) {
		return LeaveConfirm, "", nil
	}
	return LeaveNow, "", nil
}

// agentRecordChannel is zdO/ $n: a record channel of an agent
// (record_type agent_channel, or a record id starting "AC:").
func agentRecordChannel(c *slack.Conversation) bool {
	p := propsOf(c).RecordChannel
	return p.RecordID != "" && (p.RecordType == "agent_channel" || strings.HasPrefix(p.RecordID, "AC:"))
}

// LeaveErrorTeamName is LeaveChannelError's {teamName} (rUVB F1 of the
// current team) and {orgName} (the org's name, Ikiy): the org's on a
// grid, else the team's.
func (a *App) LeaveErrorTeamName() string { return a.SlackConnectTeamName() }

// SelfIsWorkspaceAdmin is isCurrentUserWorkspaceAdmin (Bn/N Zo): the
// viewer's is_admin.
func (a *App) SelfIsWorkspaceAdmin() bool {
	u, ok := a.S.User(a.S.SelfID())
	return ok && u.IsAdmin
}

// IsRecordChannel is zdO/ rI for ch: a record channel
// (properties.record_channel.record_id).
func (a *App) IsRecordChannel(ch string) bool {
	c, ok := a.S.Conv(ch)
	return ok && c != nil && propsOf(c).RecordChannel.RecordID != ""
}
