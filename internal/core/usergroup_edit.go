package core

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/frecency"
	"github.com/some-du6e/clack/internal/slack"
)

// Who may edit a user group, as Slack's web client decides it (build
// 32bb3cc8205b, 2026-11-13; client-boot jiA8, QaSy, T63e, wY11, ZF7T,
// AcSl, qPNw):
//
//   - canEditUserGroupsInWorkspace(state, teamId) (jiA8 wC), what the
//     classic User Group pane (client-channel-user-group-view oXTE) asks
//     with the group's team_id (gMz2 $): false for a restricted viewer
//     (wY11 Ez: is_restricted, guests); else the featureAccess store's
//     user_groups__manage (ZF7T l, features.access.policies.list) and the
//     EDIT_USER_GROUPS or CREATE_USER_GROUPS permission allowed for that
//     team (T63e Vf: allowedForTeams has it).
//   - canCreateUserGroupsInWorkspace (Ew) is CREATE_USER_GROUPS alone,
//     canDeleteUserGroupsInWorkspace (rk) DELETE_USER_GROUPS, with the
//     same restricted and feature checks.
//   - The pane's ⋮ menu (UserGroupContextMenu) asks the any-workspace
//     forms instead (ql, Mm, fF: getPermissionForAnyWorkspace, isAllowed);
//     with one workspace they are the same answer.
//
// The permissions come from getPermissionWithAllowedTeams (QaSy oI →
// getWorkspacesUserCanDoActionOn): with the top-level team an enterprise
// (selfTeamIds.teamId is the org on a grid) the store's per-workspace
// answer, fetched by fetchPermissions (qPNw) as edge permissions/info
// {user_id, permissions, team_ids: the viewer's workspaces} and read from
// allowed_per_team; off a grid permissions/info {user_id, permissions,
// as_admin: true} (no channel, no team_ids), whose allowed list counts
// for the team itself. A failed call allows nothing (qPNw answers empty
// lists). Slack keeps the answers an hour (AcSl maxAge 3600).
//
// An org-level group (an E… team_id) gets the newer pane, which edits
// nothing: its ManageUserGroupLink offers the org dashboard's Groups page
// when canUserAccessGroupsInOrgDash (I4YG bC): CREATE_AND_EDIT_ORG_USER_GROUPS,
// EDIT_ORG_USER_GROUPS or DELETE_ORG_USER_GROUPS for the top-level team
// (QaSy tA: permissions/info with as_admin, the org's answer).

// The user group permissions (QxkB's enum keys) and feature.
const (
	PermEditUserGroups             = "EDIT_USER_GROUPS"
	PermCreateUserGroups           = "CREATE_USER_GROUPS"
	PermDeleteUserGroups           = "DELETE_USER_GROUPS"
	PermCreateAndEditOrgUserGroups = "CREATE_AND_EDIT_ORG_USER_GROUPS"
	PermEditOrgUserGroups          = "EDIT_ORG_USER_GROUPS"
	PermDeleteOrgUserGroups        = "DELETE_ORG_USER_GROUPS"
	// FeatureUserGroupsManage is ZF7T's featureAccess name.
	FeatureUserGroupsManage = "user_groups__manage"
)

// UsergroupRights is what the viewer may do to a team's user groups.
type UsergroupRights struct {
	Edit   bool // canEditUserGroupsInWorkspace (wC): EDIT or CREATE
	Create bool // canCreateUserGroupsInWorkspace (Ew)
	Delete bool // canDeleteUserGroupsInWorkspace (rk)
	// EditAny, CreateAny and DeleteAny are canEditUserGroups (ql),
	// canCreateUserGroups (Mm) and canDeleteUserGroups (fF): allowed in
	// any of the viewer's workspaces (the pane's ⋮ menu).
	EditAny, CreateAny, DeleteAny bool
	// OrgDash is canUserAccessGroupsInOrgDash (I4YG bC).
	OrgDash bool
}

// Editable is the classic pane's userGroupIsEditable without the group's
// state: any of the three workspace rights.
func (r UsergroupRights) Editable() bool { return r.Edit || r.Delete || r.Create }

type ugRightsCache struct {
	mu      sync.Mutex
	at      time.Time
	perTeam map[string]map[string]bool // team → allowed permissions
	org     map[string]bool            // the top-level team's (as_admin)
	known   bool
}

var ugRightsIndex sync.Map // *App → *ugRightsCache

func (a *App) ugRights() *ugRightsCache {
	v, _ := ugRightsIndex.LoadOrStore(a, &ugRightsCache{})
	return v.(*ugRightsCache)
}

// ugRightsMaxAge is AcSl's maxAge for a permission (3600 s).
const ugRightsMaxAge = time.Hour

// CachedUsergroupRights is UsergroupRights from the last answers when
// they are under an hour old (ok false: ask UsergroupRights).
func (a *App) CachedUsergroupRights(team string) (UsergroupRights, bool) {
	c := a.ugRights()
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.known || time.Since(c.at) > ugRightsMaxAge {
		return UsergroupRights{}, false
	}
	return a.ugRightsFrom(c, team), true
}

// UsergroupRights reads the viewer's user group rights for team (a
// group's team_id): the feature, then permissions/info for the three
// workspace permissions and the three org ones.
func (a *App) UsergroupRights(ctx context.Context, team string) UsergroupRights {
	if r, ok := a.CachedUsergroupRights(team); ok {
		return r
	}
	a.featuresNow(ctx)
	perms := []string{PermEditUserGroups, PermCreateUserGroups, PermDeleteUserGroups}
	perTeam := map[string]map[string]bool{}
	body := map[string]any{"user_id": a.S.SelfID(), "permissions": perms}
	top := a.C.Creds.Team
	grid := a.C.Creds.EnterpriseID != ""
	if grid {
		top = a.C.Creds.EnterpriseID
		body["team_ids"] = a.selfWorkspaceIDs()
	} else {
		body["as_admin"] = true // qPNw: neither teamIds nor a channel
	}
	var r struct {
		Allowed        []string            `json:"allowed"`
		AllowedPerTeam map[string][]string `json:"allowed_per_team"`
	}
	if err := a.C.Edge(ctx, "permissions/info", body, &r); err == nil {
		if grid {
			for t, ps := range r.AllowedPerTeam {
				perTeam[t] = setOf(ps)
			}
		} else if top != "" {
			perTeam[top] = setOf(r.Allowed)
		}
	}
	org := map[string]bool{}
	if grid { // canUserPerformActionOnOrg: the org's own answer
		ob := map[string]any{"user_id": a.S.SelfID(), "as_admin": true,
			"permissions": []string{PermCreateAndEditOrgUserGroups, PermEditOrgUserGroups, PermDeleteOrgUserGroups}}
		var or struct {
			Allowed []string `json:"allowed"`
		}
		if err := a.C.Edge(ctx, "permissions/info", ob, &or); err == nil {
			org = setOf(or.Allowed)
		}
	}
	c := a.ugRights()
	c.mu.Lock()
	c.perTeam, c.org, c.at, c.known = perTeam, org, time.Now(), true
	out := a.ugRightsFrom(c, team)
	c.mu.Unlock()
	return out
}

// selfWorkspaceIDs is selfTeamIds.internalWorkspaceIds: the workspace
// Clack signed in to.
func (a *App) selfWorkspaceIDs() []string {
	if t := a.C.Creds.Team; t != "" {
		return []string{t}
	}
	return []string{}
}

func setOf(ps []string) map[string]bool {
	m := make(map[string]bool, len(ps))
	for _, p := range ps {
		m[p] = true
	}
	return m
}

// ugRightsFrom applies the selectors to c's answers (c.mu held).
func (a *App) ugRightsFrom(c *ugRightsCache, team string) UsergroupRights {
	a.S.RLock()
	restricted := a.S.Self.IsRestricted
	a.S.RUnlock()
	if restricted || !a.FeatureAccess(FeatureUserGroupsManage) {
		return UsergroupRights{}
	}
	in := func(t, p string) bool { return c.perTeam[t][p] }
	anyTeam := func(p string) bool {
		for t := range c.perTeam {
			if in(t, p) {
				return true
			}
		}
		return false
	}
	return UsergroupRights{
		Edit:      team != "" && (in(team, PermEditUserGroups) || in(team, PermCreateUserGroups)),
		Create:    team != "" && in(team, PermCreateUserGroups),
		Delete:    team != "" && in(team, PermDeleteUserGroups),
		EditAny:   anyTeam(PermEditUserGroups) || anyTeam(PermCreateUserGroups),
		CreateAny: anyTeam(PermCreateUserGroups),
		DeleteAny: anyTeam(PermDeleteUserGroups),
		OrgDash:   c.org[PermCreateAndEditOrgUserGroups] || c.org[PermEditOrgUserGroups] || c.org[PermDeleteOrgUserGroups],
	}
}

// featuresNow makes sure the featureAccess store has been read (the boot
// read Slack does before any pane opens), waiting for it.
func (a *App) featuresNow(ctx context.Context) {
	f := a.features()
	for {
		f.mu.Lock()
		known, loading := f.known && time.Since(f.at) <= time.Hour, f.loading
		if !known && !loading {
			f.loading = true
		}
		f.mu.Unlock()
		if known {
			return
		}
		if !loading {
			a.loadFeatures(ctx)
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// ---- writes ----

// UsergroupUpdate is updateUserGroup's arguments (boot-shared 55+m).
type UsergroupUpdate struct {
	ID, TeamID   string
	Name, Handle string // sent when set
	Description  string // always sent ("" clears it)
	// Channels are the default channels, sent whenever non-nil (an
	// empty list clears them).
	Channels []string
	// AdditionalChannels go only under internal_spaces, when there are
	// any (isInternalSpacesEnabled, client-boot O2By Ow).
	AdditionalChannels []string
	EnableSection      bool // isUserGroupSectionEnabled
}

// UpdateUsergroup is updateUserGroup (boot-shared 55+m, "Upsert changes
// to a user group’s name, handle, description, default channels and
// members"): usergroups.update {usergroup, name?, handle?, channels?,
// team_id?, enable_section, additional_channels?, description}. Lists go
// comma-joined (getFormData appends a string array as is). Once it
// answers ok the description is stored (updateDescription); the rest
// comes back in subteam_updated.
func (a *App) UpdateUsergroup(ctx context.Context, u UsergroupUpdate) error {
	p := url.Values{"usergroup": {u.ID}}
	if u.Name != "" {
		p.Set("name", u.Name)
	}
	if u.Handle != "" {
		p.Set("handle", u.Handle)
	}
	if u.Channels != nil {
		p.Set("channels", strings.Join(u.Channels, ","))
	}
	if u.TeamID != "" {
		p.Set("team_id", u.TeamID)
	}
	p.Set("enable_section", strconv.FormatBool(u.EnableSection))
	if a.experiment("internal_spaces") == "on" && len(u.AdditionalChannels) > 0 {
		p.Set("additional_channels", strings.Join(u.AdditionalChannels, ","))
	}
	p.Set("description", u.Description)
	if err := a.C.Call(ctx, "usergroups.update", p, nil); err != nil {
		return err
	}
	a.S.SetUsergroupDescription(u.ID, u.Description)
	return nil
}

// SetUsergroupUsers is bulkEditUserGroupMembers (9CgL), Edit members'
// Save: usergroups.users.update {usergroup, users, team_id?}; the
// answer's usergroup.users replace the group's members
// (bulkAddMembersToGroup).
func (a *App) SetUsergroupUsers(ctx context.Context, g Usergroup, users []string) error {
	p := url.Values{"usergroup": {g.ID}, "users": {strings.Join(users, ",")}}
	if g.TeamID != "" {
		p.Set("team_id", g.TeamID)
	}
	var res struct {
		Usergroup struct {
			ID    string   `json:"id"`
			Users []string `json:"users"`
		} `json:"usergroup"`
	}
	if err := a.C.Call(ctx, "usergroups.users.update", p, &res); err != nil {
		return err
	}
	if res.Usergroup.ID != "" {
		a.S.SetGroupUsers(res.Usergroup.ID, res.Usergroup.Users, false)
		a.S.SetUsergroupCount(res.Usergroup.ID, len(res.Usergroup.Users))
	}
	return nil
}

// RemoveUsergroupMember is removeUserGroupMember (client-channel-user-
// group-view 7AXZ): the group's stored members less member, sent as
// usergroups.users.update {usergroup, users} (no team_id). A member not
// in the stored list sends nothing (Slack warns and resolves). Nothing
// is stored: subteam_members_changed follows. no_users_provided is the
// last member.
func (a *App) RemoveUsergroupMember(ctx context.Context, id, member string) error {
	ids, _ := a.S.GroupUsers(id)
	if !slices.Contains(ids, member) {
		return nil
	}
	rest := make([]string, 0, len(ids))
	for _, u := range ids {
		if u != member {
			rest = append(rest, u)
		}
	}
	return a.C.Call(ctx, "usergroups.users.update", url.Values{"usergroup": {id}, "users": {strings.Join(rest, ",")}}, nil)
}

// RemoveUsergroupChannel is removeUserGroupChannel (oXTE ru): the
// group's default channels less ch, through updateUserGroup with only
// the channels and the section flag, so usergroups.update also carries
// description "" (55+m sends description || ""), as Slack's does.
func (a *App) RemoveUsergroupChannel(ctx context.Context, g UsergroupProfile, ch string) error {
	chans := g.DefaultChannels()
	if !slices.Contains(chans, ch) {
		return nil
	}
	rest := make([]string, 0, len(chans))
	for _, c := range chans {
		if c != ch {
			rest = append(rest, c)
		}
	}
	return a.UpdateUsergroup(ctx, UsergroupUpdate{ID: g.ID, Channels: rest, EnableSection: g.IsSection})
}

// DisableUsergroup is disableAndUpdateUserGroup (oXTE eZ):
// usergroups.disable {usergroup, team_id?}; subteam_updated brings the
// group back with date_delete.
func (a *App) DisableUsergroup(ctx context.Context, id, team string) error {
	p := url.Values{"usergroup": {id}}
	if team != "" {
		p.Set("team_id", team)
	}
	return a.C.Call(ctx, "usergroups.disable", p, nil)
}

// EnableUsergroup is enableAndUpdateUserGroup (oXTE eY):
// usergroups.enable {usergroup, team_id?}.
func (a *App) EnableUsergroup(ctx context.Context, id, team string) error {
	p := url.Values{"usergroup": {id}}
	if team != "" {
		p.Set("team_id", team)
	}
	return a.C.Call(ctx, "usergroups.enable", p, nil)
}

// CheckUsergroupName is subteams.validateName {name, target_team}
// (boot-vendors TOPD), the server half of validateUserGroupName: the
// Slack error code ("" when the name is free).
func (a *App) CheckUsergroupName(ctx context.Context, name, team string) (string, error) {
	return a.ugCheck(ctx, "subteams.validateName", url.Values{"name": {name}, "target_team": {team}})
}

// CheckUsergroupHandle is subteams.validateHandle {handle, target_team}
// (boot-vendors Juv9).
func (a *App) CheckUsergroupHandle(ctx context.Context, handle, team string) (string, error) {
	return a.ugCheck(ctx, "subteams.validateHandle", url.Values{"handle": {handle}, "target_team": {team}})
}

func (a *App) ugCheck(ctx context.Context, method string, p url.Values) (string, error) {
	if p.Get("target_team") == "" {
		p.Del("target_team")
	}
	err := a.C.Call(ctx, method, p, nil)
	var se *slack.Error
	if errors.As(err, &se) {
		return se.Code, err
	}
	return "", err
}

// ---- the pickers' searches ----

// UsergroupMemberCandidates is Edit members' searcher (sk61
// EditUserGroupMembers): people on the viewer's team, Slackbot, bots,
// guests and deactivated people left out, 40 at most, frecency first.
// An empty query (allowEmptyQuery) lists the people frecency knows.
func (a *App) UsergroupMemberCandidates(ctx context.Context, query string) ([]User, error) {
	var us []User
	if strings.TrimSpace(query) == "" {
		for _, id := range a.frecencyIDs("", func(id string) bool { return id != "" && (id[0] == 'U' || id[0] == 'W') }) {
			if su, ok := a.S.User(id); ok {
				us = append(us, a.userOf(su))
			}
		}
	} else {
		res, err := a.SearchUsers(ctx, query, 40)
		if err != nil {
			return nil, err
		}
		us = frecencySortBy(a.FrecencyCache(), query, res, func(u User) string { return u.ID })
	}
	out := make([]User, 0, len(us))
	for _, u := range us {
		if u.ID == "USLACKBOT" || u.IsBot || u.Deleted || u.Guest || u.External {
			continue
		}
		out = append(out, u)
		if len(out) == 40 {
			break
		}
	}
	return out, nil
}

// UsergroupChannelCandidates is the default channel picker's searcher
// (Ou/6 UserGroupDefaultChannelPicker): public and private channels,
// none archived, no group DMs, 100 at most, frecency first.
func (a *App) UsergroupChannelCandidates(ctx context.Context, query string) ([]ChannelHit, error) {
	hs, err := a.SearchChannels(ctx, query, 100)
	if err != nil {
		return nil, err
	}
	hs = frecencySortBy(a.FrecencyCache(), query, hs, func(h ChannelHit) string { return h.ID })
	out := make([]ChannelHit, 0, len(hs))
	for _, h := range hs {
		if h.IsArchived || h.Kind == "mpim" || h.Kind == "im" {
			continue
		}
		out = append(out, h)
	}
	return out, nil
}

// frecencyIDs lists the frecency ids keep accepts, best first.
func (a *App) frecencyIDs(query string, keep func(string) bool) []string {
	fc := a.FrecencyCache()
	if fc == nil {
		return nil
	}
	now := time.Now()
	hits := fc.Hits(query, now)
	var ids []string
	for id := range hits {
		if keep(id) {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	sc := fc.ScoreItems(hits, ids, now, nil)
	frecency.SortItems(sc)
	out := make([]string, len(sc))
	for i, s := range sc {
		out[i] = s.ID
	}
	return out
}

// frecencySortBy orders xs by their frecency for query (ties keep the
// server's order).
func frecencySortBy[T any](fc *frecency.Cache, query string, xs []T, id func(T) string) []T {
	if fc == nil || len(xs) < 2 {
		return xs
	}
	now := time.Now()
	ids := make([]string, len(xs))
	for i, x := range xs {
		ids[i] = id(x)
	}
	sc := fc.ScoreItems(fc.Hits(query, now), ids, now, nil)
	frecency.SortItems(sc)
	out := make([]T, len(xs))
	for i, s := range sc {
		out[i] = xs[s.Index]
	}
	return out
}

// ---- the stored group ----

// DefaultChannels is getUserGroupDefaultChannels (PWhS UN): the
// channels, then the private ones.
func (g UsergroupProfile) DefaultChannels() []string {
	return append(append([]string{}, g.Channels...), g.Groups...)
}

// StoredUsergroupProfile is group id as the store has it now (the boot
// list, refreshed by usergroups.search and subteam_updated), with its
// default channels when known.
func (a *App) StoredUsergroupProfile(id string) (UsergroupProfile, bool) {
	a.S.RLock()
	var out UsergroupProfile
	found := false
	for _, g := range a.S.Groups {
		if g.ID == id {
			out.Usergroup, out.Disabled, found = usergroupOf(g), g.DateDelete != 0, true
		}
	}
	a.S.RUnlock()
	if chans, ok := a.S.GroupChannels(id); ok {
		out.Channels, out.Groups = splitGroupChannels(chans)
	}
	return out, found
}

// usergroupOf is a stored record as core's Usergroup.
func usergroupOf(g slack.Usergroup) Usergroup {
	return Usergroup{ID: g.ID, Handle: g.Handle, Name: g.Name, Description: g.Description, UserCount: g.UserCount,
		TeamID: g.TeamID, AutoType: g.AutoType, IsSection: g.IsSection, Updated: g.DateUpdate}
}

// splitGroupChannels undoes the store's channels-then-groups list: the
// private (G…) ids are prefs.groups.
func splitGroupChannels(ids []string) (chans, groups []string) {
	for _, id := range ids {
		if strings.HasPrefix(id, "G") {
			groups = append(groups, id)
		} else {
			chans = append(chans, id)
		}
	}
	return chans, groups
}

// usergroupPrefs is a group's prefs as usergroups.search and
// subteam_updated carry them.
type usergroupPrefs struct {
	Channels           []string `json:"channels"`
	Groups             []string `json:"groups"`
	AdditionalChannels []string `json:"additional_channels"`
}

// noteUsergroupEvent is subteamUpdated / subteamCreated (boot-render):
// the event's group replaces the stored one (subteam_updated only a
// known one), keeping its members, and its prefs give the default
// channels.
func (a *App) noteUsergroupEvent(raw json.RawMessage, created bool) {
	var e struct {
		Subteam struct {
			slack.Usergroup
			Prefs *usergroupPrefs `json:"prefs"`
		} `json:"subteam"`
	}
	if json.Unmarshal(raw, &e) != nil || e.Subteam.ID == "" {
		return
	}
	if !created && (a.StoredUsergroup(e.Subteam.ID) == Usergroup{}) {
		return // subteamUpdated skips a group it doesn't know
	}
	a.S.PutUsergroup(e.Subteam.Usergroup)
	if p := e.Subteam.Prefs; p != nil {
		a.S.SetGroupChannels(e.Subteam.ID, append(append([]string{}, p.Channels...), p.Groups...))
	}
}

// The experiment the update reads (TestExperimentReadsListed).
func init() {
	experimentReads["internal_spaces"] = on
}
