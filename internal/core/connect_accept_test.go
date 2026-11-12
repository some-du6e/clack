package core

import (
	"context"
	"slices"
	"testing"

	"github.com/some-du6e/clack/internal/fakeslack"
)

// The channel invite's Accept against fakeslack (dcZX, Me1V, I6kl, WlHm,
// nZov; docs/ACTIVITY.md "Accepting a Slack Connect channel invite"):
// the eligibility read with its args, the Grid retry on the accepted
// workspace, acceptSharedInvite's snake-cased args, joinConnectedShared,
// and connectInvites.list read back pending (both organisations still to
// review) and approved.
func TestConnectChannelAcceptRoundTrip(t *testing.T) {
	a, fs := actFake(t)
	ctx := context.Background()
	el, err := a.ChannelInviteEligible(ctx, fakeslack.ActivityInviteChannelSig)
	if err != nil || !el.IsEligible || el.RequiredPrivacy() != "" || el.CanJoinChannel() {
		t.Fatalf("eligibility %+v %v", el, err)
	}
	w := actWrite(t, fs, "conversations.sharedInviteAcceptanceEligibility")
	if w.Form.Get("sig") != fakeslack.ActivityInviteChannelSig || w.Form.Get("include_eligible_alternative_actions") != "true" ||
		w.Form.Get("reason") != "fetch-and-upsert-channel-acceptance-eligibility" || w.Form.Has("team_id") {
		t.Errorf("eligibility args %v", w.Form)
	}

	// grid_workspace_required: asked again on the workspace the user
	// accepted on (accepting_sub_team)
	fs.SetChannelInviteEligibility(map[string]any{"is_eligible": false, "reason": "grid_workspace_required", "team_id": fakeslack.TeamID,
		"connect_invite": map[string]any{"acceptances": []any{map[string]any{"accepting_team": map[string]any{"id": fakeslack.TeamID},
			"accepting_sub_team": map[string]any{"id": "T0FAKESUB1"}}}}})
	if _, err := a.ChannelInviteEligible(ctx, fakeslack.ActivityInviteChannelSig); err != nil {
		t.Fatal(err)
	}
	if w := actWrite(t, fs, "conversations.sharedInviteAcceptanceEligibility"); w.Form.Get("team_id") != "T0FAKESUB1" {
		t.Errorf("the Grid retry's team_id %q", w.Form.Get("team_id"))
	}
	// the requirements and the alternatives
	fs.SetChannelInviteEligibility(map[string]any{"is_eligible": true, "requirements": map[string]any{"channel_privacy": map[string]any{"privacy": "private"}}})
	if el, _ := a.ChannelInviteEligible(ctx, fakeslack.ActivityInviteChannelSig); el.RequiredPrivacy() != "private" {
		t.Errorf("required privacy %q", el.RequiredPrivacy())
	}
	fs.SetChannelInviteEligibility(map[string]any{"is_eligible": false, "alternative_actions": map[string]any{"join_channel": map[string]any{}}})
	if el, _ := a.ChannelInviteEligible(ctx, fakeslack.ActivityInviteChannelSig); !el.CanJoinChannel() {
		t.Error("join_channel alternative not read")
	}
	if err := a.JoinConnectedChannel(ctx, fakeslack.ActivityInviteChannelSig, fakeslack.ActivityInviteChannel); err != nil {
		t.Fatal(err)
	}
	if w := actWrite(t, fs, "conversations.joinConnectedShared"); w.Form.Get("reason") != "slack-connect-accept-channel-invite-join-channel" ||
		w.Form.Get("invite_id") != fakeslack.ActivityInviteChannel || w.Form.Get("sig") != fakeslack.ActivityInviteChannelSig {
		t.Errorf("joinConnectedShared %v", w.Form)
	}
	fs.SetChannelInviteEligibility(nil)

	if _, err := a.AcceptConnectChannelInvite(ctx, AcceptChannelInviteOpts{Sig: fakeslack.ActivityInviteChannelSig,
		InviteID: fakeslack.ActivityInviteChannel, ChannelName: "partners-shared", Private: true}); err != nil {
		t.Fatal(err)
	}
	w = actWrite(t, fs, "conversations.acceptSharedInvite")
	for k, want := range map[string]string{"reason": "slack-connect-accept-channel-invite", "sig": fakeslack.ActivityInviteChannelSig,
		"invite_id": fakeslack.ActivityInviteChannel, "channel_name": "partners-shared", "is_private": "true", "free_trial_accepted": "false"} {
		if got := w.Form.Get(k); got != want {
			t.Errorf("acceptSharedInvite %s = %q, want %q", k, got, want)
		}
	}
	if w.Form.Has("team_id") {
		t.Error("team_id sent for the current workspace")
	}

	st, err := a.ConnectChannelInvite(ctx, fakeslack.ActivityInviteChannel)
	if err != nil || !st.Found {
		t.Fatalf("connectInvites.list %+v %v", st, err)
	}
	if st.Approved || st.ChannelID != fakeslack.ActivityInviteChannelID || st.ChannelName != "partners-shared" || !st.ChannelPrivate ||
		!slices.Equal(st.PendingTeams, []string{fakeslack.TeamID, fakeslack.ExternalTeamID}) || st.InvitingTeam != "Partner Co" {
		t.Errorf("pending invite %+v", st)
	}
	if w := actWrite(t, fs, "connectInvites.list"); w.Form.Get("invite_ids") != `["`+fakeslack.ActivityInviteChannel+`"]` ||
		w.Form.Get("invite_types") != `["channel"]` || w.Form.Get("reason") != "open-welcome-modal" {
		t.Errorf("connectInvites.list %v", w.Form)
	}
	fs.SetChannelInviteApproved(true)
	if st, _ := a.ConnectChannelInvite(ctx, fakeslack.ActivityInviteChannel); !st.Approved || len(st.PendingTeams) != 0 {
		t.Errorf("approved invite %+v", st)
	}
}
