package fakeslack

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/some-du6e/clack/internal/slack"
)

// User groups come in two kinds, and Slack reads their members two ways
// (client-channel-user-group-view's BaseUserGroupView):
//
//   - a workspace group (team_id a T… id, like every group on the real
//     grid): usergroups.users.list lists its members, and search.enterprise's
//     belongs_to_usergroup_id finds nobody, as it does on the real grid
//     (2026-11-12: S09HH6NFVPZ has 29 members, the search 0);
//   - an org-level group (team_id the E… enterprise id): search.enterprise
//     finds its members, 100 a page; usergroups.users.list pages with limit.
//
// The generated groups S0FAKE%05d are workspace groups of user_count
// members (U0FAKE00001…); WorkspaceGroupID and OrgGroupID are fixed ones.
const (
	WorkspaceGroupID = "S0FAKEWKSP" // @snackcrew: a workspace group
	OrgGroupID       = "S0FAKEORGG" // @orgcrew: an org-level group
)

// fixtureGroup is one of the fixed groups.
type fixtureGroup struct {
	g        slack.Usergroup
	members  []string
	channels []string
}

func (s *Server) fixtureGroups() []fixtureGroup {
	return []fixtureGroup{
		{g: slack.Usergroup{ID: WorkspaceGroupID, Handle: "snackcrew", Name: "Snack crew", Description: "Synthetic workspace group",
			UserCount: 6, TeamID: TeamID},
			members: []string{SelfID, ProfileID, "U0FAKE00001", "U0FAKE00002", "U0FAKE00003", "U0FAKE00004"}},
		{g: slack.Usergroup{ID: OrgGroupID, Handle: "orgcrew", Name: "Org crew", Description: "Synthetic org-level group",
			UserCount: 4, TeamID: EnterpriseID},
			members: []string{SelfID, ProfileID, "U0FAKE00005", "U0FAKE00006", DeactivatedID}},
	}
}

// usergroupsAll is usergroups.list's groups: the generated ones, then the
// fixtures.
func (s *Server) usergroupsAll() []slack.Usergroup {
	var gs []slack.Usergroup
	for i := 0; i < s.Size.Usergroups; i++ {
		gs = append(gs, slack.Usergroup{ID: fmt.Sprintf("S0FAKE%05d", i), Handle: s.usergroupHandle(i),
			Name: title(s.words[(i*7)%len(s.words)]), Description: s.words[(i*3)%len(s.words)], UserCount: i % 50, TeamID: TeamID})
	}
	for _, f := range s.fixtureGroups() {
		gs = append(gs, f.g)
	}
	return gs
}

// usergroupMembers is group id's member ids (and whether it exists).
func (s *Server) usergroupMembers(id string) ([]string, slack.Usergroup, bool) {
	for _, f := range s.fixtureGroups() {
		if f.g.ID == id {
			return f.members, f.g, true
		}
	}
	for _, g := range s.usergroupsAll() {
		if g.ID != id {
			continue
		}
		var ids []string
		for k := 1; k <= g.UserCount && k < len(s.users); k++ {
			ids = append(ids, s.users[k].ID)
		}
		return ids, g, true
	}
	return nil, slack.Usergroup{}, false
}

func (s *Server) handleUsergroups(method string, f url.Values) (any, bool) {
	switch method {
	case "usergroups.list":
		return ok("usergroups", s.usergroupsAll()), true
	case "usergroups.search":
		// UserGroupDetails' refresh: {keyword: the handle, count: 1}
		kw := strings.ToLower(f.Get("keyword"))
		n, _ := strconv.Atoi(f.Get("count"))
		var out []map[string]any
		for _, g := range s.usergroupsAll() {
			if kw != "" && !strings.HasPrefix(g.Handle, kw) && !strings.Contains(strings.ToLower(g.Name), kw) {
				continue
			}
			out = append(out, map[string]any{"id": g.ID, "handle": g.Handle, "name": g.Name, "description": g.Description,
				"user_count": g.UserCount, "date_delete": 0, "team_id": g.TeamID, "is_usergroup": true, "is_subteam": true,
				"is_org_level": strings.HasPrefix(g.TeamID, "E"), "prefs": map[string]any{"channels": []string{}}})
			if n > 0 && len(out) >= n {
				break
			}
		}
		return ok("usergroups", out), true
	case "usergroups.users.list":
		ids, g, found := s.usergroupMembers(f.Get("usergroup"))
		if !found {
			return map[string]any{"ok": false, "error": "no_such_subteam"}, true
		}
		var live []string // include_disabled is false: deactivated accounts drop out
		for _, id := range ids {
			if u, ok := s.userByID(id); !ok || !u.Deleted {
				live = append(live, id)
			}
		}
		next := ""
		if lim, _ := strconv.Atoi(f.Get("limit")); lim > 0 && strings.HasPrefix(g.TeamID, "E") {
			from, _ := strconv.Atoi(f.Get("cursor"))
			from = min(from, len(live))
			to := min(from+lim, len(live))
			if to < len(live) {
				next = strconv.Itoa(to)
			}
			live = live[from:to]
		}
		return ok("users", live, "response_metadata", map[string]any{"next_cursor": next}), true
	case "search.enterprise":
		return s.searchGroupMembers(f)
	}
	return nil, false
}

// searchGroupMembers answers search.enterprise's searchUserGroupMembers
// query ({and: [is user, belongs_to_usergroup_id, fuzzy_with_email?]}):
// an org-level group's members, none for a workspace group. Other queries
// fall through.
func (s *Server) searchGroupMembers(f url.Values) (any, bool) {
	var q struct {
		Clauses []struct {
			Type  string `json:"type"`
			Value string `json:"value"`
		} `json:"clauses"`
	}
	if json.Unmarshal([]byte(f.Get("query")), &q) != nil {
		return nil, false
	}
	group, term := "", ""
	for _, c := range q.Clauses {
		switch c.Type {
		case "belongs_to_usergroup_id":
			group = c.Value
		case "fuzzy_with_email":
			term = strings.ToLower(c.Value)
		}
	}
	if group == "" {
		return nil, false
	}
	cursor := f.Get("cursor_mark")
	ids, g, found := s.usergroupMembers(group)
	if !found || !strings.HasPrefix(g.TeamID, "E") {
		return ok("items", []slack.User{}, "num_found", 0, "next_cursor_mark", cursor), true
	}
	var us []slack.User
	for _, id := range ids {
		u, ok := s.userByID(id)
		if !ok || u.Deleted && f.Get("include_deleted") == "0" {
			continue
		}
		if term != "" && !strings.Contains(strings.ToLower(u.Name+" "+u.RealName+" "+u.Profile.DisplayName), term) {
			continue
		}
		us = append(us, u)
	}
	sort.SliceStable(us, func(i, j int) bool { return strings.ToLower(u2name(us[i])) < strings.ToLower(u2name(us[j])) })
	count, _ := strconv.Atoi(f.Get("count"))
	if count <= 0 {
		count = 100
	}
	from := 0
	if cursor != "*" && cursor != "" {
		from, _ = strconv.Atoi(cursor)
	}
	from = min(from, len(us))
	to := min(from+count, len(us))
	next := cursor // Slack: the cursor sent back means done
	if to < len(us) {
		next = strconv.Itoa(to)
	}
	return ok("items", us[from:to], "num_found", len(us), "next_cursor_mark", next), true
}

func u2name(u slack.User) string {
	if u.Profile.RealName != "" {
		return u.Profile.RealName
	}
	return u.RealName
}
