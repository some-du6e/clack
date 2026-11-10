package ui

// The channel browser's Workspaces and Organizations filters (Slack's
// SearchFilterWorkspace and SearchFilterOrgs) against internal/fakeslack,
// which answers the browser's search.modules.channels and
// users.slackConnect.orgList through a hook here. Synthetic only.

import (
	"context"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/views"
)

// browserHook answers the channel browser's calls and records their forms.
type browserHook struct {
	mu    sync.Mutex
	forms []url.Values
}

func (b *browserHook) hook(method string, form url.Values) (any, bool) {
	switch method {
	case "search.modules.channels":
		if form.Get("search_context") != "desktop_channel_browser" {
			return nil, false
		}
		b.mu.Lock()
		b.forms = append(b.forms, form)
		b.mu.Unlock()
		return map[string]any{"ok": true, "items": []any{
			map[string]any{"id": "G0BROWSE01", "name": "plans", "is_private": true, "is_member": true, "member_count": 4,
				"purpose": map[string]any{"value": "Synthetic plans"}},
			map[string]any{"id": "C0BROWSE02", "name": "lounge", "is_member": false, "member_count": 12,
				"purpose": map[string]any{"value": "Synthetic lounge"}},
		}, "pagination": map[string]any{"total_count": 2, "page": 1, "page_count": 1}}, true
	case "users.slackConnect.orgList":
		return map[string]any{"ok": true, "orgs": []any{map[string]any{"team_summary": map[string]any{
			"id": fakeslack.ExternalTeamID, "name": "Partner Co"}}}}, true
	}
	return nil, false
}

// sent reports whether a browser search went out with every param in want.
func (b *browserHook) sent(want map[string]string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, f := range b.forms {
		ok := true
		for k, v := range want {
			if f.Get(k) != v {
				ok = false
			}
		}
		if ok {
			return true
		}
	}
	return false
}

func TestChannelBrowserFilters(t *testing.T) {
	e, fs := previewEnv(t)
	bh := &browserHook{}
	fs.SetHook(bh.hook)
	t.Cleanup(func() { fs.SetHook(nil) })
	e.a.Navigate("directories?tab=channels")
	e.settle(300 * time.Millisecond)
	v, ok := e.a.views[Route("directories?tab=channels")].(*views.Directories)
	if !ok {
		t.Fatalf("no channel browser: %T", e.a.views[Route("directories?tab=channels")])
	}
	e.until("the browser's results", 20*time.Second, func() bool { return bh.sent(map[string]string{"channel_type": "exclude_archived"}) })

	// Organizations: the connected orgs as suggestions; a pick narrows to
	// the channels shared with it
	e.until("Partner Co in Organizations", 20*time.Second, func() bool {
		return slices.Contains(v.FilterOptions("Organizations"), "Partner Co")
	})
	if got := strings.Join(v.FilterOptions("Organizations"), " | "); got != "Suggestions | Partner Co" {
		t.Errorf("Organizations options: %s", got)
	}
	v.OpenFilter("Organizations")
	e.settle(300 * time.Millisecond)
	e.shot("channel-browser-organizations")
	if !v.PickFilter("Organizations", fakeslack.ExternalTeamID) {
		t.Fatal("no Partner Co option")
	}
	e.until("external_shared_team_ids", 10*time.Second, func() bool {
		return bh.sent(map[string]string{"external_shared_team_ids": fakeslack.ExternalTeamID, "channel_type": "external_shared"})
	})
	if got := v.FilterText("Organizations"); got != "With Partner Co" {
		t.Errorf("Organizations pill %q", got)
	}
	v.PickFilter("Organizations", fakeslack.ExternalTeamID) // unpick
	e.settle(100 * time.Millisecond)

	// Workspaces: All workspaces, then Your workspaces
	e.until("the workspace's name", 20*time.Second, func() bool {
		return slices.Contains(v.FilterOptions("Workspaces"), "Fake Club")
	})
	if got := strings.Join(v.FilterOptions("Workspaces"), " | "); got != "All workspaces | Your workspaces | Fake Club" {
		t.Errorf("Workspaces options: %s", got)
	}
	v.OpenFilter("Workspaces")
	e.settle(300 * time.Millisecond)
	e.shot("channel-browser-workspaces")
	if !v.PickFilter("Workspaces", fakeslack.TeamID) {
		t.Fatal("no Fake Club option")
	}
	e.until("search_only_team", 10*time.Second, func() bool {
		return bh.sent(map[string]string{"search_only_team": fakeslack.TeamID, "channel_type": "exclude_archived"})
	})
	if got := v.FilterText("Workspaces"); got != "In Fake Club" {
		t.Errorf("Workspaces pill %q", got)
	}
	if got := strings.Join(v.FilterOptions("Workspaces"), " | "); got != "All workspaces | Fake Club" {
		t.Errorf("Workspaces options with one chosen: %s", got)
	}
	e.settle(300 * time.Millisecond)
	e.shot("channel-browser-workspace-chosen")
	for _, f := range []string{"Title", "Location"} {
		if v.OpenFilter(f) {
			t.Errorf("the channel browser has a %s filter", f)
		}
	}
}

// The People tab's Title and Location filters send profile_title /
// profile_location and suggest search.modules.people's filter_suggestions.
func TestPeopleBrowserFacetFilters(t *testing.T) {
	e, fs := previewEnv(t)
	var mu sync.Mutex
	var forms []url.Values
	fs.SetHook(func(method string, form url.Values) (any, bool) {
		if method != "search.modules.people" || form.Get("search_context") != "desktop_people_browser" {
			return nil, false
		}
		mu.Lock()
		forms = append(forms, form)
		mu.Unlock()
		return map[string]any{"ok": true, "items": []any{}, "pagination": map[string]any{"total_count": 0, "page": 1, "page_count": 1},
			"filter_suggestions": map[string]any{
				"title":    []any{map[string]any{"value": "Engineer", "facet_count": 4, "checked": false}},
				"location": []any{map[string]any{"value": "Oslo", "facet_count": 1, "checked": false}, map[string]any{"value": "Tokyo", "facet_count": 3, "checked": false}},
			}}, true
	})
	t.Cleanup(func() { fs.SetHook(nil) })
	e.a.Navigate("directories")
	e.settle(300 * time.Millisecond)
	v, ok := e.a.views[Route("directories")].(*views.Directories)
	if !ok {
		t.Fatalf("no directories: %T", e.a.views[Route("directories")])
	}
	v.ShowTab(0)
	e.until("Title suggestions", 20*time.Second, func() bool { return slices.Contains(v.FilterOptions("Title"), "Engineer") })
	if got := strings.Join(v.FilterOptions("Location"), " | "); got != "Suggestions | Tokyo | Oslo" {
		t.Errorf("Location options: %s", got)
	}
	v.OpenFilter("Location")
	e.settle(300 * time.Millisecond)
	e.shot("people-browser-location")
	v.PickFilter("Location", "Tokyo")
	v.PickFilter("Title", "Engineer")
	e.until("profile filters", 10*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		for _, f := range forms {
			if f.Get("profile_location") == "Tokyo" && f.Get("profile_title") == "Engineer" {
				return true
			}
		}
		return false
	})
	if got := v.FilterText("Location"); got != "Location: Tokyo" {
		t.Errorf("Location pill %q", got)
	}
	e.settle(300 * time.Millisecond)
	e.shot("people-browser-filtered")
}

// A channel with a draft gets BaseChannelSuffixDraftIcon's pencil while it
// isn't the open one (screenshot set: sidebar-draft-pencil).
func TestSidebarDraftPencil(t *testing.T) {
	e, _ := previewEnv(t)
	ch := fakeslackMemberChannel(e)
	if _, err := e.a.Core().SaveDraft(context.Background(), core.DraftOpts{Channel: ch, Text: "synthetic draft"}); err != nil {
		t.Fatal(err)
	}
	e.a.data.loadDrafts(e.a)
	e.until("the channel's draft mark", 10*time.Second, func() bool { return e.a.data.draftChans[ch] })
	e.a.Navigate("threads")
	e.settle(300 * time.Millisecond)
	e.shot("sidebar-draft-pencil")
}
