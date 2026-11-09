package core

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/fakeslack"
)

// wantForm checks p against want; "*" means any non-empty value.
func wantForm(t *testing.T, name string, p url.Values, want map[string]string) {
	t.Helper()
	for k, v := range want {
		got, ok := p[k]
		switch {
		case !ok:
			t.Errorf("%s: %s missing", name, k)
		case v == "*":
			if len(got) != 1 || got[0] == "" {
				t.Errorf("%s: %s = %q, want a value", name, k, got)
			}
		case len(got) != 1 || got[0] != v:
			t.Errorf("%s: %s = %q, want %q", name, k, got, v)
		}
	}
	for k := range p {
		if _, ok := want[k]; !ok {
			t.Errorf("%s: extra arg %s=%q (Slack doesn't send it)", name, k, p.Get(k))
		}
	}
}

// base is the searchModules fetcher's args for every module.
func base(module, query, count, sctx string) map[string]string {
	return map[string]string{"module": module, "query": query, "page": "1", "client_req_id": "*", "search_session_id": "*",
		"extracts": "1", "highlight": "1", "max_extract_len": "200", "extra_message_data": "1", "no_user_profile": "1",
		"count": count, "file_title_only": "false", "query_rewrite_disabled": "false", "include_files_shares": "1",
		"search_context": sctx}
}

func merge(a map[string]string, b map[string]string) map[string]string {
	for k, v := range b {
		a[k] = v
	}
	return a
}

// The results page's module calls carry exactly the args app.slack.com
// sent for "test" (captured 2026-11-08), one session for all of them.
func TestSearchModuleParams(t *testing.T) {
	s := NewSearchSession()
	rc := `{"active_cid":"","recent_filter_in":[],"recent_filter_from":[]}`
	wantForm(t, "messages", messageSearchParams(SearchOpts{Query: "test", Session: s}, false, false),
		merge(base("messages", "test", "20", "desktop_messages_tab"), map[string]string{
			"search_exclude_bots": "false", "search_only_my_channels": "false", "spell_correction": "FUZZY_MATCH",
			"search_only_team": "", "facets_result_count": "5", "query_refinement_suggestions_version": "1",
			"recent_channels": "", "sort": "score", "sort_dir": "desc", "max_filter_suggestions": "10",
			"request_context": rc, "search_tab_filter": "messages", "search_tab_sort": "score"}))
	p := messageSearchParams(SearchOpts{Query: "test", Session: s, Sort: "oldest", ExactOnly: true,
		RecentChannels: []string{"C1", "D2"}, ActiveCID: "C1"}, true, true)
	for k, v := range map[string]string{"sort": "timestamp", "sort_dir": "asc", "search_exact_matches_only": "true",
		"search_exclude_bots": "true", "search_only_my_channels": "true", "recent_channels": "C1,D2", "search_tab_sort": "timestamp"} {
		if p.Get(k) != v {
			t.Errorf("messages (prefs on, oldest, exact): %s = %q, want %q", k, p.Get(k), v)
		}
	}
	var ctxJSON map[string]any
	if json.Unmarshal([]byte(p.Get("request_context")), &ctxJSON) != nil || ctxJSON["active_cid"] != "C1" {
		t.Errorf("request_context = %s", p.Get("request_context"))
	}
	if p.Get("search_session_id") != s.ID || p.Get("client_req_id") != s.ReqID {
		t.Error("the session ids aren't the search's")
	}
	wantForm(t, "dms", messageSearchParams(SearchOpts{Query: "test", Module: ModDMs, Session: s}, true, true),
		base("dms", "test", "40", "desktop_dms_tab"))
	files := merge(base("files", "test", "0", "desktop_files_tab"), map[string]string{
		"search_exclude_bots": "false", "search_only_my_channels": "false", "search_only_team": "", "facets_result_count": "5",
		"sort": "timestamp", "sort_dir": "desc", "max_filter_suggestions": "10", "request_context": rc,
		"search_tab_filter": "messages", "search_tab_sort": "score"})
	wantForm(t, "files", fileSearchParams(FileSearchOpts{Query: "test", CountOnly: true, Session: s}, false, false), files)
	canv := merge(map[string]string{}, files)
	canv["query"], canv["search_context"] = "type:quip test", "desktop_canvases_tab"
	wantForm(t, "canvases", fileSearchParams(FileSearchOpts{Query: "test", Canvases: true, CountOnly: true, Session: s}, false, false), canv)
	wantForm(t, "channels", channelSearchParams("test", 1, s, false), merge(base("channels", "test", "20", "desktop_channels_tab"),
		map[string]string{"search_only_my_channels": "false", "channel_type": "all", "external_shared_team_ids": "",
			"exclude_my_channels": "0", "request_context": rc, "spell_correction": "FUZZY_MATCH", "sort": "relevant",
			"sort_dir": "desc", "search_only_team": ""}))
	wantForm(t, "people", peopleSearchParams("test", 1, s), merge(base("people", "test", "20", "desktop_people_tab"),
		map[string]string{"tz": "", "external_shared_team_ids": "", "custom_fields": "{}", "hide_deactivated_users": "0",
			"request_context": rc, "spell_correction": "FUZZY_MATCH", "sort": "relevant", "sort_dir": "desc",
			"search_only_team": "", "profile_location": "", "profile_title": "", "profile_division": "",
			"profile_department": "", "profile_celebration": ""}))
	// the popover's Recent messages
	now := time.Unix(1794199974, 0)
	wantForm(t, "inline", inlineParams(InlineOpts{Query: "test", RecentChannels: []string{"C1", "D2"}, Session: s, Now: now}),
		map[string]string{"search_session_id": s.ID, "client_req_id": s.ReqID, "max_ts": "1794199974", "count": "3",
			"page": "1", "query": "test", "thread_replies": "", "extract_len": "110", "recent_channels": "C1,D2",
			"from_me": "true", "with_me": "true"})
	// search in channel (⌘F): the channel's in: replaces any other
	wantForm(t, "in channel", messageSearchParams(SearchOpts{Query: "in:<#C9|x> deploy", InChannel: "C1", Sort: "timestamp", Session: s}, true, true),
		merge(base("messages", "in:<#C1> deploy", "10", "search_in_channel"), map[string]string{
			"search_exclude_bots": "false", "sort": "timestamp", "sort_dir": "desc"}))
}

// The fake answers in app.slack.com's shapes: two pages for the
// fixture word, highlight markers stripped, filter suggestions, the
// modifiers, the prefs and search.inline's extracts.
func TestSearchAgainstFake(t *testing.T) {
	a, fs := canvasApp(t)
	ctx := context.Background()
	r, err := a.SearchMessages(ctx, SearchOpts{Query: fakeslack.SearchWord})
	if err != nil {
		t.Fatal(err)
	}
	if r.Total != fakeslack.SearchFixtureCount || r.Pages != 2 || r.Page != 1 || len(r.Messages) != 20 {
		t.Fatalf("page 1: total %d pages %d page %d, %d messages", r.Total, r.Pages, r.Page, len(r.Messages))
	}
	for _, m := range r.Messages {
		if strings.ContainsAny(m.Text, "") {
			t.Fatalf("highlight markers left in %q", m.Text)
		}
		if !strings.Contains(m.Text, fakeslack.SearchWord) || m.Permalink == "" || m.Channel == "" {
			t.Fatalf("result %+v", m)
		}
	}
	if len(r.FilterFrom) == 0 || len(r.FilterIn) == 0 || r.FilterIn[0].Count == 0 {
		t.Errorf("filter_suggestions: in %v from %v", r.FilterIn, r.FilterFrom)
	}
	r2, err := a.SearchMessages(ctx, SearchOpts{Query: fakeslack.SearchWord, Page: 2})
	if err != nil || r2.Page != 2 || len(r2.Messages) != fakeslack.SearchFixtureCount-20 {
		t.Fatalf("page 2: %v page %d, %d messages", err, r2.Page, len(r2.Messages))
	}
	th, _ := a.SearchMessages(ctx, SearchOpts{Query: fakeslack.SearchWord + " is:thread"})
	if th.Total == 0 || th.Total >= r.Total {
		t.Errorf("is:thread: %d of %d", th.Total, r.Total)
	}
	for _, m := range th.Messages {
		if m.ReplyCount == 0 {
			t.Errorf("is:thread result without replies: %+v", m)
		}
	}
	old, _ := a.SearchMessages(ctx, SearchOpts{Query: fakeslack.SearchWord, Sort: "oldest"})
	if len(old.Messages) == 0 || old.Messages[0].TS > old.Messages[len(old.Messages)-1].TS {
		t.Error("oldest isn't oldest first")
	}
	none, _ := a.SearchMessages(ctx, SearchOpts{Query: fakeslack.SearchNothing})
	if none.Total != 0 || len(none.Messages) != 0 {
		t.Errorf("no-results query: %d", none.Total)
	}
	dms, _ := a.SearchMessages(ctx, SearchOpts{Query: fakeslack.SearchWord, Module: ModDMs})
	if dms.Total != 1 || !strings.HasPrefix(dms.Messages[0].Channel, "D") {
		t.Errorf("dms: %+v", dms)
	}
	// the prefs go out as Slack's params
	if err := a.SetPref(ctx, "search_exclude_bots", true); err != nil {
		t.Fatal(err)
	}
	_, _ = a.SearchMessages(ctx, SearchOpts{Query: fakeslack.SearchWord, ExactOnly: true})
	forms := fs.SearchForms("search.modules.messages")
	last := forms[len(forms)-1]
	if last.Get("search_exclude_bots") != "true" || last.Get("search_exact_matches_only") != "true" {
		t.Errorf("prefs not sent: %v", last)
	}

	in, err := a.SearchInline(ctx, InlineOpts{Query: fakeslack.SearchWord})
	if err != nil || len(in.Messages) != 3 || in.Total != fakeslack.SearchFixtureCount {
		t.Fatalf("inline: %v %d of %d", err, len(in.Messages), in.Total)
	}
	if strings.ContainsAny(in.Messages[0].Text, "") || in.Messages[0].User == "" {
		t.Errorf("inline hit %+v", in.Messages[0])
	}
	files, err := a.AutocompleteFiles(ctx, "zephyr")
	if err != nil || len(files) != 1 || files[0].Title != fakeslack.SearchFileTitle {
		t.Errorf("autocomplete.files: %v %+v", err, files)
	}
	fp, err := a.SearchFiles(ctx, FileSearchOpts{Query: "zephyr"})
	if err != nil || fp.Total != 1 || len(fp.Files) != 1 {
		t.Errorf("files tab: %v %+v", err, fp)
	}
	n, err := a.ModuleCount(ctx, ModFiles, "zephyr", NewSearchSession())
	if err != nil || n != 1 {
		t.Errorf("files count %d %v", n, err)
	}
	if err := a.SaveSearch(ctx, " zephyr "); err != nil {
		t.Fatal(err)
	}
	if s := fs.SavedSearches(); len(s) != 1 || s[0] != "zephyr" {
		t.Errorf("search.save terms %v", s)
	}
}
