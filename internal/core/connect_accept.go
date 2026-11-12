package core

// Accepting a Slack Connect channel invitation from Activity, as Slack's
// web client does it (build d3de3a487b1c, public CDN, no cookie; module
// ids are gantry-v2-async-client-boot-shared's unless marked vendors):
//
//   - dcZX SlackConnectAcceptChannelInviteAction: Accept first asks Me1V
//     fetchAndUpsertChannelAcceptanceEligibility, that is vendors tkxP
//     conversations.sharedInviteAcceptanceEligibility {sig,
//     include_eligible_alternative_actions: true, reason
//     fetch-and-upsert-channel-acceptance-eligibility}. On a Grid
//     workspace that answers grid_workspace_required it asks again with
//     the team the user already accepted on (accepting_sub_team) or the
//     one suggested workspace.
//   - The answer picks the next step: eligible → the multi-pane accept
//     modal (popups/sc_accept.go); eligible but needing an upgrade, or
//     upgrade_required_but_user_cannot → SlackConnectUpgradeModal; an
//     alternative join_channel → vendors WlHm conversations.joinConnectedShared
//     {reason slack-connect-accept-channel-invite-join-channel, sig,
//     invite_id} straight away.
//   - The modal's accept is vendors I6kl conversations.acceptSharedInvite
//     {reason slack-connect-accept-channel-invite, sig, invite_id,
//     channel_name, is_private, free_trial_accepted, [team_id]} (the
//     fetcher snake-cases its args; team_id only for another workspace).
//   - Then g7VB acceptSharedInviteAndMaybeOpenWelcomeModal reads the
//     invite back with nZov connectInvites.list {invite_ids, invite_types:
//     [channel], reason open-welcome-modal}: an approved acceptance opens
//     the channel, and SBlm SharedInviteCreatorWelcomeModal says whether
//     it's connected or waiting on admins (Vd/3: the teams whose review is
//     still missing).

import (
	"context"
	"encoding/json"
	"net/url"
)

// Connect channel invite eligibility reasons Clack acts on (vendors tkxP Sv).
const (
	SCReasonGridWorkspaceRequired = "grid_workspace_required"
	SCReasonUpgradeButCannot      = "upgrade_required_but_user_cannot"
)

// ChannelInviteEligibility is conversations.sharedInviteAcceptanceEligibility's
// answer, the parts the accept flow reads.
type ChannelInviteEligibility struct {
	IsEligible bool   `json:"is_eligible"`
	Reason     string `json:"reason"`
	TeamID     string `json:"team_id"`
	// Requirements: a free trial, an upgrade, and the privacy the
	// accepting side must use (ChannelPrivacyField disables the other).
	Requirements struct {
		Trial          bool `json:"trial"`
		Upgrade        bool `json:"upgrade"`
		ChannelPrivacy *struct {
			Privacy string `json:"privacy"` // "public" | "private" (MhJt SQ)
		} `json:"channel_privacy"`
	} `json:"requirements"`
	Alternatives struct {
		JoinChannel        json.RawMessage `json:"join_channel"`
		JoinWhenConnected  json.RawMessage `json:"join_when_connected"`
		AcceptOnOtherSpace *struct {
			Suggested []struct {
				TeamID string `json:"team_id"`
			} `json:"suggested_workspaces"`
		} `json:"accept_on_other_workspace"`
	} `json:"alternative_actions"`
	ConnectInvite *struct {
		Acceptances []struct {
			AcceptingTeam    struct{ ID string } `json:"accepting_team"`
			AcceptingSubTeam *struct {
				ID string `json:"id"`
			} `json:"accepting_sub_team"`
		} `json:"acceptances"`
	} `json:"connect_invite"`
}

// RequiredPrivacy is the privacy the accepting side has to use ("" when
// either is allowed).
func (e ChannelInviteEligibility) RequiredPrivacy() string {
	if p := e.Requirements.ChannelPrivacy; p != nil {
		return p.Privacy
	}
	return ""
}

// CanJoinChannel reports the join_channel alternative (the user's org is
// already in the channel).
func (e ChannelInviteEligibility) CanJoinChannel() bool {
	return len(e.Alternatives.JoinChannel) > 0 && string(e.Alternatives.JoinChannel) != "null" && string(e.Alternatives.JoinChannel) != "false"
}

func (a *App) channelInviteEligibility(ctx context.Context, sig, teamID string) (ChannelInviteEligibility, error) {
	var r ChannelInviteEligibility
	f := url.Values{"sig": {sig}, "include_eligible_alternative_actions": {"true"},
		"reason": {"fetch-and-upsert-channel-acceptance-eligibility"}}
	if teamID != "" {
		f.Set("team_id", teamID)
	}
	err := a.C.Call(ctx, "conversations.sharedInviteAcceptanceEligibility", f, &r)
	return r, err
}

// ChannelInviteEligible is Me1V fetchAndUpsertChannelAcceptanceEligibility.
func (a *App) ChannelInviteEligible(ctx context.Context, sig string) (ChannelInviteEligibility, error) {
	r, err := a.channelInviteEligibility(ctx, sig, "")
	if err != nil || r.Reason != SCReasonGridWorkspaceRequired {
		return r, err
	}
	// l && d: the workspace the user accepted on; l && u.length === 1:
	// the only suggested workspace
	sub := ""
	if ci := r.ConnectInvite; ci != nil {
		for _, ac := range ci.Acceptances {
			if ac.AcceptingTeam.ID == r.TeamID && ac.AcceptingSubTeam != nil {
				sub = ac.AcceptingSubTeam.ID
			}
		}
	}
	if sub == "" {
		if o := r.Alternatives.AcceptOnOtherSpace; o != nil && len(o.Suggested) == 1 {
			sub = o.Suggested[0].TeamID
		}
	}
	if sub == "" {
		return r, nil
	}
	return a.channelInviteEligibility(ctx, sig, sub)
}

// AcceptChannelInviteOpts are SlackConnectAcceptChannelInviteModal's
// acceptSlackConnectChannelInvite(channelName, privacy, freeTrial,
// workspace).
type AcceptChannelInviteOpts struct {
	Sig, InviteID string
	ChannelName   string
	Private       bool
	FreeTrial     bool
	// TeamID is the workspace picked when it isn't the current one.
	TeamID string
}

// AcceptConnectChannelInvite is conversations.acceptSharedInvite. It
// reports can_open_scdm.
func (a *App) AcceptConnectChannelInvite(ctx context.Context, o AcceptChannelInviteOpts) (bool, error) {
	f := url.Values{"reason": {"slack-connect-accept-channel-invite"}, "sig": {o.Sig}, "invite_id": {o.InviteID},
		"channel_name": {o.ChannelName}, "is_private": {boolStr(o.Private)}, "free_trial_accepted": {boolStr(o.FreeTrial)}}
	if o.TeamID != "" {
		f.Set("team_id", o.TeamID)
	}
	var r struct {
		CanOpenSCDM bool `json:"can_open_scdm"`
	}
	err := a.C.Call(ctx, "conversations.acceptSharedInvite", f, &r)
	return r.CanOpenSCDM, err
}

// JoinConnectedChannel is the join_channel alternative:
// conversations.joinConnectedShared.
func (a *App) JoinConnectedChannel(ctx context.Context, sig, inviteID string) error {
	return a.C.Call(ctx, "conversations.joinConnectedShared", url.Values{"reason": {"slack-connect-accept-channel-invite-join-channel"},
		"sig": {sig}, "invite_id": {inviteID}}, nil)
}

// ConnectInviteState is the invite connectInvites.list answers after an
// accept: what the welcome modal and the navigation read.
type ConnectInviteState struct {
	ChannelID, ChannelName string
	ChannelPrivate         bool
	InvitingTeamID         string
	InvitingTeam           string
	InvitingUser           string // user id
	// Approved is the first acceptance's approval_status == approved
	// (v+N2 vl); PendingTeams are the teams whose review is still
	// missing (Vd/3), the accepting side's first.
	Approved     bool
	PendingTeams []string
	Found        bool
}

// ConnectChannelInvite is nZov connectInvites.list for one channel invite
// (reason open-welcome-modal).
func (a *App) ConnectChannelInvite(ctx context.Context, inviteID string) (ConnectInviteState, error) {
	ids, _ := json.Marshal([]string{inviteID})
	types, _ := json.Marshal([]string{"channel"})
	var r struct {
		ConnectInvites []struct {
			InvitePayload struct {
				Channel struct {
					ID        string `json:"id"`
					Name      string `json:"name"`
					IsPrivate bool   `json:"is_private"`
				} `json:"channel"`
			} `json:"invite_payload"`
			InvitingTeam struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"inviting_team"`
			InvitingUser struct {
				ID string `json:"id"`
			} `json:"inviting_user"`
			Direction   string `json:"direction"`
			Acceptances []struct {
				ApprovalStatus string `json:"approval_status"`
				AcceptingTeam  struct {
					ID string `json:"id"`
				} `json:"accepting_team"`
				AcceptingSubTeam *struct {
					ID string `json:"id"`
				} `json:"accepting_sub_team"`
				Reviews []struct {
					ReviewingTeam struct {
						ID string `json:"id"`
					} `json:"reviewing_team"`
				} `json:"reviews"`
			} `json:"acceptances"`
		} `json:"connect_invites"`
	}
	err := a.C.Call(ctx, "connectInvites.list", url.Values{"invite_ids": {string(ids)}, "invite_types": {string(types)},
		"reason": {"open-welcome-modal"}}, &r)
	if err != nil || len(r.ConnectInvites) == 0 {
		return ConnectInviteState{}, err
	}
	ci := r.ConnectInvites[0]
	st := ConnectInviteState{Found: true, ChannelID: ci.InvitePayload.Channel.ID, ChannelName: ci.InvitePayload.Channel.Name,
		ChannelPrivate: ci.InvitePayload.Channel.IsPrivate, InvitingTeamID: ci.InvitingTeam.ID, InvitingTeam: ci.InvitingTeam.Name,
		InvitingUser: ci.InvitingUser.ID}
	if len(ci.Acceptances) == 0 {
		return st, nil
	}
	ac := ci.Acceptances[0]
	st.Approved = ac.ApprovalStatus == "approved"
	// Vd/3: the inviting team (a) and the accepting side (the sub team r,
	// else the home team i: the accepting team for an incoming invite)
	inviting := ci.InvitingTeam.ID
	home := ac.AcceptingTeam.ID
	if ci.Direction == "outgoing" {
		home = inviting
	}
	sub := ""
	if ac.AcceptingSubTeam != nil {
		sub = ac.AcceptingSubTeam.ID
	}
	var mine, theirs bool
	for _, rv := range ac.Reviews {
		id := rv.ReviewingTeam.ID
		if id == sub || id == home {
			mine = true
		}
		if id == inviting {
			theirs = true
		}
	}
	if !mine {
		st.PendingTeams = append(st.PendingTeams, firstNonEmpty(sub, home))
	}
	if !theirs {
		st.PendingTeams = append(st.PendingTeams, inviting)
	}
	return st, nil
}
