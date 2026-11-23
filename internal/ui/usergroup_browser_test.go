package ui

// Directories > User Groups against internal/fakeslack, as Slack's
// BrowseUserGroupsView draws it (shared-js_modern_generated_types_api_
// methods_usergroups_create_ts chunk HF/B, client-boot wBUp, puFU, 0dhJ;
// build a09b625d90aa, 2026-11-22). The fake is a grid with 2,000-odd
// groups, like Hack Club: the browser pages usergroups.search 50 at a
// time and keeps going with Load More; Create User Group and its banner
// come only for a viewer who may create groups. Synthetic only.

import (
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/views"
)

// groupsBrowser opens Directories on User Groups and waits for its first page.
func groupsBrowser(t *testing.T, e *perfEnv) *views.Directories {
	t.Helper()
	e.a.Navigate("directories")
	e.settle(300 * time.Millisecond)
	v, ok := e.a.views[Route("directories")].(*views.Directories)
	if !ok {
		t.Fatalf("no Directories: %T", e.a.views[Route("directories")])
	}
	v.ShowTab(2)
	e.until("the first page of groups", 20*time.Second, func() bool {
		ids, _, _ := v.GroupsShown()
		return v.GroupsLoaded() && len(ids) > 0
	})
	return v
}

func TestUserGroupBrowserLikeSlack(t *testing.T) {
	e, fs := previewEnvWith(t, func(fs *fakeslack.Server) {
		fs.Size.Usergroups = 2003 // a warm Hack Club cache holds 2,003
		fs.UsergroupEditor = true
	})
	v := groupsBrowser(t, e)
	c := e.a.data.core
	ids, total, more := v.GroupsShown()
	if len(ids) != 50 || total != 2003+3 || !more {
		t.Fatalf("first page: %d groups of %d, Load More %v; want 50 of 2006 and Load More", len(ids), total, more)
	}
	forms := fs.UsergroupForms("usergroups.search")
	if len(forms) == 0 {
		t.Fatal("no usergroups.search")
	}
	f := forms[len(forms)-1]
	for k, want := range map[string]string{"count": "50", "sort": "name", "sort_dir": "asc", "include_hidden": "false", "include_disabled": "true", "keyword": ""} {
		if f.Get(k) != want || !f.Has(k) {
			t.Errorf("first page %s = %q, want %q", k, f.Get(k), want)
		}
	}
	if f.Has("cursor_mark") {
		t.Errorf("the first page sent cursor_mark %q", f.Get("cursor_mark"))
	}
	prev := ""
	for _, id := range ids { // A to Z
		n := strings.ToLower(c.StoredUsergroup(id).Name)
		if n < prev {
			t.Fatalf("not A to Z: %q after %q", n, prev)
		}
		prev = n
	}

	// a viewer who may create groups: Create User Group and, on the first
	// visit, the banner, whose pref the visit writes (it unmounts on the
	// first refetch below)
	if !v.CreateGroupShown() {
		t.Error("no Create User Group for a viewer with CREATE_USER_GROUPS")
	}
	e.until("the banner", 10*time.Second, func() bool { return v.GroupsHero() })
	e.shot("usergroup-browser-light")
	e.until("the seen pref", 10*time.Second, func() bool {
		p, ok := fs.Pref("browsers_seen_initial_user_groups_education")
		return ok && p == true
	})

	// Load More: the next 50 from next_cursor_mark, after the first
	v.LoadMoreGroups()
	e.until("the second page", 20*time.Second, func() bool {
		got, _, _ := v.GroupsShown()
		return v.GroupsLoaded() && len(got) == 100
	})
	got, _, _ := v.GroupsShown()
	if strings.Join(got[:50], ",") != strings.Join(ids, ",") {
		t.Error("the second page replaced the first")
	}
	forms = fs.UsergroupForms("usergroups.search")
	if f := forms[len(forms)-1]; f.Get("cursor_mark") != "ugc:50" || f.Get("count") != "50" {
		t.Errorf("Load More sent %v", f)
	}

	// Most members: member_count desc, from the top again
	v.SortGroups(4)
	e.until("the member sort", 20*time.Second, func() bool {
		got, _, _ := v.GroupsShown()
		return v.GroupsLoaded() && len(got) == 50
	})
	forms = fs.UsergroupForms("usergroups.search")
	if f := forms[len(forms)-1]; f.Get("sort") != "member_count" || f.Get("sort_dir") != "desc" || f.Has("cursor_mark") {
		t.Errorf("Most members sent %v", f)
	}
	got, _, _ = v.GroupsShown()
	if n := c.StoredUsergroup(got[0]).UserCount; n != 49 {
		t.Errorf("Most members starts with a %d-member group", n)
	}

	if v.GroupsHero() {
		t.Error("the banner is back after Load More and a sort (it unmounted, and the pref is seen)")
	}
	e.a.Navigate("activity")
	e.settle(200 * time.Millisecond)
	e.a.Navigate("directories")
	v.ShowTab(2)
	e.settle(500 * time.Millisecond)
	if v.GroupsHero() {
		t.Error("the banner came back on the next visit (2,006 groups, seen)")
	}
	e.shot("usergroup-browser-seen-light")

	// a row opens its group's User Group pane (showUserGroupDetails)
	got, _, _ = v.GroupsShown()
	v.OpenGroup(got[0])
	e.frame()
	if p, ok := e.a.secondary.view.(*popups.UserGroupView); !ok || p.GroupID() != got[0] {
		t.Errorf("the row opened %T", e.a.secondary.view)
	}

	// Create User Group opens the create dialog, never a toast
	e.a.toast = toastState{}
	v.CreateUserGroup()
	e.frame()
	var d *popups.UserGroupEditModal
	for _, o := range e.a.overlays {
		if m, ok := o.(*popups.UserGroupEditModal); ok && !m.Dismissed() && m.Creating() {
			d = m
		}
	}
	if d == nil || e.a.toast.text != "" {
		t.Fatalf("Create User Group opened %v, toasted %q", d, e.a.toast.text)
	}
	if d.Title() != "Create User Group" || d.FooterNote() != "Step 1 of 2" {
		t.Errorf("the dialog is %q, %q", d.Title(), d.FooterNote())
	}
}

// A viewer without CREATE_USER_GROUPS (the fake's default): no Create
// User Group, no banner, the groups all the same.
func TestUserGroupBrowserNoCreateLikeSlack(t *testing.T) {
	e, _ := previewEnv(t)
	v := groupsBrowser(t, e)
	e.settle(800 * time.Millisecond) // the rights' answer
	if v.CreateGroupShown() || v.GroupsHero() {
		t.Errorf("Create User Group %v, banner %v for a viewer who can't create groups", v.CreateGroupShown(), v.GroupsHero())
	}
	if ids, total, _ := v.GroupsShown(); len(ids) != 23 || total != 23 {
		t.Errorf("%d groups of %d, want all 23", len(ids), total)
	}
	e.shot("usergroup-browser-viewer-light")
}
