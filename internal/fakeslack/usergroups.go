package fakeslack

import (
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

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
// members (U0FAKE00001…); WorkspaceGroupID and OrgGroupID are fixed ones,
// and AdminsGroupID is the locked group Slack generates for a
// workspace's admins (auto_type "admin": its name, handle and members
// can't be edited).
//
// Editing (the User Group pane's editor actions): usergroups.update,
// usergroups.users.update, usergroups.disable and usergroups.enable
// change the fake's copy of a group and push subteam_updated (and
// subteam_members_changed for members), as Slack's server does;
// subteams.validateName and subteams.validateHandle check the names.
// Whether the viewer may edit is UsergroupEditor: permissions/info then
// allows EDIT_USER_GROUPS, CREATE_USER_GROUPS and DELETE_USER_GROUPS on
// the workspace (a non-editor viewer gets none of them, as before), and
// features.access.policies.list has user_groups__manage on.
const (
	WorkspaceGroupID = "S0FAKEWKSP" // @snackcrew: a workspace group
	OrgGroupID       = "S0FAKEORGG" // @orgcrew: an org-level group
	AdminsGroupID    = "S0FAKEADMN" // @admins: the auto-generated (locked) admins group
)

// fixtureGroup is one of the fixed groups.
type fixtureGroup struct {
	g        slack.Usergroup
	members  []string
	channels []string
}

// UsergroupFixture is a user group a workspace built on the fake (the
// demo) brings instead of the fixed ones (SetUsergroups).
type UsergroupFixture struct {
	Group    slack.Usergroup
	Members  []string
	Channels []string // default channels (prefs.channels / prefs.groups)
}

// SetUsergroups replaces the fixed groups (snackcrew, orgcrew, admins)
// with gs; nil goes back to them. Set it before the calls.
func (s *Server) SetUsergroups(gs []UsergroupFixture) {
	e := s.ugEdits()
	e.mu.Lock()
	defer e.mu.Unlock()
	e.own = nil
	for _, g := range gs {
		g.Group.UserCount = len(g.Members)
		e.own = append(e.own, fixtureGroup{g: g.Group, members: slices.Clone(g.Members), channels: slices.Clone(g.Channels)})
	}
	e.ownSet = gs != nil
}

func (s *Server) fixtureGroups() []fixtureGroup {
	e := s.ugEdits()
	e.mu.Lock()
	own, set := e.own, e.ownSet
	e.mu.Unlock()
	if set {
		return own
	}
	return []fixtureGroup{
		{g: slack.Usergroup{ID: WorkspaceGroupID, Handle: "snackcrew", Name: "Snack crew", Description: "Synthetic workspace group",
			UserCount: 6, TeamID: TeamID},
			members: []string{SelfID, ProfileID, "U0FAKE00001", "U0FAKE00002", "U0FAKE00003", "U0FAKE00004"}},
		{g: slack.Usergroup{ID: OrgGroupID, Handle: "orgcrew", Name: "Org crew", Description: "Synthetic org-level group",
			UserCount: 4, TeamID: EnterpriseID},
			members: []string{SelfID, ProfileID, "U0FAKE00005", "U0FAKE00006", DeactivatedID}},
		{g: slack.Usergroup{ID: AdminsGroupID, Handle: "admins", Name: "Admins", Description: "",
			UserCount: 2, TeamID: TeamID, AutoType: "admin"},
			members: []string{SelfID, ProfileID}},
	}
}

// ugState is the fake's copy of a group once something edited it.
type ugState struct {
	g        slack.Usergroup
	members  []string
	channels []string // prefs.channels
	groups   []string // prefs.groups (private channels)
	updated  int64    // date_update
}

type ugEdits struct {
	mu     sync.Mutex
	m      map[string]*ugState
	own    []fixtureGroup // SetUsergroups
	ownSet bool
}

var ugEditIndex sync.Map // *Server → *ugEdits

func (s *Server) ugEdits() *ugEdits {
	v, _ := ugEditIndex.LoadOrStore(s, &ugEdits{m: map[string]*ugState{}})
	return v.(*ugEdits)
}

// usergroupState is group id as it stands now (a copy; ok false when
// there is no such group).
func (s *Server) usergroupState(id string) (ugState, bool) {
	e := s.ugEdits()
	e.mu.Lock()
	st, ok := e.m[id]
	if ok {
		out := *st
		out.members = slices.Clone(st.members)
		out.channels, out.groups = slices.Clone(st.channels), slices.Clone(st.groups)
		e.mu.Unlock()
		return out, true
	}
	e.mu.Unlock()
	for _, f := range s.fixtureGroups() {
		if f.g.ID == id {
			return ugState{g: f.g, members: f.members, channels: f.channels}, true
		}
	}
	for i := 0; i < s.Size.Usergroups; i++ {
		g := s.generatedGroup(i)
		if g.ID != id {
			continue
		}
		var ids []string
		for k := 1; k <= g.UserCount && k < len(s.users); k++ {
			ids = append(ids, s.users[k].ID)
		}
		return ugState{g: g, members: ids}, true
	}
	return ugState{}, false
}

// editUsergroup applies fn to group id's copy (made on first edit).
func (s *Server) editUsergroup(id string, fn func(st *ugState)) (ugState, bool) {
	cur, ok := s.usergroupState(id)
	if !ok {
		return ugState{}, false
	}
	e := s.ugEdits()
	e.mu.Lock()
	st := e.m[id]
	if st == nil {
		st = &cur
		e.m[id] = st
	}
	fn(st)
	st.updated = max(time.Now().Unix(), st.updated+1) // each change gets a new date_update
	st.g.DateUpdate = st.updated
	st.g.UserCount = len(s.liveMembers(st.members))
	e.mu.Unlock()
	out, _ := s.usergroupState(id)
	return out, true
}

func (s *Server) generatedGroup(i int) slack.Usergroup {
	return slack.Usergroup{ID: fmt.Sprintf("S0FAKE%05d", i), Handle: s.usergroupHandle(i),
		Name: title(s.words[(i*7)%len(s.words)]), Description: s.words[(i*3)%len(s.words)], UserCount: i % 50, TeamID: TeamID}
}

// usergroupsAll is every group (the generated ones, then the fixtures)
// as it stands now, disabled ones included.
func (s *Server) usergroupsAll() []slack.Usergroup {
	var ids []string
	for i := 0; i < s.Size.Usergroups; i++ {
		ids = append(ids, fmt.Sprintf("S0FAKE%05d", i))
	}
	for _, f := range s.fixtureGroups() {
		ids = append(ids, f.g.ID)
	}
	gs := make([]slack.Usergroup, 0, len(ids))
	for _, id := range ids {
		if st, ok := s.usergroupState(id); ok {
			gs = append(gs, st.g)
		}
	}
	return gs
}

// usergroupMembers is group id's member ids (and whether it exists).
func (s *Server) usergroupMembers(id string) ([]string, slack.Usergroup, bool) {
	st, ok := s.usergroupState(id)
	return st.members, st.g, ok
}

// liveMembers drops deactivated accounts (include_disabled false).
func (s *Server) liveMembers(ids []string) []string {
	var live []string
	for _, id := range ids {
		if u, ok := s.userByID(id); !ok || !u.Deleted {
			live = append(live, id)
		}
	}
	return live
}

// usergroupJSON is a group as usergroups.search, usergroups.update and
// subteam_updated carry it.
func (s *Server) usergroupJSON(st ugState, withUsers bool) map[string]any {
	g := st.g
	m := map[string]any{"id": g.ID, "handle": g.Handle, "name": g.Name, "description": g.Description,
		"user_count": g.UserCount, "date_delete": g.DateDelete, "date_update": st.updated, "team_id": g.TeamID, "is_usergroup": true, "is_subteam": true,
		"is_org_level": strings.HasPrefix(g.TeamID, "E"), "is_section": g.IsSection, "auto_type": nilIfEmpty(g.AutoType),
		"prefs": map[string]any{"channels": nonNil(st.channels), "groups": nonNil(st.groups)}}
	if withUsers {
		m["users"] = nonNil(s.liveMembers(st.members))
	}
	return m
}

func nonNil(ids []string) []string {
	if ids == nil {
		return []string{}
	}
	return ids
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// SetUsergroupChannels gives group id default channels (tests: the
// Channels tab, its ⋮ menu); private (G… or a private fake channel) ids
// go to prefs.groups.
func (s *Server) SetUsergroupChannels(id string, chans []string) {
	s.editUsergroup(id, func(st *ugState) { st.channels, st.groups = splitPrefs(chans) })
}

// splitPrefs files ids under prefs.channels or prefs.groups.
func splitPrefs(ids []string) (chans, groups []string) {
	chans, groups = []string{}, []string{}
	for _, id := range ids {
		if id == "" {
			continue
		}
		if strings.HasPrefix(id, "G") {
			groups = append(groups, id)
		} else {
			chans = append(chans, id)
		}
	}
	return chans, groups
}

// UsergroupNow is group id as it stands (tests).
func (s *Server) UsergroupNow(id string) (g slack.Usergroup, members, channels []string) {
	st, _ := s.usergroupState(id)
	return st.g, st.members, append(st.channels, st.groups...)
}

func (s *Server) pushSubteamUpdated(st ugState) {
	s.emit(map[string]any{"type": "subteam_updated", "subteam": s.usergroupJSON(st, true), "event_ts": fmt.Sprintf("%d.000300", time.Now().Unix())})
}

// usergroupPerms answers permissions/info for the user group
// permissions (nil, false for any other ask).
func (s *Server) usergroupPerms(edge map[string]json.RawMessage) (any, bool) {
	var perms []string
	_ = json.Unmarshal(edge["permissions"], &perms)
	ws := []string{"EDIT_USER_GROUPS", "CREATE_USER_GROUPS", "DELETE_USER_GROUPS"}
	org := []string{"CREATE_AND_EDIT_ORG_USER_GROUPS", "EDIT_ORG_USER_GROUPS", "DELETE_ORG_USER_GROUPS"}
	mine := func(set []string) bool {
		for _, p := range perms {
			if slices.Contains(set, p) {
				return true
			}
		}
		return false
	}
	switch {
	case mine(ws):
		var teams []string
		_ = json.Unmarshal(edge["team_ids"], &teams)
		per, allowed := map[string]any{}, []string{}
		if s.UsergroupEditor {
			allowed = slices.DeleteFunc(slices.Clone(perms), func(p string) bool { return !slices.Contains(ws, p) })
			for _, t := range teams {
				per[t] = allowed
			}
		}
		return ok("allowed", allowed, "allowed_per_team", per), true
	case mine(org):
		allowed := []string{}
		if s.UsergroupOrgAdmin {
			allowed = slices.DeleteFunc(slices.Clone(perms), func(p string) bool { return !slices.Contains(org, p) })
		}
		return ok("allowed", allowed, "allowed_per_team", map[string]any{}), true
	}
	return nil, false
}

func (s *Server) handleUsergroups(method string, f url.Values, edge map[string]json.RawMessage) (any, bool) {
	switch method {
	case "edge/permissions/info":
		return s.usergroupPerms(edge)
	case "features.access.policies.list": // user_groups__manage (ZF7T): the pane's editor actions
		return ok("feature_policies", map[string]any{"user_groups__manage": map[string]any{"is_enabled": true}}), true
	case "usergroups.list":
		var live []slack.Usergroup // include_disabled false
		for _, g := range s.usergroupsAll() {
			if g.DateDelete == 0 || f.Get("include_disabled") == "true" {
				live = append(live, g)
			}
		}
		return ok("usergroups", live), true
	case "usergroups.search":
		// UserGroupDetails' refresh: {keyword: the handle, count: 1}
		kw := strings.ToLower(f.Get("keyword"))
		n, _ := strconv.Atoi(f.Get("count"))
		var out []map[string]any
		for _, g := range s.usergroupsAll() {
			if kw != "" && !strings.HasPrefix(g.Handle, kw) && !strings.Contains(strings.ToLower(g.Name), kw) {
				continue
			}
			st, _ := s.usergroupState(g.ID)
			out = append(out, s.usergroupJSON(st, false))
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
		live := s.liveMembers(ids) // include_disabled is false: deactivated accounts drop out
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
	case "usergroups.update":
		return s.updateUsergroup(f), true
	case "usergroups.users.update":
		return s.updateUsergroupUsers(f), true
	case "usergroups.disable", "usergroups.enable":
		st, found := s.editUsergroup(f.Get("usergroup"), func(st *ugState) {
			st.g.DateDelete = 0
			if method == "usergroups.disable" {
				st.g.DateDelete = time.Now().Unix()
			}
		})
		if !found {
			return map[string]any{"ok": false, "error": "no_such_subteam"}, true
		}
		s.pushSubteamUpdated(st)
		return ok("usergroup", s.usergroupJSON(st, true)), true
	case "subteams.validateName":
		name := strings.ToLower(strings.TrimSpace(f.Get("name")))
		for _, g := range s.usergroupsAll() {
			if strings.ToLower(g.Name) == name {
				return map[string]any{"ok": false, "error": "name_already_exists"}, true
			}
		}
		return ok(), true
	case "subteams.validateHandle":
		h := f.Get("handle")
		for _, r := range h {
			if r > unicode.MaxASCII {
				return map[string]any{"ok": false, "error": "bad_handle"}, true
			}
		}
		for _, g := range s.usergroupsAll() {
			if g.Handle == h {
				return map[string]any{"ok": false, "error": "handle_already_exists"}, true
			}
		}
		for _, u := range s.users {
			if u.Name == h {
				return map[string]any{"ok": false, "error": "handle_already_exists"}, true
			}
		}
		return ok(), true
	}
	return nil, false
}

// updateUsergroup is usergroups.update: name, handle, description,
// channels (comma-joined; prefs.channels and prefs.groups) and
// enable_section, then subteam_updated.
func (s *Server) updateUsergroup(f url.Values) any {
	id := f.Get("usergroup")
	cur, found := s.usergroupState(id)
	if !found {
		return map[string]any{"ok": false, "error": "no_such_subteam"}
	}
	if cur.g.AutoType != "" && (f.Has("name") && f.Get("name") != cur.g.Name || f.Has("handle") && f.Get("handle") != cur.g.Handle) {
		return map[string]any{"ok": false, "error": "cannot_edit_auto_generated"}
	}
	var chans []string
	if f.Has("channels") {
		for _, c := range strings.Split(f.Get("channels"), ",") {
			if c != "" {
				chans = append(chans, c)
			}
		}
		if len(chans) > 100 {
			return map[string]any{"ok": false, "error": "too_many_linked_channels"}
		}
	}
	st, _ := s.editUsergroup(id, func(st *ugState) {
		if v := f.Get("name"); v != "" {
			st.g.Name = v
		}
		if v := f.Get("handle"); v != "" {
			st.g.Handle = v
		}
		if f.Has("description") {
			st.g.Description = f.Get("description")
		}
		if f.Has("channels") {
			st.channels, st.groups = s.splitChannelPrefs(chans)
		}
		if f.Has("enable_section") {
			st.g.IsSection = f.Get("enable_section") == "true"
		}
	})
	s.pushSubteamUpdated(st)
	return ok("usergroup", s.usergroupJSON(st, true))
}

// splitChannelPrefs files channel ids under prefs.channels or
// prefs.groups (private channels), as Slack keeps them.
func (s *Server) splitChannelPrefs(ids []string) (chans, groups []string) {
	chans, groups = []string{}, []string{}
	for _, id := range ids {
		if strings.HasPrefix(id, "G") || s.channelIsPrivate(id) {
			groups = append(groups, id)
		} else {
			chans = append(chans, id)
		}
	}
	return chans, groups
}

func (s *Server) channelIsPrivate(id string) bool {
	c, found := s.conv(id)
	return found && c.IsPrivate
}

// updateUsergroupUsers is usergroups.users.update: users (comma-joined)
// replace the members (no_users_provided for none), then
// subteam_members_changed and subteam_updated.
func (s *Server) updateUsergroupUsers(f url.Values) any {
	id := f.Get("usergroup")
	var users []string
	for _, u := range strings.Split(f.Get("users"), ",") {
		if u != "" && !slices.Contains(users, u) {
			users = append(users, u)
		}
	}
	cur, found := s.usergroupState(id)
	if !found {
		return map[string]any{"ok": false, "error": "no_such_subteam"}
	}
	if len(users) == 0 {
		return map[string]any{"ok": false, "error": "no_users_provided"}
	}
	if cur.g.AutoType != "" {
		return map[string]any{"ok": false, "error": "cannot_edit_auto_generated"}
	}
	var added, removed []string
	for _, u := range users {
		if !slices.Contains(cur.members, u) {
			added = append(added, u)
		}
	}
	for _, u := range cur.members {
		if !slices.Contains(users, u) {
			removed = append(removed, u)
		}
	}
	st, _ := s.editUsergroup(id, func(st *ugState) { st.members = users })
	now := time.Now().Unix()
	s.emit(map[string]any{"type": "subteam_members_changed", "subteam_id": id, "team_id": st.g.TeamID,
		"added_users": nonNil(added), "added_users_count": strconv.Itoa(len(added)),
		"removed_users": nonNil(removed), "removed_users_count": strconv.Itoa(len(removed)),
		"date_previous_update": cur.updated, "date_update": now, "event_ts": fmt.Sprintf("%d.000400", now)})
	s.pushSubteamUpdated(st)
	return ok("usergroup", s.usergroupJSON(st, true))
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
