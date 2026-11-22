package core

// The external organization page an External row opens
// (ExternalOrganizationView, chunk external-organization-view zJKG, build
// a09b625d90aa; Organization's use_organization_entities):
//
//   - use_org_info: fetchAndUpsertOrgInfo, users.slackConnect.orgInfo
//     {team_id, reason: use-org-info} (the team_summary upserted into the
//     teams store), then fetchChannelsById (pX: Flannel channels/info) for
//     its channel_ids;
//   - use_channel_search / use_member_search: the searcher (dfbP) with
//     allowEmptyQuery forced off, so with no query neither asks; with one,
//     the local store and Flannel: channels/search {filter "xws AND
//     shared", external_team} (getRemoteChannels with the channel options
//     merged onto the searcher's defaults: includeFromAnotherTeam,
//     onlyExternallyShared), users/search {filter "NOT deactivated AND NOT
//     org" ("NOT team" off a grid), external_team} (getRemoteMembers:
//     onlyExternal, onlyFromExternalTeamId; includeDeleted off, the
//     defaults' includeAgents keeping bots in), limit 25 each;
//   - the conversations, with no query the org's channels and the channel
//     search's (none) by id, with one the search's alone, split into the
//     viewer's IMs and MPIMs, the channels they're in (the membership
//     keyset) and the others.

import (
	"context"
	"net/url"
	"slices"
	"strings"

	"github.com/some-du6e/clack/internal/slack"
)

// ExternalOrgLimit is Organization's use_organization_entities limit.
const ExternalOrgLimit = 25

// ExternalOrgInfo is fetchAndUpsertOrgInfo: the team (noted for the hub's
// rows) and its channels, resolved through Flannel channels/info.
func (a *App) ExternalOrgInfo(ctx context.Context, teamID string) (ExternalTeam, []Conv, error) {
	var res struct {
		Team       externalTeamRaw `json:"team_summary"`
		ChannelIDs []string        `json:"channel_ids"`
	}
	if err := a.C.Call(ctx, "users.slackConnect.orgInfo", url.Values{"team_id": {teamID}, "reason": {"use-org-info"}}, &res); err != nil {
		return ExternalTeam{}, nil, err
	}
	t := res.Team.team()
	if t.ID != "" {
		a.noteExternalTeams([]ExternalTeam{t})
	}
	var need []string
	for _, id := range res.ChannelIDs {
		if c, ok := a.Conv(id); !ok || !named(c) {
			need = append(need, id)
		}
	}
	if len(need) > 0 {
		a.fetchChannelsByID(ctx, need)
	}
	var out []Conv
	for _, id := range res.ChannelIDs {
		if c, ok := a.Conv(id); ok && named(c) {
			out = append(out, c)
		}
	}
	return t, out, nil
}

// SearchExternalOrg is use_channel_search and use_member_search for
// query (none for an empty one): the store's matches first, then
// Flannel's, each once, up to limit.
func (a *App) SearchExternalOrg(ctx context.Context, teamID, query string, limit int) ([]Conv, []User, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil, nil
	}
	grid := a.C.Creds.EnterpriseID != ""
	convs := a.localOrgConvs(teamID, query)
	hs, cerr := a.C.SearchChannelsWith(ctx, query, slack.ChannelSearchOpts{Count: limit, Filter: "xws AND shared", ExternalTeam: teamID})
	// the hits the store doesn't hold are fetched (fetchChannelsById);
	// the ones it holds keep what it has (a leave since stays left)
	var hits, need []string
	for _, h := range hs {
		if slices.ContainsFunc(convs, func(c Conv) bool { return c.ID == h.ID }) {
			continue
		}
		hits = append(hits, h.ID)
		if c, ok := a.Conv(h.ID); !ok || !named(c) {
			need = append(need, h.ID)
		}
	}
	if len(need) > 0 {
		a.fetchChannelsByID(ctx, need)
	}
	for _, id := range hits {
		if c, ok := a.Conv(id); ok && named(c) && !c.IsArchived {
			convs = append(convs, c)
		}
	}
	if len(convs) > limit {
		convs = convs[:limit]
	}
	filter := "NOT deactivated AND NOT org"
	if !grid {
		filter = "NOT deactivated AND NOT team"
	}
	members := a.localOrgMembers(teamID, query)
	us, uerr := a.C.SearchUsersWith(ctx, query, slack.UserSearchOpts{Count: limit, Filter: filter, ExternalTeam: teamID})
	if uerr == nil {
		a.S.PutUsers(us)
	}
	for i := range us {
		u := a.userOf(&us[i])
		if u.ID == a.S.SelfID() || u.Deleted || u.IsBot || slices.ContainsFunc(members, func(x User) bool { return x.ID == u.ID }) {
			continue
		}
		members = append(members, u)
	}
	if len(members) > limit {
		members = members[:limit]
	}
	if cerr != nil && uerr != nil && len(convs) == 0 && len(members) == 0 {
		return nil, nil, cerr
	}
	return convs, members, nil
}

// localOrgConvs is the local channel search's onlyFromTeamId and
// onlyExternallyShared: the store's conversations shared with teamID (or
// an IM with one of its people) whose name has query, not archived.
func (a *App) localOrgConvs(teamID, query string) []Conv {
	q := strings.ToLower(query)
	var ids []string
	a.S.RLock()
	for id, c := range a.S.Convs {
		if c == nil || c.IsArchived {
			continue
		}
		in := slices.Contains(c.ConnectedTeamIDs, teamID)
		if !in && c.IsIM {
			if u, ok := a.S.Users[c.User]; ok && u.TeamID == teamID {
				in = true
			}
		}
		if in {
			ids = append(ids, id)
		}
	}
	a.S.RUnlock()
	slices.Sort(ids)
	var out []Conv
	for _, id := range ids {
		c, ok := a.Conv(id)
		if !ok {
			continue
		}
		name := c.Name
		if c.Kind == "im" {
			if u, ok := a.User(c.User); ok {
				name = u.Name + " " + u.Handle + " " + u.RealName
			}
		}
		if strings.Contains(strings.ToLower(name), q) {
			out = append(out, c)
		}
	}
	return out
}

// localOrgMembers is the local member search's onlyFromExternalTeamId:
// the store's people of teamID (not deleted, not the viewer) matching
// query.
func (a *App) localOrgMembers(teamID, query string) []User {
	q := strings.ToLower(query)
	self := a.S.SelfID()
	var ids []string
	a.S.RLock()
	for id, u := range a.S.Users {
		if u == nil || u.Deleted || u.IsBot || u.TeamID != teamID || id == self { // includeBots off
			continue
		}
		if strings.Contains(strings.ToLower(u.Name+" "+u.Profile.DisplayName+" "+u.RealName), q) {
			ids = append(ids, id)
		}
	}
	a.S.RUnlock()
	slices.Sort(ids)
	var out []User
	for _, id := range ids {
		if u, ok := a.User(id); ok {
			out = append(out, u)
		}
	}
	return out
}

// OrgChannelCounts is fetchMemberCountsForChannel as the org page's rows
// read it: edge users/counts {channel, as_admin} (people, the
// counts' member_count, and by_team, member_count_by_team).
func (a *App) OrgChannelCounts(ctx context.Context, ch string) (people int, byTeam map[string]int, err error) {
	var res struct {
		Counts struct {
			People  int            `json:"people"`
			Members int            `json:"members"`
			ByTeam  map[string]int `json:"by_team"`
		} `json:"counts"`
	}
	asAdmin := a.C.Creds.EnterpriseID == "" && a.SelfOrgAdmin() // !TZ (not a grid) && s$ (an admin)
	if err := a.C.Edge(ctx, "users/counts", map[string]any{"channel": ch, "as_admin": asAdmin}, &res); err != nil {
		return 0, nil, err
	}
	people = res.Counts.People
	if people == 0 {
		people = res.Counts.Members
	}
	return people, res.Counts.ByTeam, nil
}

// SelfTeamIDs is the viewer's workspace and org (el.ZT and el.jV), the
// teams Other channels' caption leaves out ("" ones dropped).
func (a *App) SelfTeamIDs() []string {
	t, e := a.S.TeamIDs()
	var out []string
	for _, id := range []string{t, e} {
		if id != "" {
			out = append(out, id)
		}
	}
	return out
}

// fetchChannelsByID is fetchChannelsById (pX): Flannel channels/info
// {check_membership, updated_ids}, 100 at a time; a channel the viewer
// isn't in is kept beside the sidebar's (the store's other
// conversations), so the page's rows can read it.
func (a *App) fetchChannelsByID(ctx context.Context, ids []string) {
	for i := 0; i < len(ids); i += 100 {
		j := min(i+100, len(ids))
		up := map[string]int{}
		for _, id := range ids[i:j] {
			up[id] = 0
		}
		var r struct {
			Results []slack.Conversation `json:"results"`
		}
		if err := a.C.Edge(ctx, "channels/info", map[string]any{"check_membership": true, "updated_ids": up}, &r); err != nil {
			continue
		}
		for _, c := range r.Results {
			if c.ID == "" {
				continue
			}
			// stored only when nothing holds it by now (checked under the
			// store's lock: a membership change while this read was out wins)
			a.S.AddExtConv(c)
		}
	}
}
