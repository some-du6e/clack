package core

// The Directories' Workspaces, Organizations, Title and Location filters
// (Slack's SearchFilterWorkspace, SearchFilterOrgs, SearchFilterTitle and
// SearchFilterLocation; their params are XjMX getSearchAPIParams').

import (
	"context"
	"encoding/json"
	"net/url"
)

// facetRaw is one filter_suggestions entry (isSharedFacetSuggestion:
// value, facet_count, checked).
type facetRaw struct {
	Value      string `json:"value"`
	FacetCount int    `json:"facet_count"`
}

func facets(in []facetRaw) []Facet {
	var out []Facet
	seen := map[string]bool{}
	for _, f := range in {
		if f.Value == "" || seen[f.Value] {
			continue
		}
		seen[f.Value] = true
		out = append(out, Facet{Value: f.Value, Count: f.FacetCount})
	}
	return out
}

// EnterpriseTeams are the org's workspaces the viewer is in, the
// Workspaces filter's "Your workspaces" (6/pc n1: the known teams whose
// enterprise_id is the viewer's): the session's workspace, then the rest
// of the viewer's enterprise_user.teams. Names and icons come from Team
// once EnsureTeam has read them.
func (a *App) EnterpriseTeams() []string {
	a.S.RLock()
	cur := a.S.TeamID
	a.S.RUnlock()
	cur = firstNonEmpty(cur, a.C.Creds.Team)
	var out []string
	seen := map[string]bool{}
	add := func(id string) {
		if id != "" && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	add(cur)
	for _, id := range a.Self().Teams {
		if id != a.C.Creds.EnterpriseID { // the org itself isn't a workspace
			add(id)
		}
	}
	return out
}

// ConnectedOrgs are the organizations that share a channel with the
// viewer (users.slackConnect.orgList connection_type=member, limit 1000,
// sort count: XjMX Mr, the Organizations filter's suggestions), most
// shared first. Their names and icons are recorded for Team.
func (a *App) ConnectedOrgs(ctx context.Context) ([]string, error) {
	var res struct {
		Orgs []struct {
			TeamSummary struct {
				ID   string          `json:"id"`
				Name string          `json:"name"`
				Icon json.RawMessage `json:"icon"`
			} `json:"team_summary"`
		} `json:"orgs"`
	}
	p := url.Values{"connection_type": {"member"}, "limit": {"1000"}, "sort": {"count"}}
	if err := a.C.Call(ctx, "users.slackConnect.orgList", p, &res); err != nil {
		return nil, err
	}
	var out []string
	for _, o := range res.Orgs {
		t := o.TeamSummary
		if t.ID == "" {
			continue
		}
		a.setTeamName(t.ID, t.Name)
		if len(t.Icon) > 0 && string(t.Icon) != "null" {
			a.setTeamIcon(t.ID, t.Icon)
		}
		out = append(out, t.ID)
	}
	return out, nil
}
