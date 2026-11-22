package core

// Directories › External and Invitations: Slack's Slack Connect hub, as
// its web client reads it (build a09b625d90aa, 2026-11-22; public CDN, no
// cookie; module ids are client-boot / boot-shared / the slack-connect-hub
// chunks'):
//
//   - External (ExternalOrganizationsView, SjEJ): useExternalTeamsList
//     (c5UU) reads Flannel's externalTeams/list {count, filter: connected
//     (60) | available (20, paged by next_marker), marker} (Xgp+ af), then
//     externalTeams/counts {team_ids, count} (Xgp+ Cl) for the teams it has
//     no counts for. fetchAndUpsertExternalTeamsCounts (SZOG) adds
//     conversations_member_of, the open DMs connected to the team (eyz3 S,
//     pS2E gs), and sums total. An "available" team the viewer already
//     shares a conversation or channel with is left out.
//   - Invitations (SlackConnectPage, gmwg): fetchAndResetAllConnectInvites
//     (PC2X) asks connectInvites.list {invite_types: [channel, im],
//     include_invite_requests: true} and splits it into the inbound and the
//     outbound invites (F5qO Ef / I4). On a grid the team host answers
//     team_is_restricted, so the call goes to the enterprise host (Client
//     learns the route).
//   - Revoke (SlackConnectRevokeInviteModal, JDA4): a channel invite
//     conversations.revokeSharedInvite {invite_id}, a DM invite
//     sharedInvites.revoke {id}; then the outgoing invites are read again.
//
// Who sees what (all read from what the client already has):
//
//   - canUserInviteExternalMembersWithReason (RRVt lR): not a guest, may
//     create a channel, the allowed-workspaces pref, not a free team, and
//     the org's can_create_slack_connect_channel_invite /
//     can_create_external_limited_invite with the viewer in their
//     who_can_… (bCT7 l6, UIJR _d, JGki y). Failing any, ok is false on a
//     paid team (true with the reasons on a free one: it may upgrade).
//   - shouldUserSeeSlackConnectAdminContent (3jjM): a paid team, and on a
//     grid an org admin or owner (ltQJ n5 / rr), else a workspace admin.
//   - The DM button: g/ka u, with light_read_who_can_dm_anyone_pref_migration
//     off the DM eligibility (I0Fa aJ: sharedInvites.canGetLink
//     is_eligible), on it the CREATE_SLACK_CONNECT_DM permission.

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"slices"
	"strings"
	"time"
)

// ExternalTeam is a team externalTeams/list answers (Flannel's team).
type ExternalTeam struct {
	ID, Name, Domain string
	// Icon is the team's image_88 (the 36px row avatar at 2x); IconDefault
	// says it is Slack's generated one.
	Icon        string
	IconDefault bool
	// IconLarge is the largest image (image_230, for the org page's 160px
	// icon and its banner).
	IconLarge   string
	IsVerified  bool
	DateCreated int64
}

// ExternalTeamCount is externalTeams/counts for a team, with
// conversations_member_of and total added as SZOG adds them.
type ExternalTeamCount struct {
	ConversationsMemberOf, PublicMemberOf, PublicNotMemberOf, PrivateMemberOf int
	Total                                                                     int
}

type externalTeamRaw struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Domain      string `json:"domain"`
	IsVerified  bool   `json:"is_verified"`
	DateCreated int64  `json:"date_created"`
	Icon        struct {
		Default  bool   `json:"image_default"`
		Image44  string `json:"image_44"`
		Image68  string `json:"image_68"`
		Image88  string `json:"image_88"`
		Image132 string `json:"image_132"`
		Image230 string `json:"image_230"`
	} `json:"icon"`
}

func (r externalTeamRaw) team() ExternalTeam {
	return ExternalTeam{ID: r.ID, Name: r.Name, Domain: r.Domain, IsVerified: r.IsVerified, DateCreated: r.DateCreated,
		Icon: firstNonEmpty(r.Icon.Image88, r.Icon.Image68, r.Icon.Image44), IconDefault: r.Icon.Default,
		IconLarge: firstNonEmpty(r.Icon.Image230, r.Icon.Image132, r.Icon.Image88, r.Icon.Image68, r.Icon.Image44)}
}

// ExternalTeams is fetchExternalTeamsList: externalTeams/list {count,
// filter, marker?}. filter is "connected" or "available". It returns the
// page and its next_marker ("" when done).
func (a *App) ExternalTeams(ctx context.Context, filter string, count int, marker string) ([]ExternalTeam, string, error) {
	body := map[string]any{"count": count, "filter": filter}
	if marker != "" {
		body["marker"] = marker
	}
	var res struct {
		Results    []externalTeamRaw `json:"results"`
		NextMarker string            `json:"next_marker"`
	}
	if err := a.C.Edge(ctx, "externalTeams/list", body, &res); err != nil {
		return nil, "", err
	}
	out := make([]ExternalTeam, 0, len(res.Results))
	for _, r := range res.Results {
		if r.ID != "" {
			out = append(out, r.team())
		}
	}
	a.noteExternalTeams(out)
	return out, res.NextMarker, nil
}

// SearchExternalTeams is fetchAndUpsertExternalTeamsWithQuery:
// externalTeams/search {query} (the hub's searcher, its teams entity).
func (a *App) SearchExternalTeams(ctx context.Context, query string) ([]ExternalTeam, error) {
	var res struct {
		Results []externalTeamRaw `json:"results"`
	}
	if err := a.C.Edge(ctx, "externalTeams/search", map[string]any{"query": query}, &res); err != nil {
		return nil, err
	}
	out := make([]ExternalTeam, 0, len(res.Results))
	for _, r := range res.Results {
		if r.ID != "" {
			out = append(out, r.team())
		}
	}
	a.noteExternalTeams(out)
	return out, nil
}

// ExternalTeamCounts is fetchAndUpsertExternalTeamsCounts:
// externalTeams/counts {team_ids, count}, with conversations_member_of
// counted from the open DMs connected to each team.
func (a *App) ExternalTeamCounts(ctx context.Context, ids []string, count int) (map[string]ExternalTeamCount, error) {
	var res struct {
		Counts map[string]struct {
			PublicMemberOf    int `json:"public_channels_member_of"`
			PublicNotMemberOf int `json:"public_channels_not_member_of"`
			PrivateMemberOf   int `json:"private_channels_member_of"`
		} `json:"counts"`
	}
	if err := a.C.Edge(ctx, "externalTeams/counts", map[string]any{"team_ids": ids, "count": count}, &res); err != nil {
		return nil, err
	}
	dms := a.connectedDMTeams()
	out := make(map[string]ExternalTeamCount, len(res.Counts))
	for id, c := range res.Counts {
		n := ExternalTeamCount{ConversationsMemberOf: dms[id], PublicMemberOf: c.PublicMemberOf, PublicNotMemberOf: c.PublicNotMemberOf,
			PrivateMemberOf: c.PrivateMemberOf}
		n.Total = n.ConversationsMemberOf + n.PublicMemberOf + n.PublicNotMemberOf + n.PrivateMemberOf
		out[id] = n
	}
	return out, nil
}

// SharesWith is the available list's filter (c5UU): a team the viewer
// already has a conversation or a channel with.
func (c ExternalTeamCount) SharesWith() bool {
	return c.ConversationsMemberOf > 0 || c.PublicMemberOf > 0 || c.PrivateMemberOf > 0
}

// connectedDMTeams counts the open DMs per connected team (eyz3 S: open
// DMs; pS2E gs: their connected_team_ids).
func (a *App) connectedDMTeams() map[string]int {
	out := map[string]int{}
	a.S.RLock()
	defer a.S.RUnlock()
	for _, c := range a.S.Convs {
		if c == nil || !c.IsIM || !c.IsOpen {
			continue
		}
		for _, t := range c.ConnectedTeamIDs {
			out[t]++
		}
	}
	return out
}

// externalTeams keeps the teams the hub read, for its rows (Slack's
// teams store, 1Nz1 OU upsertTeams).
type externalTeamStore struct {
	m map[string]ExternalTeam
}

var externalTeamIndex = newPerApp()

func (a *App) extTeams() *externalTeamStore {
	v, _ := externalTeamIndex.LoadOrStore(a, &externalTeamStore{m: map[string]ExternalTeam{}})
	return v.(*externalTeamStore)
}

func (a *App) noteExternalTeams(ts []ExternalTeam) {
	s := a.extTeams()
	a.S.Lock()
	for _, t := range ts {
		s.m[t.ID] = t
	}
	a.S.Unlock()
}

// ExternalTeam is a team the hub read (ok false when it didn't).
func (a *App) ExternalTeamByID(id string) (ExternalTeam, bool) {
	s := a.extTeams()
	a.S.RLock()
	defer a.S.RUnlock()
	t, ok := s.m[id]
	return t, ok
}

// ---- invitations ----

// ConnectInvite is a connect invite as the hub's rows read it (F5qO's
// accessors).
type ConnectInvite struct {
	ActivityInvite
	DateCreated, DateLastUpdated int64
	RecipientEmail               string
	RecipientUserID              string
	Link                         string // a link invite's
	ChannelID, ChannelName       string
	ChannelPrivate               bool
	Message                      string
	InviterID                    string
	InvitingTeam                 string // name
	// Acceptors are the acceptances' people and teams, in order.
	Acceptors []InviteAcceptor
	// RequestDenied is invite_request.date_denied (a request an admin
	// turned down).
	RequestDenied int64
}

// InviteAcceptor is an acceptance's user and team.
type InviteAcceptor struct {
	UserID, UserName, TeamID, TeamName string
	Status                             string // approval_status
	DateAccepted, DateInvalid          int64
}

type connectInviteRaw struct {
	actInvite
	DateCreated     int64  `json:"date_created"`
	DateLastUpdated int64  `json:"date_last_updated"`
	RecipientEmail  string `json:"recipient_email"`
	RecipientUserID string `json:"recipient_user_id"`
	Link            string `json:"link"`
	InviteRequest   *struct {
		DateDenied int64 `json:"date_denied"`
	} `json:"invite_request"`
}

// ConnectInvites is fetchAndResetAllConnectInvites (PC2X Y):
// connectInvites.list {invite_types: [channel, im], include_invite_requests:
// true}, the inbound and the outbound invites newest first (s32x Tc / aX
// sort by date_created).
func (a *App) ConnectInvites(ctx context.Context) (inbound, outbound []ConnectInvite, err error) {
	var res struct {
		Invites []json.RawMessage `json:"connect_invites"`
	}
	p := url.Values{"invite_types": {`["channel","im"]`}, "include_invite_requests": {"true"}}
	if err := a.C.Call(ctx, "connectInvites.list", p, &res); err != nil {
		return nil, nil, err
	}
	for _, raw := range res.Invites {
		ci, ok := parseConnectInvite(raw)
		if !ok {
			continue
		}
		if ci.Direction == "incoming" {
			inbound = append(inbound, ci)
		} else if ci.Direction == "outgoing" {
			outbound = append(outbound, ci)
		}
	}
	byNew := func(x, y ConnectInvite) int { return int(y.DateCreated - x.DateCreated) }
	slices.SortStableFunc(inbound, byNew)
	slices.SortStableFunc(outbound, byNew)
	return inbound, outbound, nil
}

func parseConnectInvite(raw json.RawMessage) (ConnectInvite, bool) {
	var r connectInviteRaw
	if json.Unmarshal(raw, &r) != nil || r.ID == "" {
		return ConnectInvite{}, false
	}
	var acc struct {
		Acceptances []struct {
			ApprovalStatus string `json:"approval_status"`
			DateAccepted   int64  `json:"date_accepted"`
			DateInvalid    int64  `json:"date_invalid"`
			AcceptingUser  struct {
				ID      string `json:"id"`
				Name    string `json:"name"`
				Profile struct {
					RealName string `json:"real_name"`
				} `json:"profile"`
			} `json:"accepting_user"`
			AcceptingTeam struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"accepting_team"`
		} `json:"acceptances"`
	}
	_ = json.Unmarshal(raw, &acc)
	var it ActivityItem
	actInviteFill(&it, &r.actInvite)
	ci := ConnectInvite{DateCreated: r.DateCreated, DateLastUpdated: r.DateLastUpdated, RecipientEmail: r.RecipientEmail,
		RecipientUserID: r.RecipientUserID, Link: r.Link, Message: it.Text, InviterID: it.Actor, InvitingTeam: it.Team}
	if it.Invite != nil {
		ci.ActivityInvite = *it.Invite
	} else {
		ci.ActivityInvite = ActivityInvite{ID: r.ID, Sig: r.Sig, Kind: r.InviteType, Direction: r.Direction, Status: r.Status, DateInvalid: r.DateInvalid}
	}
	ch := r.Channel
	if r.InvitePayload != nil && r.InvitePayload.Channel != nil {
		ch = r.InvitePayload.Channel
	}
	if ch != nil {
		ci.ChannelID, ci.ChannelName = ch.ID, ch.Name
	}
	if r.InviteRequest != nil {
		ci.RequestDenied = r.InviteRequest.DateDenied
	}
	for _, x := range acc.Acceptances {
		ci.Acceptors = append(ci.Acceptors, InviteAcceptor{UserID: x.AcceptingUser.ID, UserName: firstNonEmpty(x.AcceptingUser.Profile.RealName, x.AcceptingUser.Name),
			TeamID: x.AcceptingTeam.ID, TeamName: x.AcceptingTeam.Name, Status: x.ApprovalStatus, DateAccepted: x.DateAccepted, DateInvalid: x.DateInvalid})
	}
	return ci, true
}

// ConnectInvitesOutgoing is fetchAndResetOutboundConnectInvites (McFF p),
// after a revoke: connectInvites.list {direction: outgoing, invite_types:
// [channel, im], include_invite_requests: true}, newest first.
func (a *App) ConnectInvitesOutgoing(ctx context.Context) ([]ConnectInvite, error) {
	var res struct {
		Invites []json.RawMessage `json:"connect_invites"`
	}
	p := url.Values{"direction": {"outgoing"}, "invite_types": {`["channel","im"]`}, "include_invite_requests": {"true"},
		"reason": {"revoke-channel-invite-slack-connect-invite-actions"}}
	if err := a.C.Call(ctx, "connectInvites.list", p, &res); err != nil {
		return nil, err
	}
	var out []ConnectInvite
	for _, raw := range res.Invites {
		if ci, ok := parseConnectInvite(raw); ok && ci.Direction == "outgoing" {
			out = append(out, ci)
		}
	}
	slices.SortStableFunc(out, func(x, y ConnectInvite) int { return int(y.DateCreated - x.DateCreated) })
	return out, nil
}

// Connect invite actions for invites you sent (F5qO Eo).
const (
	InviteActCopyLink = "copy-link"
	InviteActRevoke   = "revoke"
)

// IsEmail is isEmailInvite (a recipient_email); a link invite has none.
func (ci *ConnectInvite) IsEmail() bool { return ci.RecipientEmail != "" }

// Active is isInviteActive: not expired, not revoked.
func (ci *ConnectInvite) Active(now time.Time) bool {
	return !ci.expired(now) && ci.Status != "revoked"
}

// HubActions is FI for any invite: Copy link (an active link you sent),
// Revoke (an active invite you sent: an email one only while nobody has
// accepted it or its acceptance still waits on approval), then the
// received invite's Ignore / Accept (ActivityInvite.InviteActions). An
// invite request (status requested) shows no Revoke (SlackConnectInviteActions).
func (ci *ConnectInvite) HubActions(self string, now time.Time) []string {
	var out []string
	if ci.Direction == "outgoing" {
		if !ci.IsEmail() && ci.Active(now) {
			out = append(out, InviteActCopyLink)
		}
		revoke := ci.Active(now)
		if ci.IsEmail() && revoke && len(ci.Acceptances) > 0 {
			a := ci.Acceptances[0]
			revoke = a.Status == "pending_approval" && !(len(ci.Acceptors) > 0 && ci.Acceptors[0].DateInvalid != 0 && ci.Acceptors[0].DateInvalid < now.Unix())
		}
		if revoke && ci.Status != "requested" {
			out = append(out, InviteActRevoke)
		}
		return out
	}
	return ci.ActivityInvite.InviteActions(self, now)
}

// RevokeConnectInvite is SlackConnectRevokeInviteModal's Revoke / Disable:
// conversations.revokeSharedInvite {invite_id} for a channel invite,
// sharedInvites.revoke {id} for a DM one.
func (a *App) RevokeConnectInvite(ctx context.Context, ci ConnectInvite) error {
	switch ci.Kind {
	case "channel":
		return a.C.Call(ctx, "conversations.revokeSharedInvite", url.Values{"invite_id": {ci.ID}, "reason": {"slack-connect-invite-revoke-invite-action"}}, nil)
	case "im":
		return a.C.Call(ctx, "sharedInvites.revoke", url.Values{"id": {ci.ID}, "reason": {"slack-connect-invite-revoke-invite-action"}}, nil)
	}
	return errors.New("revoke: not a channel or DM invite")
}

// ---- who sees what ----

// SCInviteRights is canUserInviteExternalMembersWithReason: OK, and why
// not (reasons, Slack's symbols' names).
type SCInviteRights struct {
	OK      bool
	Reasons []string
}

// Allowed-workspace reason (RRVt jM AllowedWorkspacePref).
const SCReasonAllowedWorkspacePref = "slack_connect_allowed_workspaces_pref"

// SCHub is what the hub's tabs show for the viewer, read once as the tab
// opens (Slack's selectors over its stores; the permissions from
// permissions/info, the DM eligibility from sharedInvites.canGetLink).
type SCHub struct {
	Invite SCInviteRights // RRVt lR
	// CreateChannel is 8cBv MM: CREATE_PUBLIC_CHANNEL or
	// CREATE_PRIVATE_CHANNEL somewhere, and not a guest.
	CreateChannel bool
	AdminContent  bool // 3jjM
	Paid          bool // xY2B O
	DMEligible    bool // g/ka u
	TeamName      string
}

// LoadSlackConnectHub reads SCHub: permissions/info for the channel
// permissions (as the create menu's fetchPermissions), then
// sharedInvites.canGetLink.
func (a *App) LoadSlackConnectHub(ctx context.Context) SCHub {
	h := SCHub{AdminContent: a.SlackConnectAdminContent(), Paid: a.SlackConnectPaidPlan(), TeamName: a.SlackConnectTeamName()}
	body := map[string]any{"user_id": a.S.SelfID(), "permissions": []string{"CREATE_PUBLIC_CHANNEL", "CREATE_PRIVATE_CHANNEL"}}
	if a.C.Creds.EnterpriseID != "" {
		body["team_ids"] = a.selfWorkspaceIDs()
	} else {
		body["as_admin"] = true
	}
	var r struct {
		Allowed        []string            `json:"allowed"`
		AllowedPerTeam map[string][]string `json:"allowed_per_team"`
	}
	if err := a.C.Edge(ctx, "permissions/info", body, &r); err == nil {
		has := len(r.Allowed) > 0
		for _, ps := range r.AllowedPerTeam {
			has = has || len(ps) > 0
		}
		h.CreateChannel = has && !a.viewerRestricted()
	}
	h.Invite = a.slackConnectInviteRights(h.CreateChannel)
	h.DMEligible = a.SlackConnectDMEligible(ctx)
	return h
}

// slackConnectInviteRights is RRVt lR for the viewer.
func (a *App) slackConnectInviteRights(canCreate bool) SCInviteRights {
	if a.viewerRestricted() {
		return SCInviteRights{Reasons: []string{"user_is_restricted"}}
	}
	grid := a.C.Creds.EnterpriseID != ""
	free := !grid && a.OnFreePlan()
	var rs []string
	if !canCreate {
		rs = append(rs, "create_channel_restricted_action")
	}
	if grid && !a.scAllowedWorkspaces() {
		rs = append(rs, SCReasonAllowedWorkspacePref)
	}
	if free {
		rs = append(rs, "free_team")
	}
	limited := a.scInvitePref("can_create_external_limited_invite", "who_can_create_external_limited_invite")
	unlimited := a.scInvitePref("can_create_slack_connect_channel_invite", "who_can_create_slack_connect_channel_invite")
	if !limited && !unlimited {
		rs = append(rs, "who_can_create_external_limited_invite_pref", "who_can_create_slack_connect_channel_pref")
	}
	if len(rs) > 0 {
		return SCInviteRights{OK: free, Reasons: rs}
	}
	return SCInviteRights{OK: true}
}

// scAllowedWorkspaces is isEveryTargetWorkspaceAllowedToHaveSharedChannels
// for the viewer's workspace: slack_connect_allowed_workspaces is ALL,
// ONLY_SELECTED with it, or ALL_EXCEPT_SELECTED without it.
func (a *App) scAllowedWorkspaces() bool {
	raw := a.EnterpriseLockedPref("slack_connect_allowed_workspaces")
	if len(raw) == 0 {
		return true
	}
	var p struct {
		Type  []string `json:"type"`
		Teams []string `json:"teams"`
	}
	if json.Unmarshal(raw, &p) != nil || len(p.Type) == 0 {
		return true
	}
	in := slices.Contains(p.Teams, a.C.Creds.Team)
	switch p.Type[0] {
	case "ONLY_SELECTED":
		return in
	case "ALL_EXCEPT_SELECTED":
		return !in
	}
	return true
}

// scInvitePref is bCT7 l6 / UIJR _d on a grid: the org's can_… pref, and
// the viewer in its who_can_… (JGki y).
func (a *App) scInvitePref(can, who string) bool {
	if a.C.Creds.EnterpriseID == "" {
		raw, _ := a.TeamPref(who)
		return a.teamPrefOn(can) && a.whoCanSelf(raw)
	}
	if string(a.EnterpriseLockedPref(can)) != "true" {
		return false
	}
	return a.whoCanSelf(a.EnterpriseLockedPref(who))
}

// whoCanSelf is JGki y's grid switch on a who_can pref: org admins and
// owners always pass the TOPLEVEL kinds, the SELECTED kind also passes
// the users it names, EVERYONE passes all.
func (a *App) whoCanSelf(raw json.RawMessage) bool {
	var p struct {
		Type []string `json:"type"`
		User []string `json:"user"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &p) != nil || len(p.Type) == 0 {
		return false
	}
	orgAdmin := a.SelfOrgAdmin()
	switch p.Type[0] {
	case "TOPLEVEL_ADMINS_AND_OWNERS", "ALL_ADMINS_AND_OWNERS":
		return orgAdmin
	case "TOPLEVEL_ADMINS_AND_OWNERS_AND_SELECTED":
		return orgAdmin || slices.Contains(p.User, a.S.SelfID())
	case "EVERYONE":
		return true
	}
	return false
}

// SelfOrgAdmin is ltQJ n5 || rr: an org admin or owner (on a grid), else
// a workspace admin or owner.
func (a *App) SelfOrgAdmin() bool {
	u, ok := a.S.User(a.S.SelfID())
	if !ok {
		return false
	}
	if a.C.Creds.EnterpriseID != "" {
		return u.EnterpriseUser != nil && (u.EnterpriseUser.IsAdmin || u.EnterpriseUser.IsOwner)
	}
	return u.IsAdmin || u.IsOwner
}

// SlackConnectAdminContent is shouldUserSeeSlackConnectAdminContent
// (3jjM): a paid team's admin (on a grid the org's).
func (a *App) SlackConnectAdminContent() bool {
	if a.C.Creds.EnterpriseID == "" && a.OnFreePlan() {
		return false
	}
	return a.SelfOrgAdmin()
}

// SlackConnectPaidPlan is xY2B O (DZwJ q): the team is on a paid plan.
func (a *App) SlackConnectPaidPlan() bool {
	return a.C.Creds.EnterpriseID != "" || !a.OnFreePlan()
}

// SlackConnectDMEligible is the DM button's I0Fa aJ:
// sharedInvites.canGetLink is_eligible (fetchSlackConnectDmEligibility;
// a failed call is not eligible).
func (a *App) SlackConnectDMEligible(ctx context.Context) bool {
	var res struct {
		IsEligible bool `json:"is_eligible"`
	}
	if err := a.C.Call(ctx, "sharedInvites.canGetLink", url.Values{"reason": {"slack-connect-dm-eligibility-cfm"}}, &res); err != nil {
		return false
	}
	return res.IsEligible
}

// SlackConnectTeamName is the hub's {selfOrgOrTeamName} (rUVB F1 on W5jp
// Yc): the org's name on a grid, else the team's.
func (a *App) SlackConnectTeamName() string {
	if u, ok := a.S.User(a.S.SelfID()); ok && u.EnterpriseUser != nil && u.EnterpriseUser.EnterpriseName != "" {
		return u.EnterpriseUser.EnterpriseName
	}
	a.S.RLock()
	defer a.S.RUnlock()
	return strings.TrimSpace(a.S.TeamName)
}
