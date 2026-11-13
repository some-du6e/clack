package core

import (
	"context"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// A user group's members come two ways, as in Slack's BaseUserGroupView:
// a workspace group (T… team_id) through usergroups.users.list, an
// org-level one (E… team_id) through search.enterprise. On the fake, as
// on the real grid, search.enterprise's belongs_to_usergroup_id finds
// nobody in a workspace group: the query the pane used to send for every
// group, which showed "Nobody’s in this group" for one with 29 members.
func TestUsergroupMembersByKind(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	a := New(slack.New(fs.Creds()), store.New(""))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var mu sync.Mutex
	forms := map[string][]url.Values{}
	fs.SetHook(func(method string, form url.Values) (any, bool) {
		mu.Lock()
		forms[method] = append(forms[method], form)
		mu.Unlock()
		return nil, false
	})
	last := func(m string) url.Values {
		mu.Lock()
		defer mu.Unlock()
		if got := forms[m]; len(got) > 0 {
			return got[len(got)-1]
		}
		return nil
	}
	gs, err := a.C.Usergroups(ctx)
	if err != nil {
		t.Fatal(err)
	}
	a.S.SetUsergroups(gs)

	ws := a.StoredUsergroup(fakeslack.WorkspaceGroupID)
	org := a.StoredUsergroup(fakeslack.OrgGroupID)
	if ws.TeamID != fakeslack.TeamID || ws.OrgLevel() {
		t.Fatalf("workspace group %+v", ws)
	}
	if org.TeamID != fakeslack.EnterpriseID || !org.OrgLevel() {
		t.Fatalf("org group %+v", org)
	}
	for id, want := range map[string]bool{"E0FAKE0001": true, "E12345678": true, "E1234567": false, "T0266FRGM": false, "e0fake0001": false, "": false} {
		if got := (Usergroup{TeamID: id}).OrgLevel(); got != want {
			t.Errorf("OrgLevel(%q) = %v", id, got)
		}
	}

	// the old path finds nobody in a workspace group
	us, _, n, err := a.UsergroupMembers(ctx, ws.ID, "", "")
	if err != nil || len(us) != 0 || n != 0 {
		t.Fatalf("search.enterprise on a workspace group: %d users, num_found %d, %v", len(us), n, err)
	}
	// fetchAndUpsertUserGroupMembers: usergroups.users.list {usergroup, team_id}
	us, next, err := a.UsergroupUsers(ctx, ws, "")
	if err != nil || len(us) != 6 || next != "" {
		t.Fatalf("usergroups.users.list: %d users, next %q, %v", len(us), next, err)
	}
	f := last("usergroups.users.list")
	if f.Get("usergroup") != ws.ID || f.Get("team_id") != fakeslack.TeamID || f.Has("limit") || f.Has("cursor") {
		t.Errorf("usergroups.users.list form %v", f)
	}
	a.S.RLock()
	realNames := a.S.Prefs.DisplayRealNames
	a.S.RUnlock()
	for i := 1; i < len(us); i++ {
		p, _ := a.S.User(us[i-1].ID)
		q, _ := a.S.User(us[i].ID)
		if prefLowerName(p, realNames) > prefLowerName(q, realNames) {
			t.Errorf("not sorted by getPrefCompliantMemberNameLowerCase: %q before %q", prefLowerName(p, realNames), prefLowerName(q, realNames))
		}
	}
	if cached, ok := a.CachedUsergroupUsers(ws.ID, true); !ok || len(cached) != 6 {
		t.Errorf("cached members: %d %v", len(cached), ok)
	}

	// an org-level group: search.enterprise, deactivated accounts left out
	us, next, n, err = a.UsergroupMembers(ctx, org.ID, "", "")
	if err != nil || len(us) != 4 || n != 4 || next != "" {
		t.Fatalf("search.enterprise on an org group: %d users, num_found %d, next %q, %v", len(us), n, next, err)
	}
	if f := last("search.enterprise"); f.Get("cursor_mark") != "*" || f.Get("count") != "100" {
		t.Errorf("search.enterprise form %v", f)
	}
	if cached, ok := a.CachedUsergroupUsers(org.ID, false); !ok || len(cached) != 4 {
		t.Errorf("org cached members: %d %v", len(cached), ok)
	}
	// its usergroups.users.list asks 100 at a time
	if _, _, err := a.UsergroupUsers(ctx, org, ""); err != nil {
		t.Fatal(err)
	}
	if f := last("usergroups.users.list"); f.Get("limit") != "100" || f.Get("team_id") != fakeslack.EnterpriseID {
		t.Errorf("org usergroups.users.list form %v", f)
	}

	// no team_id: Slack's thunk refuses before calling
	if _, _, err := a.UsergroupUsers(ctx, Usergroup{ID: "S0NOTEAM"}, ""); err == nil {
		t.Error("no team_id: no error")
	}
}
