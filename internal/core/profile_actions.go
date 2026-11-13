package core

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/some-du6e/clack/internal/slack"
)

// Calls behind the profile surfaces (popups/profile_*.go, app_card.go),
// each the one Slack's web client makes (build a84662d2d135, 2026-11).

// ChannelMembership is getChannelMembershipInfoForUsers' flannel call
// (client-boot 42rc fetchChannelMembershipForChannel): edge
// channels/membership {channel, users, as_admin} answers members and
// non_members. The add-to-channel dialog asks it for the picked channel.
func (a *App) ChannelMembership(ctx context.Context, ch string, users []string) (map[string]bool, error) {
	var r struct {
		Members    []string `json:"members"`
		NonMembers []string `json:"non_members"`
	}
	if err := a.C.Edge(ctx, "channels/membership", map[string]any{"channel": ch, "users": users, "as_admin": false}, &r); err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(users))
	for _, u := range r.Members {
		out[u] = true
	}
	for _, u := range r.NonMembers {
		out[u] = false
	}
	return out, nil
}

// AppScope is one entry of apps.permissions.internal.add's scope_info
// (OauthScopeInfo draws it: the short description, a dangerous one with
// the warning icon, the long one behind "Show more").
type AppScope struct {
	Scopes    []string `json:"scopes"`
	Short     string   `json:"short_description"`
	Long      string   `json:"long_description"`
	Dangerous bool     `json:"is_dangerous"`
}

// AddAppToChannel is fetchAppAddResource (boot-shared hRNA):
// apps.permissions.internal.add {app_user, channel, did_confirm,
// send_ephemeral_error_message}. Unconfirmed, Slack's addAppToChannel
// (XcD2) asks it for the scopes the app wants in ch and shows them in
// AppPermissionDialog; Allow sends it again confirmed, which adds the app.
func (a *App) AddAppToChannel(ctx context.Context, appUser, ch string, confirm bool) ([]AppScope, error) {
	var r struct {
		ScopeInfo []AppScope `json:"scope_info"`
	}
	p := url.Values{"app_user": {appUser}, "channel": {ch}, "did_confirm": {strconv.FormatBool(confirm)},
		"send_ephemeral_error_message": {"false"}}
	if err := a.C.Call(ctx, "apps.permissions.internal.add", p, &r); err != nil {
		return nil, err
	}
	return r.ScopeInfo, nil
}

// DenyAddAppToChannel is fetchAppDenyAddResource (hRNA): "Don't Allow" in
// AppPermissionDialog sends apps.permissions.internal.denyAdd {app_user,
// channel}.
func (a *App) DenyAddAppToChannel(ctx context.Context, appUser, ch string) error {
	return a.C.Call(ctx, "apps.permissions.internal.denyAdd", url.Values{"app_user": {appUser}, "channel": {ch}}, nil)
}

// VIPError is togglePriorityUserById's error toast text for a failed
// users.priority.add (boot-shared 77my PriorityErrorToast): "" when the
// error isn't an API answer (Slack toasts only errors with data).
func VIPError(err error, name string) string {
	var se *slack.Error
	if !errors.As(err, &se) {
		return ""
	}
	switch se.Code {
	case "user_not_found":
		return "User is not eligible to be added as VIP."
	case "too_many_vips":
		var b struct {
			Max json.Number `json:"max_vip_limit"`
		}
		_ = json.Unmarshal(se.Body, &b)
		return name + " could not be added to VIP. You can only have a maximum of " + b.Max.String() + " VIPs."
	}
	return "Hmm, something went wrong. Try again?"
}

// UsergroupProfile is what Slack's user group pane (UserGroupDetails)
// shows of a group.
type UsergroupProfile struct {
	Usergroup
	Disabled bool     // date_delete set: "<name> (Deactivated)", no tabs
	Channels []string // prefs.channels: the Channels tab
	// Groups are prefs.groups, the private default channels
	// (DefaultChannels lists both).
	Groups []string
	// AdditionalChannels are prefs.additional_channels (internal spaces).
	AdditionalChannels []string
}

// UsergroupProfile finds group id: the boot list first, then
// UserGroupDetails' refresh, usergroups.search {keyword: the handle,
// count: 1} (client-channel-user-group-view), which also brings the
// group's channels and state.
func (a *App) UsergroupProfile(ctx context.Context, id string) (UsergroupProfile, error) {
	var out UsergroupProfile
	out.Usergroup = a.StoredUsergroup(id)
	if out.Handle == "" {
		return out, errors.New("unknown user group " + id)
	}
	var res struct {
		Usergroups []struct {
			ID          string          `json:"id"`
			Handle      string          `json:"handle"`
			Name        string          `json:"name"`
			Description string          `json:"description"`
			UserCount   json.RawMessage `json:"user_count"`
			DateDelete  int64           `json:"date_delete"`
			TeamID      string          `json:"team_id"`
			AutoType    string          `json:"auto_type"`
			DateUpdate  int64           `json:"date_update"`
			IsSection   bool            `json:"is_section"`
			Prefs       usergroupPrefs  `json:"prefs"`
		} `json:"usergroups"`
	}
	if err := a.C.Call(ctx, "usergroups.search", url.Values{"keyword": {out.Handle}, "count": {"1"}}, &res); err != nil {
		return out, err
	}
	for _, g := range res.Usergroups {
		if g.ID != id {
			continue
		}
		team := firstNonEmpty(g.TeamID, out.TeamID)
		out.Usergroup = Usergroup{ID: g.ID, Handle: g.Handle, Name: g.Name, Description: g.Description, UserCount: int(looseInt(g.UserCount)),
			TeamID: team, AutoType: g.AutoType, IsSection: g.IsSection, Updated: g.DateUpdate}
		out.Disabled = g.DateDelete != 0
		out.Channels, out.Groups, out.AdditionalChannels = g.Prefs.Channels, g.Prefs.Groups, g.Prefs.AdditionalChannels
		// fetchAndUpsertUserGroup: the answer joins the store (bulkAddGroups)
		a.S.SetGroupChannels(g.ID, out.DefaultChannels())
	}
	return out, nil
}

// StoredUsergroup is group id from the boot list (zero when unknown).
func (a *App) StoredUsergroup(id string) Usergroup {
	a.S.RLock()
	defer a.S.RUnlock()
	for _, g := range a.S.Groups {
		if g.ID == id {
			return usergroupOf(g)
		}
	}
	return Usergroup{}
}

// UsergroupUsers is fetchAndUpsertUserGroupMembers (boot-shared YdXc),
// the members of a workspace group's User Group pane (the classic
// UserGroupDetails that client-channel-user-group-view's
// BaseUserGroupView picks when the group isn't OrgLevel), also what a
// @group mention's tooltip asks (useUserGroupMemberIdsForTooltip):
// usergroups.users.list {usergroup, team_id, limit: 100 for an org-level
// group only, cursor}. The ids replace the group's stored members (a
// later page appends) and come back as users sorted by
// getPrefCompliantMemberNameLowerCase (UserGroupDetails' sortMembers),
// with the next cursor ("" when done).
//
// search.enterprise's belongs_to_usergroup_id (UsergroupMembers) is the
// org groups' path only: on a workspace group it finds nobody.
func (a *App) UsergroupUsers(ctx context.Context, g Usergroup, cursor string) ([]User, string, error) {
	if g.TeamID == "" {
		return nil, "", errors.New("cannot fetch usergroup members for " + g.ID + " because there is no team_id or enterprise_id for that group")
	}
	p := url.Values{"usergroup": {g.ID}, "team_id": {g.TeamID}}
	if g.OrgLevel() {
		p.Set("limit", "100")
	}
	if cursor != "" {
		p.Set("cursor", cursor)
	}
	var res struct {
		Users []string `json:"users"`
		Meta  struct {
			NextCursor string `json:"next_cursor"`
		} `json:"response_metadata"`
	}
	if err := a.C.Call(ctx, "usergroups.users.list", p, &res); err != nil {
		return nil, "", err
	}
	a.S.SetGroupUsers(g.ID, res.Users, cursor != "")
	if cursor == "" {
		a.S.SetUsergroupCount(g.ID, len(res.Users)) // bulkAddMembersToGroup's user_count
	}
	a.EnsureUsers(ctx, res.Users)
	return a.groupMembers(res.Users, true), res.Meta.NextCursor, nil
}

// CachedUsergroupUsers is the group's members as last fetched (Slack's
// userGroups record keeps its users between visits), sorted like
// UsergroupUsers; ok is false before the first fetch.
func (a *App) CachedUsergroupUsers(id string, sorted bool) ([]User, bool) {
	ids, ok := a.S.GroupUsers(id)
	if !ok {
		return nil, false
	}
	return a.groupMembers(ids, sorted), true
}

// UsergroupTooltipIDs is useUserGroupMemberIdsForTooltip (boot-shared
// rZ8h U), what the composer's @group tooltip (TextyTooltipUserGroup)
// lists: the group's member ids as stored, in the order
// usergroups.users.list gave them (TooltipMemberNames doesn't sort), and
// fetch, whether the hook's effect fetches them on this hover
// (fetchAndUpsertUserGroupMembers, UsergroupUsers): the group is active
// (isUserGroupActive: known, no date_delete) and has no members stored
// and none fetched yet (hasFetchedUserGroupMembers).
func (a *App) UsergroupTooltipIDs(id string) (ids []string, fetch bool) {
	ids, fetched := a.S.GroupUsers(id)
	if len(ids) > 0 || fetched {
		return ids, false
	}
	a.S.RLock()
	defer a.S.RUnlock()
	for _, g := range a.S.Groups {
		if g.ID == id {
			return nil, g.DateDelete == 0
		}
	}
	return nil, false
}

// groupMembers resolves ids to the users the store knows, in order;
// sorted orders them by prefLowerName, stably (lodash sortBy).
func (a *App) groupMembers(ids []string, sorted bool) []User {
	a.S.RLock()
	realNames := a.S.Prefs.DisplayRealNames
	a.S.RUnlock()
	type member struct {
		u   User
		key string
	}
	ms := make([]member, 0, len(ids))
	for _, id := range ids {
		su, ok := a.S.User(id)
		if !ok {
			continue
		}
		ms = append(ms, member{a.userOf(su), prefLowerName(su, realNames)})
	}
	if sorted {
		sort.SliceStable(ms, func(i, j int) bool { return ms[i].key < ms[j].key })
	}
	out := make([]User, len(ms))
	for i := range ms {
		out[i] = ms[i].u
	}
	return out
}

// prefLowerName is getPrefCompliantMemberNameLowerCase (client-boot
// vUCu V3): _real_name_normalized_lc, or _display_name_normalized_lc
// when the viewer doesn't ask for real names and it is set.
func prefLowerName(u *slack.User, realNames bool) string {
	n := strings.ToLower(firstNonEmpty(u.Profile.RealNameNormalized, u.Profile.RealName, u.RealName))
	if !realNames {
		if d := strings.ToLower(firstNonEmpty(u.Profile.DisplayNameNormalized, u.Profile.DisplayName)); d != "" {
			n = d
		}
	}
	return n
}

// UsergroupMembers is searchAndUpsertUserGroupMembers (client-channel-
// user-group-view oXTE, "Fetch and upsert user group members via
// search.enterprise"), the members of an OrgLevel group's User Group
// pane (the newer UserGroupDetails): searchUserGroupMembers' people in
// group (is user, belongs_to_usergroup_id, plus fuzzy_with_email for the
// "Find members" term), 100 a page from cursor ("*" first), ascending,
// no deleted users or bots. Without a term the page replaces the group's
// stored members (a later page appends). It returns the members, the
// next cursor ("" when done) and num_found.
func (a *App) UsergroupMembers(ctx context.Context, group, term, cursor string) ([]User, string, int, error) {
	clauses := []map[string]string{{"type": "is", "value": "user"}, {"type": "belongs_to_usergroup_id", "value": group}}
	if term != "" {
		clauses = append(clauses, map[string]string{"type": "fuzzy_with_email", "value": term})
	}
	q, _ := json.Marshal(map[string]any{"type": "and", "clauses": clauses})
	first := cursor == ""
	if first {
		cursor = "*"
	}
	p := url.Values{"query": {string(q)}, "count": {"100"}, "cursor_mark": {cursor}, "include_deleted": {"0"},
		"include_bots": {"0"}, "include_dangling": {"0"}, "sort_dir": {"asc"}}
	var res struct {
		Items      []slack.User `json:"items"`
		NextCursor string       `json:"next_cursor_mark"`
		NumFound   int          `json:"num_found"`
	}
	if err := a.C.Call(ctx, "search.enterprise", p, &res); err != nil {
		return nil, "", 0, err
	}
	a.S.PutUsers(res.Items)
	out := make([]User, 0, len(res.Items))
	ids := make([]string, 0, len(res.Items))
	for i := range res.Items {
		if res.Items[i].ID == "" {
			continue
		}
		out = append(out, a.userOf(&res.Items[i]))
		ids = append(ids, res.Items[i].ID)
	}
	if term == "" {
		a.S.SetGroupUsers(group, ids, !first)
	}
	next := res.NextCursor
	if next == cursor {
		next = "" // Slack: a cursor equal to the one sent means done
	}
	return out, next, res.NumFound, nil
}
