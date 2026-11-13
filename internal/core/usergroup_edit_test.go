package core

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// ugEnv is a client on the fake with its usergroups.list loaded, the
// fake's realtime events fed straight in, and every call recorded.
type ugEnv struct {
	fs    *fakeslack.Server
	a     *App
	mu    sync.Mutex
	forms map[string][]url.Values
	edges map[string][]map[string]json.RawMessage
}

func newUGEnv(t *testing.T, editor bool) (*ugEnv, context.Context) {
	t.Helper()
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	fs.UsergroupEditor = editor
	e := &ugEnv{fs: fs, a: New(slack.New(fs.Creds()), store.New("")), forms: map[string][]url.Values{}, edges: map[string][]map[string]json.RawMessage{}}
	fs.Emit = func(ev map[string]any) {
		b, _ := json.Marshal(ev)
		typ, _ := ev["type"].(string)
		e.a.Inject(slack.Event{Type: typ, Raw: b})
	}
	fs.SetHook(func(method string, form url.Values) (any, bool) {
		e.mu.Lock()
		e.forms[method] = append(e.forms[method], form)
		e.mu.Unlock()
		return nil, false
	})
	fs.SetEdgeHook(func(method string, body map[string]json.RawMessage) (any, bool) {
		e.mu.Lock()
		e.edges[method] = append(e.edges[method], body)
		e.mu.Unlock()
		return nil, false
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	e.a.EnsureUsers(ctx, []string{fakeslack.SelfID, fakeslack.ProfileID})
	if u, ok := e.a.S.User(fakeslack.SelfID); ok {
		e.a.S.Self = *u
	}
	gs, err := e.a.C.Usergroups(ctx)
	if err != nil {
		t.Fatal(err)
	}
	e.a.S.SetUsergroups(gs)
	return e, ctx
}

func (e *ugEnv) last(m string) url.Values {
	e.mu.Lock()
	defer e.mu.Unlock()
	if got := e.forms[m]; len(got) > 0 {
		f := url.Values{}
		for k, v := range got[len(got)-1] {
			if k != "token" { // the client's own
				f[k] = v
			}
		}
		return f
	}
	return nil
}

func (e *ugEnv) count(m string) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.forms[m])
}

// canEditUserGroupsInWorkspace and its kin (jiA8 wC, Ew, rk): the
// featureAccess store's user_groups__manage, then permissions/info on
// the grid with team_ids (allowed_per_team), the group's team. A
// non-editor gets nothing, a restricted viewer nothing whatever the
// server says, and a team without the feature nothing.
func TestUsergroupRightsLikeSlack(t *testing.T) {
	e, ctx := newUGEnv(t, false)
	if r := e.a.UsergroupRights(ctx, fakeslack.TeamID); r.Editable() || r.EditAny || r.OrgDash {
		t.Errorf("a non-editor: %+v", r)
	}
	e.mu.Lock()
	bodies := e.edges["edge/permissions/info"]
	e.mu.Unlock()
	if len(bodies) != 2 {
		t.Fatalf("permissions/info asked %d times, want the workspace and the org", len(bodies))
	}
	var perms, teams []string
	var self string
	_ = json.Unmarshal(bodies[0]["permissions"], &perms)
	_ = json.Unmarshal(bodies[0]["team_ids"], &teams)
	_ = json.Unmarshal(bodies[0]["user_id"], &self)
	if !slices.Equal(perms, []string{PermEditUserGroups, PermCreateUserGroups, PermDeleteUserGroups}) ||
		!slices.Equal(teams, []string{fakeslack.TeamID}) || self != fakeslack.SelfID || bodies[0]["as_admin"] != nil {
		t.Errorf("the workspace ask: %s", bodies[0])
	}
	var org []string
	_ = json.Unmarshal(bodies[1]["permissions"], &org)
	if !slices.Equal(org, []string{PermCreateAndEditOrgUserGroups, PermEditOrgUserGroups, PermDeleteOrgUserGroups}) || string(bodies[1]["as_admin"]) != "true" {
		t.Errorf("the org ask: %s", bodies[1])
	}
	if e.count("features.access.policies.list") == 0 {
		t.Error("the feature was never read")
	}

	ed, ctx2 := newUGEnv(t, true)
	r := ed.a.UsergroupRights(ctx2, fakeslack.TeamID)
	if !r.Edit || !r.Create || !r.Delete || !r.EditAny || !r.Editable() || r.OrgDash {
		t.Errorf("an editor: %+v", r)
	}
	if r := ed.a.UsergroupRights(ctx2, "T0OTHER001"); r.Edit || r.Create || r.Delete || !r.EditAny {
		t.Errorf("another workspace's group: %+v", r)
	}
	if c, ok := ed.a.CachedUsergroupRights(fakeslack.TeamID); !ok || !c.Edit {
		t.Errorf("cached: %+v %v", c, ok)
	}
	ed.a.S.Lock()
	ed.a.S.Self.IsRestricted = true
	ed.a.S.Unlock()
	if r, _ := ed.a.CachedUsergroupRights(fakeslack.TeamID); r.Editable() || r.EditAny {
		t.Errorf("a guest: %+v", r)
	}

	off, ctx3 := newUGEnv(t, true)
	off.fs.SetHook(func(method string, form url.Values) (any, bool) {
		if method == "features.access.policies.list" {
			return map[string]any{"ok": true, "feature_policies": map[string]any{"user_groups__manage": map[string]any{"is_enabled": false}}}, true
		}
		return nil, false
	})
	if r := off.a.UsergroupRights(ctx3, fakeslack.TeamID); r.Editable() {
		t.Errorf("user_groups__manage off: %+v", r)
	}
}

// The writes send Slack's arguments (updateUserGroup 55+m,
// bulkEditUserGroupMembers 9CgL, removeUserGroupMember 7AXZ,
// removeUserGroupChannel ru, disable / enable, subteams.validate*), lists
// comma-joined, and the store follows the answers and subteam_updated.
func TestUsergroupWritesLikeSlack(t *testing.T) {
	e, ctx := newUGEnv(t, true)
	a := e.a
	ws := a.StoredUsergroup(fakeslack.WorkspaceGroupID)
	e.fs.SetUsergroupChannels(ws.ID, []string{"C0FAKE00000", "C0FAKE00001"})
	p, err := a.UsergroupProfile(ctx, ws.ID)
	if err != nil || !slices.Equal(p.DefaultChannels(), []string{"C0FAKE00000", "C0FAKE00001"}) {
		t.Fatalf("profile %+v %v", p, err)
	}

	err = a.UpdateUsergroup(ctx, UsergroupUpdate{ID: ws.ID, TeamID: ws.TeamID, Name: "Snack squad", Handle: "snacksquad",
		Description: "All the snacks", Channels: []string{"C0FAKE00001", "C0FAKE00002"}, EnableSection: true})
	if err != nil {
		t.Fatal(err)
	}
	f := e.last("usergroups.update")
	want := url.Values{"usergroup": {ws.ID}, "name": {"Snack squad"}, "handle": {"snacksquad"}, "channels": {"C0FAKE00001,C0FAKE00002"},
		"team_id": {fakeslack.TeamID}, "enable_section": {"true"}, "description": {"All the snacks"}}
	if f.Encode() != want.Encode() {
		t.Errorf("usergroups.update\n got %v\nwant %v", f, want)
	}
	if g := a.StoredUsergroup(ws.ID); g.Name != "Snack squad" || g.Handle != "snacksquad" || g.Description != "All the snacks" || !g.IsSection {
		t.Errorf("after subteam_updated: %+v", g)
	}
	if chans, _ := a.S.GroupChannels(ws.ID); !slices.Equal(chans, []string{"C0FAKE00001", "C0FAKE00002"}) {
		t.Errorf("default channels after the update: %v", chans)
	}
	a.S.RLock()
	h := a.S.Usergroups[ws.ID]
	a.S.RUnlock()
	if h != "snacksquad" {
		t.Errorf("the mention handle: %q", h)
	}

	// Edit members' Save: users and team_id; the answer's members are stored
	g := a.StoredUsergroup(ws.ID)
	if err := a.SetUsergroupUsers(ctx, g, []string{fakeslack.SelfID, fakeslack.ProfileID, "U0FAKE00009"}); err != nil {
		t.Fatal(err)
	}
	if f := e.last("usergroups.users.update"); f.Get("users") != fakeslack.SelfID+","+fakeslack.ProfileID+",U0FAKE00009" || f.Get("team_id") != fakeslack.TeamID {
		t.Errorf("usergroups.users.update %v", f)
	}
	if ids, _ := a.S.GroupUsers(ws.ID); len(ids) != 3 || a.StoredUsergroup(ws.ID).UserCount != 3 {
		t.Errorf("stored members %v, count %d", ids, a.StoredUsergroup(ws.ID).UserCount)
	}

	// Remove from group…: the stored members less one, no team_id
	if err := a.RemoveUsergroupMember(ctx, ws.ID, "U0FAKE00009"); err != nil {
		t.Fatal(err)
	}
	f = e.last("usergroups.users.update")
	if f.Get("users") != fakeslack.SelfID+","+fakeslack.ProfileID || f.Has("team_id") {
		t.Errorf("removeUserGroupMember %v", f)
	}
	n := e.count("usergroups.users.update")
	if err := a.RemoveUsergroupMember(ctx, ws.ID, "U0NOTHERE1"); err != nil || e.count("usergroups.users.update") != n {
		t.Errorf("removing a non-member sent a call (%v)", err)
	}
	// the last member: no_users_provided
	a.S.SetGroupUsers(ws.ID, []string{fakeslack.SelfID}, false)
	if err := a.RemoveUsergroupMember(ctx, ws.ID, fakeslack.SelfID); slackErrCode(err) != "no_users_provided" {
		t.Errorf("the last member: %v", err)
	}

	// removeUserGroupChannel: channels and the section flag, description ""
	p, _ = a.StoredUsergroupProfile(ws.ID)
	if err := a.RemoveUsergroupChannel(ctx, p, "C0FAKE00001"); err != nil {
		t.Fatal(err)
	}
	f = e.last("usergroups.update")
	want = url.Values{"usergroup": {ws.ID}, "channels": {"C0FAKE00002"}, "enable_section": {"true"}, "description": {""}}
	if f.Encode() != want.Encode() {
		t.Errorf("removeUserGroupChannel\n got %v\nwant %v", f, want)
	}

	// disable / enable carry the team; subteam_updated brings date_delete
	if err := a.DisableUsergroup(ctx, ws.ID, ws.TeamID); err != nil {
		t.Fatal(err)
	}
	if f := e.last("usergroups.disable"); f.Get("usergroup") != ws.ID || f.Get("team_id") != fakeslack.TeamID {
		t.Errorf("usergroups.disable %v", f)
	}
	if p, _ := a.StoredUsergroupProfile(ws.ID); !p.Disabled {
		t.Error("not deactivated after subteam_updated")
	}
	if err := a.EnableUsergroup(ctx, ws.ID, ""); err != nil {
		t.Fatal(err)
	}
	if f := e.last("usergroups.enable"); f.Has("team_id") {
		t.Errorf("usergroups.enable from the menu sent team_id: %v", f)
	}
	if p, _ := a.StoredUsergroupProfile(ws.ID); p.Disabled {
		t.Error("still deactivated after usergroups.enable")
	}

	// subteams.validateName / validateHandle: {name|handle, target_team}
	if code, err := a.CheckUsergroupName(ctx, "Org crew", fakeslack.TeamID); code != "name_already_exists" || err == nil {
		t.Errorf("a taken name: %q %v", code, err)
	}
	if f := e.last("subteams.validateName"); f.Get("name") != "Org crew" || f.Get("target_team") != fakeslack.TeamID {
		t.Errorf("subteams.validateName %v", f)
	}
	if code, _ := a.CheckUsergroupHandle(ctx, "orgcrew", fakeslack.TeamID); code != "handle_already_exists" {
		t.Errorf("a taken handle: %q", code)
	}
	if code, err := a.CheckUsergroupHandle(ctx, "fresh-handle", fakeslack.TeamID); code != "" || err != nil {
		t.Errorf("a free handle: %q %v", code, err)
	}

	// the locked group: auto_type comes through
	if adm := a.StoredUsergroup(fakeslack.AdminsGroupID); !adm.AutoGenerated() || adm.AutoType != "admin" {
		t.Errorf("the admins group %+v", adm)
	}
	// subteam_updated for a group the client doesn't know is skipped
	a.Inject(slack.Event{Type: "subteam_updated", Raw: json.RawMessage(`{"type":"subteam_updated","subteam":{"id":"S0UNKNOWN1","handle":"ghosts","name":"Ghosts"}}`)})
	if g := a.StoredUsergroup("S0UNKNOWN1"); g.ID != "" {
		t.Errorf("an unknown group joined the store: %+v", g)
	}
	// subteam_members_changed changes nothing in the client (isSonic)
	before, _ := a.S.GroupUsers(ws.ID)
	a.Inject(slack.Event{Type: "subteam_members_changed", Raw: json.RawMessage(`{"type":"subteam_members_changed","subteam_id":"` + ws.ID + `","added_users":["U0FAKE00007"]}`)})
	if after, _ := a.S.GroupUsers(ws.ID); !slices.Equal(before, after) {
		t.Errorf("subteam_members_changed changed the members: %v → %v", before, after)
	}
}

func slackErrCode(err error) string {
	var se *slack.Error
	if errors.As(err, &se) {
		return se.Code
	}
	return ""
}
