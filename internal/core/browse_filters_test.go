package core

import (
	"context"
	"slices"
	"testing"

	"github.com/some-du6e/clack/internal/slack"
)

// The Directories filters send Slack's params (XjMX getSearchAPIParams).
func TestBrowseFilterParams(t *testing.T) {
	a, f := testApp(t)
	ctx := context.Background()
	f.reply["search.modules.channels"] = `{"ok":true,"items":[],"pagination":{"total_count":0,"page":1,"page_count":1}}`
	cases := []struct {
		o    ChannelBrowseOpts
		want map[string]string
	}{
		{ChannelBrowseOpts{Team: "T2"}, map[string]string{"search_only_team": "T2", "channel_type": "exclude_archived"}},
		{ChannelBrowseOpts{Orgs: []string{"E9", "E8"}}, map[string]string{"external_shared_team_ids": "E9,E8", "channel_type": "external_shared"}},
		{ChannelBrowseOpts{Orgs: []string{"E9"}, Type: "archived"}, map[string]string{"channel_type": "external_shared_archived"}},
		{ChannelBrowseOpts{Orgs: []string{"E9"}, Type: "private"}, map[string]string{"channel_type": "external_shared_private"}},
		{ChannelBrowseOpts{Orgs: []string{"E9"}, Type: "public"}, map[string]string{"channel_type": "external_shared"}},
	}
	for _, c := range cases {
		if _, err := a.BrowseChannelsWith(ctx, c.o); err != nil {
			t.Fatal(err)
		}
		p := f.last("search.modules.channels")
		for k, want := range c.want {
			if formVal(p, k) != want {
				t.Errorf("%+v: %s = %q, want %q", c.o, k, formVal(p, k), want)
			}
		}
	}
	if _, err := a.BrowseChannelsWith(ctx, ChannelBrowseOpts{}); err != nil {
		t.Fatal(err)
	}
	if p := f.last("search.modules.channels"); p.Has("search_only_team") || p.Has("external_shared_team_ids") {
		t.Errorf("no filter sent %v", p)
	}

	f.reply["search.modules.people"] = `{"ok":true,"items":[],"pagination":{"total_count":0,"page":1,"page_count":1},
		"filter_suggestions":{"title":[{"value":"Engineer","facet_count":3,"checked":false},{"value":"Engineer","facet_count":3,"checked":false},
		{"value":"Designer","facet_count":9,"checked":false}],"location":[{"value":"Berlin","facet_count":2,"checked":false},
		{"value":"Tokyo","facet_count":5,"checked":true}]}}`
	pg, err := a.BrowsePeople(ctx, PeopleBrowseOpts{Team: "T2", Titles: []string{"Engineer", "Designer"}, Locations: []string{"Tokyo"}})
	if err != nil {
		t.Fatal(err)
	}
	p := f.last("search.modules.people")
	for k, want := range map[string]string{"search_only_team": "T2", "profile_title": "Engineer,Designer", "profile_location": "Tokyo"} {
		if formVal(p, k) != want {
			t.Errorf("people %s = %q, want %q", k, formVal(p, k), want)
		}
	}
	if !slices.Equal(pg.Titles, []Facet{{"Engineer", 3}, {"Designer", 9}}) {
		t.Errorf("titles %+v (kept in order, duplicates dropped)", pg.Titles)
	}
	if !slices.Equal(pg.Locations, []Facet{{"Tokyo", 5}, {"Berlin", 2}}) {
		t.Errorf("locations %+v (most people first)", pg.Locations)
	}

	f.reply["usergroups.search"] = `{"ok":true,"usergroups":[]}`
	if _, err := a.SearchUsergroups(ctx, UsergroupOpts{Team: "T2"}); err != nil {
		t.Fatal(err)
	}
	if p := f.last("usergroups.search"); formVal(p, "search_only_team") != "T2" {
		t.Errorf("usergroups search_only_team = %q", formVal(p, "search_only_team"))
	}
}

func TestConnectedOrgsAndEnterpriseTeams(t *testing.T) {
	a, f := testApp(t)
	f.reply["users.slackConnect.orgList"] = `{"ok":true,"orgs":[{"team_summary":{"id":"E9","name":"Partner Co",
		"icon":{"image_44":"https://x/44.png","image_88":"https://x/88.png"}}},{"team_summary":{"id":""}},
		{"team_summary":{"id":"T7","name":"Friends"}}]}`
	ids, err := a.ConnectedOrgs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(ids, []string{"E9", "T7"}) {
		t.Errorf("orgs %v", ids)
	}
	p := f.last("users.slackConnect.orgList")
	for k, want := range map[string]string{"connection_type": "member", "limit": "1000", "sort": "count"} {
		if formVal(p, k) != want {
			t.Errorf("orgList %s = %q, want %q", k, formVal(p, k), want)
		}
	}
	if name, icon, ok := a.Team("E9", 20); !ok || name != "Partner Co" || icon == "" {
		t.Errorf("E9: %q %q %v", name, icon, ok)
	}

	if got := a.EnterpriseTeams(); !slices.Equal(got, []string{"T1"}) {
		t.Errorf("no enterprise_user: %v, want the session's team", got)
	}
	a.C.Creds.EnterpriseID = "E1"
	a.S.PutUsers([]slack.User{{ID: "USELF", Name: "me", EnterpriseUser: &slack.EnterpriseUser{EnterpriseID: "E1", Teams: []string{"T3", "T1", "E1"}}}})
	if got := a.EnterpriseTeams(); !slices.Equal(got, []string{"T1", "T3"}) {
		t.Errorf("enterprise teams %v, want the session's first, then the rest without the org", got)
	}
}
