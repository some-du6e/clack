package ui

// The user group browser's other states against internal/fakeslack, as
// Slack's BrowseUserGroupsView and its fetch draw them (HF/B, HKCz
// getResults, wBUp, D8Wa; build a09b625d90aa): off a grid (search.team),
// failed fetches (Slack's searchers answer an error as a page with no
// groups), Load More's spinner, the banner after a fetch, a guest's tabs
// and a workspace without groups. Synthetic only.

import (
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/views"
)

// groupsBrowser0 opens Directories on User Groups without waiting for rows.
func groupsBrowser0(t *testing.T, e *perfEnv) *views.Directories {
	t.Helper()
	e.a.Navigate("directories")
	e.settle(300 * time.Millisecond)
	v, ok := e.a.views[Route("directories")].(*views.Directories)
	if !ok {
		t.Fatalf("no Directories: %T", e.a.views[Route("directories")])
	}
	v.ShowTab(2)
	e.frame()
	return v
}

func TestUserGroupBrowserOffGridLikeSlack(t *testing.T) {
	e, fs := previewEnvWith(t, func(fs *fakeslack.Server) {
		fs.OffGrid = true
		fs.SetUsergroupDisabled(fakeslack.AdminsGroupID)
	})
	c := e.a.data.core
	// the boot list (usergroups.list leaves the disabled admins group
	// out): give the store Slack's record of it, auto_type and team_id
	c.S.PutUsergroup(slack.Usergroup{ID: fakeslack.AdminsGroupID, Handle: "admins", Name: "Admins", TeamID: fakeslack.TeamID, AutoType: "admin", IsSection: true, DateUpdate: 7})
	v := groupsBrowser(t, e)
	ids, total, more := v.GroupsShown()
	if len(ids) != 23 || total != 23 || more {
		t.Fatalf("off grid: %d groups of %d, Load More %v; want all 23", len(ids), total, more)
	}
	forms := fs.UsergroupForms("search.team")
	if len(forms) == 0 || len(fs.UsergroupForms("usergroups.search")) != 0 {
		t.Fatalf("off grid asked search.team %d times, usergroups.search %d", len(forms), len(fs.UsergroupForms("usergroups.search")))
	}
	f := forms[0]
	for k, want := range map[string]string{"count": "50", "sort": "name", "sort_dir": "asc", "include_users": "false", "include_deleted": "1", "org_wide_aware": "true",
		"query": `{"clauses":[{"type":"is","value":"group"},{"type":"external","value":false}],"type":"and"}`} {
		if f.Get(k) != want {
			t.Errorf("search.team %s = %q, want %q", k, f.Get(k), want)
		}
	}
	// search.team's items have no team_id, auto_type, is_section or
	// date_update: upsertEntity keeps the stored ones
	g := c.StoredUsergroup(fakeslack.AdminsGroupID)
	if g.AutoType != "admin" || g.TeamID != fakeslack.TeamID || !g.IsSection || g.Updated != 7 || g.DateDelete == 0 {
		t.Errorf("the admins group after the directory: %+v", g)
	}
	shown := false
	for _, id := range ids {
		shown = shown || id == fakeslack.AdminsGroupID
	}
	if !shown {
		t.Error("the deactivated group isn't listed (include_deleted 1)")
	}
	e.shot("usergroup-browser-offgrid-light")
}

// A failed fetch is Slack's empty page: nothing shown is "No results"
// (no error state), a failed Load More keeps the rows and drops the
// button, a new sort starts from nothing; Load More spins while it loads.
func TestUserGroupBrowserErrorsLikeSlack(t *testing.T) {
	var mu sync.Mutex
	failFirst, failMore, hold := true, false, make(chan struct{})
	held := false
	e, fs := previewEnvWith(t, func(fs *fakeslack.Server) {
		fs.Size.Usergroups = 2003
		fs.SetHook(func(method string, f url.Values) (any, bool) {
			if method != "usergroups.search" || f.Get("count") != "50" {
				return nil, false
			}
			mu.Lock()
			h := held
			mu.Unlock()
			if f.Has("cursor_mark") && h {
				<-hold
			}
			mu.Lock()
			ff, fm := failFirst, failMore // read after the hold: the test flips them while it waits
			mu.Unlock()
			if ff && !f.Has("cursor_mark") || fm {
				return map[string]any{"ok": false, "error": "internal_error"}, true
			}
			return nil, false
		})
	})
	t.Cleanup(func() { fs.SetHook(nil) })
	v := groupsBrowser0(t, e)
	e.until("the failed first page", 20*time.Second, func() bool { return v.GroupsLoaded() })
	if got := v.GroupsEmpty(); got != "No user groups have been created yet" {
		t.Errorf("a failed first page shows %q, want Slack's empty state", got)
	}
	e.shot("usergroup-browser-failed-light")

	mu.Lock()
	failFirst, held = false, true
	mu.Unlock()
	v.SortGroups(1)
	e.until("the Z to A page", 20*time.Second, func() bool { ids, _, _ := v.GroupsShown(); return v.GroupsLoaded() && len(ids) == 50 })
	v.LoadMoreGroups()
	e.until("Load More's spinner", 10*time.Second, func() bool { return v.GroupsLoadingMore() })
	e.shot("usergroup-browser-loading-more-light")
	mu.Lock()
	held, failMore = false, true
	mu.Unlock()
	close(hold)
	e.until("the failed second page", 20*time.Second, func() bool { return v.GroupsLoaded() })
	if ids, _, more := v.GroupsShown(); len(ids) != 50 || more {
		t.Errorf("after a failed Load More: %d rows, Load More %v; want the 50 and no button", len(ids), more)
	}

	// a new sort that fails starts from nothing: Slack's results reset
	v.SortGroups(2)
	e.until("the failed sort", 20*time.Second, func() bool { return v.GroupsLoaded() })
	if ids, _, _ := v.GroupsShown(); len(ids) != 0 || v.GroupsEmpty() != "No user groups have been created yet" {
		t.Errorf("a failed new sort kept %d rows, empty state %q", len(ids), v.GroupsEmpty())
	}
}

// The banner reads its rule on every render and writes its pref as it
// unmounts: the first Load More takes it down for good (2,006 groups).
func TestUserGroupBrowserBannerAfterFetchLikeSlack(t *testing.T) {
	e, fs := previewEnvWith(t, func(fs *fakeslack.Server) {
		fs.Size.Usergroups = 2003
		fs.UsergroupEditor = true
	})
	v := groupsBrowser(t, e)
	e.until("the banner", 10*time.Second, func() bool { return v.GroupsHero() })
	v.LoadMoreGroups()
	e.until("the second page", 20*time.Second, func() bool { ids, _, _ := v.GroupsShown(); return v.GroupsLoaded() && len(ids) == 100 })
	e.settle(300 * time.Millisecond)
	if v.GroupsHero() {
		t.Error("the banner came back after Load More")
	}
	if p, ok := fs.Pref("browsers_seen_initial_user_groups_education"); !ok || p != true {
		t.Errorf("seen pref %v", p)
	}
}

// A guest has neither User Groups nor External and Invitations; a
// directories?tab=user_groups link shows People.
func TestUserGroupBrowserGuestTabsLikeSlack(t *testing.T) {
	e, _ := previewEnv(t)
	v := groupsBrowser(t, e)
	if !v.TabShown(2) || !v.TabShown(3) || !v.TabShown(4) {
		t.Fatalf("a member's tabs: groups %v external %v invitations %v", v.TabShown(2), v.TabShown(3), v.TabShown(4))
	}
	c := e.a.data.core
	u, ok := c.S.User(c.S.SelfID())
	if !ok {
		t.Fatal("no self user")
	}
	g := *u
	g.IsRestricted = true
	c.S.PutUsers([]slack.User{g})
	e.frame()
	if v.TabShown(2) || v.TabShown(3) || v.TabShown(4) || !v.TabShown(0) || !v.TabShown(1) {
		t.Errorf("a guest's tabs: groups %v external %v invitations %v", v.TabShown(2), v.TabShown(3), v.TabShown(4))
	}
	v.ShowTab(2)
	e.frame()
	if v.Tab() != "People" {
		t.Errorf("a guest's User Groups link shows %q", v.Tab())
	}
	e.shot("usergroup-browser-guest-light")
}

// A workspace without user groups: "No user groups have been created yet".
func TestUserGroupBrowserNoGroupsLikeSlack(t *testing.T) {
	e, _ := previewEnvWith(t, func(fs *fakeslack.Server) {
		fs.Size.Usergroups = 0
		fs.SetUsergroups([]fakeslack.UsergroupFixture{})
	})
	v := groupsBrowser0(t, e)
	e.until("the empty page", 20*time.Second, func() bool { return v.GroupsLoaded() })
	if got := v.GroupsEmpty(); got != "No user groups have been created yet" {
		t.Errorf("no groups: %q", got)
	}
}

// A cold boot with no persisted feature answer: the User Groups tab is in
// the strip from the first frame and stays (Slack reads the features before
// it draws the strip); a guest never has it.
func TestUserGroupTabColdBootLikeSlack(t *testing.T) {
	// the features answer is held until the strip has drawn (a slow
	// network): the strip must not wait for it, nor change when it comes
	hold := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(hold) }) }
	e, fs := previewEnvWith(t, func(fs *fakeslack.Server) {
		fs.SetHook(func(method string, f url.Values) (any, bool) {
			if method == "features.access.policies.list" {
				<-hold
			}
			return nil, false
		})
	})
	t.Cleanup(func() { release(); fs.SetHook(nil) })
	e.a.Navigate("directories")
	v, ok := e.a.views[Route("directories")].(*views.Directories)
	if !ok { // mounted by the first frame
		e.frame()
		v, ok = e.a.views[Route("directories")].(*views.Directories)
	}
	if !ok {
		t.Fatalf("no Directories: %T", e.a.views[Route("directories")])
	}
	if !v.TabShown(2) {
		t.Fatal("User Groups isn't in the strip's first frame for a member")
	}
	for i := 0; i < 20; i++ { // the answer still out
		e.frame()
		if !v.TabShown(2) {
			t.Fatal("User Groups left the strip while the features answer was out")
		}
	}
	release()
	for dl := time.Now().Add(3 * time.Second); time.Now().Before(dl); {
		e.frame()
		if !v.TabShown(2) {
			t.Fatal("User Groups left the strip when the answer came (it allows the tab)")
		}
		time.Sleep(10 * time.Millisecond)
	}
	e.until("the features answer", 10*time.Second, func() bool {
		on, known := e.a.data.core.FeatureState("user_groups__manage")
		return on && known
	})
	c := e.a.data.core
	u, _ := c.S.User(c.S.SelfID())
	g := *u
	g.IsRestricted = true
	c.S.PutUsers([]slack.User{g})
	e.frame()
	if v.TabShown(2) {
		t.Error("a guest has User Groups")
	}
}
