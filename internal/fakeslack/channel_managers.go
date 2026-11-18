package fakeslack

import (
	"net/url"
	"slices"
)

// The channel manager writes Slack's channel details roster sends
// (core/channel_managers_edit.go): admin.roles.addMembers and
// admin.roles.removeMembers {role_id Rl0A, role_scopes, user_ids,
// reason}, the lists comma-joined. They change ChannelManagers, which
// admin.roles.entity.listAssignments answers from, and ManagerWrites
// lists them in order.
//
// Answers, as the methods' error enums (boot-vendors Fnkp, bUDL) name
// them: invalid_role_id for any role but Rl0A, no_valid_scopes /
// no_valid_role_scopes without a scope, no_valid_users without users,
// failed_for_some_users (invalid_user_ids) for an add naming a
// deactivated member.

// channelManagerRole is the channel manager role's id.
const channelManagerRole = "Rl0A"

// ManagerWrite is one channel manager write the fake answered.
type ManagerWrite struct {
	Method, RoleID, Reason string
	Scopes, Users          []string
}

// ManagerWrites returns the channel manager writes so far.
func (s *Server) ManagerWrites() []ManagerWrite {
	s.convMu.Lock()
	defer s.convMu.Unlock()
	return slices.Clone(s.managerWrites)
}

// Managers returns ch's channel managers as the fake holds them.
func (s *Server) Managers(ch string) []string {
	s.convMu.Lock()
	defer s.convMu.Unlock()
	return slices.Clone(s.ChannelManagers[ch])
}

// handleChannelManagers answers the two writes (handled=false for any
// other method).
func (s *Server) handleChannelManagers(method string, f url.Values) (any, bool) {
	add := method == "admin.roles.addMembers"
	if !add && method != "admin.roles.removeMembers" {
		return nil, false
	}
	w := ManagerWrite{Method: method, RoleID: f.Get("role_id"), Reason: f.Get("reason"),
		Scopes: splitList(f.Get("role_scopes")), Users: splitList(f.Get("user_ids"))} // huddles_page.go
	var invalid []string
	if add {
		for _, u := range w.Users {
			if m, found := s.userByID(u); !found || m.Deleted {
				invalid = append(invalid, u)
			}
		}
	}
	s.convMu.Lock()
	defer s.convMu.Unlock()
	s.managerWrites = append(s.managerWrites, w)
	switch {
	case w.RoleID != channelManagerRole:
		return fail("invalid_role_id"), true
	case len(w.Scopes) == 0 && add:
		return fail("no_valid_scopes"), true
	case len(w.Scopes) == 0:
		return fail("no_valid_role_scopes"), true
	case len(w.Users) == 0:
		return fail("no_valid_users"), true
	}
	if s.ChannelManagers == nil {
		s.ChannelManagers = map[string][]string{}
	}
	for _, ch := range w.Scopes {
		cur := slices.Clone(s.ChannelManagers[ch])
		for _, u := range w.Users {
			switch {
			case slices.Contains(invalid, u):
			case add && !slices.Contains(cur, u):
				cur = append(cur, u)
			case !add:
				cur = slices.DeleteFunc(cur, func(m string) bool { return m == u })
			}
		}
		s.ChannelManagers[ch] = cur
	}
	if len(invalid) > 0 {
		return map[string]any{"ok": false, "error": "failed_for_some_users", "invalid_user_ids": invalid}, true
	}
	return ok(), true
}
