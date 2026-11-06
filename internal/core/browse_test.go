package core

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
)

// All fixtures are synthetic. User ids stay within the ones testApp knows
// (USELF, U1, U2) so EnsureUsers never reaches for the edge API.

func formVal(p url.Values, k string) string {
	if p == nil {
		return "<no call>"
	}
	return p.Get(k)
}

func TestFilesParamsAndParsing(t *testing.T) {
	a, f := testApp(t)
	f.reply["search.modules.files"] = `{"ok":true,"pagination":{"total_count":2,"page":1,"page_count":1},"items":[
		{"id":"F1","title":"Plan x","name":"plan","filetype":"quip","pretty_type":"Canvas","mode":"quip","user":"U1","created":100,"updated":200,"permalink":"https://x.example/F1"},
		{"id":"F2","name":"a.png","mimetype":"image/png","filetype":"png","user":"U2","size":10,"timestamp":50,"thumb_360":"https://x.example/t.png","thumb_360_w":360,"thumb_360_h":200,"channels":["C1"],"ims":["D1"]},
		{"id":"F3","title":"Todo","filetype":"list","mode":"list","user":"USELF"}]}`
	ctx := context.Background()
	page, err := a.Files(ctx, FilesOpts{})
	if err != nil {
		t.Fatal(err)
	}
	p := f.last("search.modules.files")
	if formVal(p, "query") != fileTypesAll || formVal(p, "sort") != "last_engaged" || formVal(p, "search_context") != "desktop_files_browser" ||
		formVal(p, "shared_with_you") != "0" || formVal(p, "search_only_my_channels") != "false" || formVal(p, "page") != "1" ||
		formVal(p, "module") != "files" || formVal(p, "count") != "50" || formVal(p, "client_req_id") == "" {
		t.Errorf("all-files params: %v", p)
	}
	if page.Page != 1 || page.Pages != 1 || page.Total != 2 || len(page.Files) != 3 {
		t.Fatalf("page: %+v", page)
	}
	f1, f2, f3 := page.Files[0], page.Files[1], page.Files[2]
	if f1.Title != "Plan x" || !f1.IsCanvas || f1.IsList || f1.IsImage || f1.Created != 100 || f1.Modified != 200 || f1.User != "U1" {
		t.Errorf("canvas: %+v", f1)
	}
	if !f2.IsImage || f2.IsCanvas || f2.Thumb != "https://x.example/t.png" || f2.ThumbW != 360 || f2.ThumbH != 200 ||
		f2.Created != 50 || strings.Join(f2.Channels, ",") != "C1,D1" || f2.Size != 10 {
		t.Errorf("image: %+v", f2)
	}
	if !f3.IsList || f3.IsCanvas {
		t.Errorf("list: %+v", f3)
	}

	cases := []struct {
		o                   FilesOpts
		query, sort, shared string
		mine                string
	}{
		{FilesOpts{Kind: "canvases"}, "type:quip", "last_engaged", "0", "false"},
		{FilesOpts{Kind: "lists", Query: " roadmap "}, "type:lists roadmap", "score", "0", "false"},
		{FilesOpts{Mine: true}, fileTypesOwned + " creator:<@USELF>", "timestamp", "0", "false"},
		{FilesOpts{Kind: "canvases", Mine: true, Page: 3}, "type:quip creator:<@USELF>", "timestamp", "0", "false"},
		{FilesOpts{SharedWithMe: true}, fileTypesOwned, "score", "1", "true"},
	}
	for _, c := range cases {
		if _, err := a.Files(ctx, c.o); err != nil {
			t.Fatal(err)
		}
		p := f.last("search.modules.files")
		if formVal(p, "query") != c.query || formVal(p, "sort") != c.sort || formVal(p, "shared_with_you") != c.shared ||
			formVal(p, "search_only_my_channels") != c.mine {
			t.Errorf("%+v: query=%q sort=%q shared=%q mine=%q", c.o, formVal(p, "query"), formVal(p, "sort"),
				formVal(p, "shared_with_you"), formVal(p, "search_only_my_channels"))
		}
		if c.o.Page == 3 && formVal(p, "page") != "3" {
			t.Errorf("page param %q", formVal(p, "page"))
		}
	}

	f.reply["search.modules.files"] = `{"ok":false,"error":"invalid_arguments"}`
	if _, err := a.Files(ctx, FilesOpts{}); err == nil {
		t.Error("error not returned")
	}
}

func TestDeletedFiles(t *testing.T) {
	a, f := testApp(t)
	f.reply["files.recentlyDeleted"] = `{"ok":true,"files":[{"id":"F9","name":"gone.txt","user":"U1","mode":"hosted","mimetype":"text/plain"}]}`
	page, err := a.DeletedFiles(context.Background())
	if err != nil || len(page.Files) != 1 || page.Files[0].ID != "F9" || page.Files[0].Name != "gone.txt" ||
		page.Total != 1 || page.Page != 1 || page.Pages != 1 {
		t.Fatalf("deleted: %+v %v", page, err)
	}
	f.reply["files.recentlyDeleted"] = `{"ok":true,"files":[]}`
	if page, err := a.DeletedFiles(context.Background()); err != nil || len(page.Files) != 0 || page.Total != 0 {
		t.Errorf("empty: %+v %v", page, err)
	}
}

func TestHuddleHistory(t *testing.T) {
	a, f := testApp(t)
	f.reply["huddles.history"] = `{"ok":true,"huddles":[
		{"id":"R1","name":"standup","created_by":"U1","date_start":100,"date_end":200,"participant_history":["U1","U2"],"channels":["C1"],"has_ended":true},
		{"id":"R2","created_by":"U2","date_start":300,"participants":["U2"],"participant_history":["U2"],"channels":["D1"],"is_dm_call":true},
		{"name":"no id"}],
		"response_metadata":{"next_cursor":"abc"}}`
	ctx := context.Background()
	hs, next, err := a.HuddleHistory(ctx, "")
	if err != nil || next != "abc" || len(hs) != 2 {
		t.Fatalf("history: %+v %q %v", hs, next, err)
	}
	if p := f.last("huddles.history"); formVal(p, "limit") != "30" || p.Has("cursor") {
		t.Errorf("first page params: %v", p)
	}
	h1, h2 := hs[0], hs[1]
	if h1.ID != "R1" || h1.Name != "standup" || h1.Active || h1.Start != 100 || h1.End != 200 ||
		strings.Join(h1.History, ",") != "U1,U2" || h1.Channels[0] != "C1" || h1.CreatedBy != "U1" {
		t.Errorf("ended huddle: %+v", h1)
	}
	if !h2.Active || !h2.DM || h2.Participants[0] != "U2" {
		t.Errorf("open huddle: %+v", h2)
	}
	if _, _, err := a.HuddleHistory(ctx, "abc"); err != nil || formVal(f.last("huddles.history"), "cursor") != "abc" {
		t.Errorf("cursor not sent: %v", err)
	}
}

func TestParseHuddle(t *testing.T) {
	room := `{"id":"R5","name":"pairing","created_by":"U1","date_start":10,"participants":["U1","U2"],"channels":["C1"]}`
	for name, in := range map[string]string{
		"event room":   `{"type":"sh_room_join","user":"U2","room":` + room + `}`,
		"event huddle": `{"type":"sh_room_update","huddle":` + room + `}`,
		"message room": room,
	} {
		h, ok := ParseHuddle(json.RawMessage(in))
		if !ok || h.ID != "R5" || h.Name != "pairing" || !h.Active || len(h.Participants) != 2 || h.Start != 10 {
			t.Errorf("%s: %+v %v", name, h, ok)
		}
	}
	ended, ok := ParseHuddle(json.RawMessage(`{"room":{"id":"R6","participants":["U1"],"date_end":20}}`))
	if !ok || ended.Active || ended.End != 20 {
		t.Errorf("ended: %+v %v", ended, ok)
	}
	for _, bad := range []string{``, `nope`, `{}`, `{"room":{}}`, `{"type":"sh_room_leave"}`, `[1]`} {
		if _, ok := ParseHuddle(json.RawMessage(bad)); ok {
			t.Errorf("parsed %q", bad)
		}
	}

	hs, err := parseActiveHuddles(json.RawMessage(`{"ok":true,"huddles":[{"id":"R7","channels":["C1"]},{"room":{"id":"R8"}},{"id":"R9","date_end":5},{}]}`))
	if err != nil || len(hs) != 3 || !hs[0].Active || hs[1].ID != "R8" || !hs[1].Active || hs[2].Active {
		t.Errorf("active: %+v %v", hs, err)
	}
	if hs, err := parseActiveHuddles(json.RawMessage(`{"ok":true,"huddles":[]}`)); err != nil || len(hs) != 0 {
		t.Errorf("empty active: %+v %v", hs, err)
	}
	if _, err := parseActiveHuddles(json.RawMessage(`not json`)); err == nil {
		t.Error("bad json accepted")
	}
}

func TestSent(t *testing.T) {
	a, f := testApp(t)
	f.reply["messages.sent.list"] = `{"ok":true,"pagination":{"total_count":3,"page":1,"per_page":25,"page_count":1,"next_cursor":"Qnwy"},"items":[
		{"msg_timestamp":"100.000001","ts":"100.000001","id":"C1","channel_id":"C1","user":"USELF"},
		{"msg_timestamp":"200.000002","channel":{"id":"D1"},"user":"USELF"},
		{"ts":"300.000003","channel_id":"C1","user":"USELF"}]}`
	f.reply["messages.list"] = `{"ok":true,"messages":{
		"C1":[{"ts":"100.000001","user":"USELF","text":"synthetic one"}],
		"D1":[{"ts":"200.000002","user":"USELF","text":"synthetic two","thread_ts":"150.000000"}]}}`
	ctx := context.Background()
	sp, err := a.Sent(ctx, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	p := f.last("messages.sent.list")
	if formVal(p, "cursor") != "*" || formVal(p, "page") != "1" || formVal(p, "count") != "25" || formVal(p, "view") != "channel_and_thread" ||
		formVal(p, "client_req_id") == "" || formVal(p, "search_session_id") == "" {
		t.Errorf("first page params: %v", p)
	}
	ids := formVal(f.last("messages.list"), "message_ids")
	if !strings.Contains(ids, "100.000001") || !strings.Contains(ids, "200.000002") || !strings.Contains(ids, "300.000003") {
		t.Errorf("hydrate ids: %s", ids)
	}
	// 300.000003 did not hydrate (deleted): dropped. Newest first.
	if len(sp.Messages) != 2 || sp.Messages[0].TS != "200.000002" || sp.Messages[0].Channel != "D1" ||
		sp.Messages[1].TS != "100.000001" || sp.Messages[1].Channel != "C1" || sp.Messages[0].Text != "synthetic two" {
		t.Fatalf("messages: %+v", sp.Messages)
	}
	if sp.Next != "Qnwy" || sp.Total != 3 || sp.Page != 1 {
		t.Errorf("paging: next=%q total=%d page=%d", sp.Next, sp.Total, sp.Page)
	}
	if sp.Convs["C1"].Name != "general" || sp.Convs["D1"].Kind != "im" {
		t.Errorf("convs: %+v", sp.Convs)
	}
	if _, err := a.Sent(ctx, "Qnwy", 2); err != nil {
		t.Fatal(err)
	}
	if p := f.last("messages.sent.list"); formVal(p, "cursor") != "Qnwy" || formVal(p, "page") != "2" {
		t.Errorf("second page params: %v", p)
	}
}

func TestBrowseChannelsWith(t *testing.T) {
	a, f := testApp(t)
	f.reply["search.modules.channels"] = `{"ok":true,"pagination":{"total_count":14919,"page":1,"page_count":299},"items":[
		{"id":"C1","name":"general","is_member":false,"member_count":5,"purpose":{"value":"talk"},"timestamp":1234},
		{"id":"G2","name":"secret","is_private":true,"is_archived":true,"member_count":2,"purpose":"hush","timestamp":"99"},
		{"id":"G3","name":"mpdm-x","is_mpim":true}]}`
	ctx := context.Background()
	pg, err := a.BrowseChannelsWith(ctx, ChannelBrowseOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if pg.Total != 14919 || pg.Pages != 299 || pg.Page != 1 || len(pg.Channels) != 2 {
		t.Fatalf("page: %+v", pg)
	}
	c1, g2 := pg.Channels[0], pg.Channels[1]
	if !c1.IsMember || c1.NumMembers != 5 || c1.Purpose != "talk" || c1.Created != 1234 || c1.Kind != "channel" {
		t.Errorf("C1: %+v", c1)
	}
	if g2.Kind != "private" || !g2.IsArchived || g2.Purpose != "hush" || g2.Created != 99 || g2.IsMember {
		t.Errorf("G2: %+v", g2)
	}
	p := f.last("search.modules.channels")
	for k, want := range map[string]string{"module": "channels", "browse": "standard", "search_context": "desktop_channel_browser",
		"sort": "recommended", "recommend_source": "channel-browser", "channel_type": "exclude_archived",
		"exclude_my_channels": "0", "search_only_my_channels": "false", "page": "1", "count": "50", "query": ""} {
		if formVal(p, k) != want {
			t.Errorf("default %s = %q, want %q", k, formVal(p, k), want)
		}
	}
	if p.Has("sort_dir") {
		t.Error("recommended sort sent sort_dir")
	}

	cases := []struct {
		o    ChannelBrowseOpts
		want map[string]string
	}{
		{ChannelBrowseOpts{Type: "public"}, map[string]string{"channel_type": "private_exclude"}},
		{ChannelBrowseOpts{Type: "private"}, map[string]string{"channel_type": "private"}},
		{ChannelBrowseOpts{Type: "archived"}, map[string]string{"channel_type": "archived"}},
		{ChannelBrowseOpts{Type: "external"}, map[string]string{"channel_type": "external_shared"}},
		{ChannelBrowseOpts{Type: "bogus"}, map[string]string{"channel_type": "exclude_archived"}},
		{ChannelBrowseOpts{Membership: "joined"}, map[string]string{"search_only_my_channels": "true", "exclude_my_channels": "0"}},
		{ChannelBrowseOpts{Membership: "other"}, map[string]string{"exclude_my_channels": "1", "search_only_my_channels": "false"}},
		{ChannelBrowseOpts{Sort: "name"}, map[string]string{"sort": "name", "sort_dir": "asc"}},
		{ChannelBrowseOpts{Sort: "name", Desc: true}, map[string]string{"sort": "name", "sort_dir": "desc"}},
		{ChannelBrowseOpts{Sort: "created", Desc: true}, map[string]string{"sort": "created", "sort_dir": "desc"}},
		{ChannelBrowseOpts{Sort: "created"}, map[string]string{"sort": "created", "sort_dir": "asc"}},
		{ChannelBrowseOpts{Sort: "member_count", Desc: true}, map[string]string{"sort": "member_count", "sort_dir": "desc"}},
		{ChannelBrowseOpts{Sort: "recommended", Query: "eng", Page: 4}, map[string]string{"sort": "recommended", "query": "eng", "page": "4"}},
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
		if formVal(p, "recommend_source") != "channel-browser" {
			t.Errorf("%+v: recommend_source missing", c.o)
		}
	}
}

func TestBrowsePeople(t *testing.T) {
	a, f := testApp(t)
	f.reply["search.modules.people"] = `{"ok":true,"pagination":{"total_count":2,"page":1,"page_count":1},"items":[
		{"id":"U7","name":"carol","profile":{"display_name":"Carol","real_name":"Carol C"}},
		{"id":"U8","name":"dave","deleted":true,"profile":{"real_name":"Dave D"}}]}`
	ctx := context.Background()
	pg, err := a.BrowsePeople(ctx, PeopleBrowseOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if pg.Total != 2 || pg.Page != 1 || pg.Pages != 1 || len(pg.Users) != 2 || pg.Users[0].Name != "Carol" || !pg.Users[1].Deleted {
		t.Fatalf("people: %+v", pg)
	}
	if u, ok := a.User("U7"); !ok || u.RealName != "Carol C" {
		t.Errorf("not cached: %+v %v", u, ok)
	}
	p := f.last("search.modules.people")
	for k, want := range map[string]string{"module": "people", "browse": "standard", "search_context": "desktop_people_browser",
		"sort": "recommended", "recommend_source": "people-browser", "hide_deactivated_users": "1", "custom_fields": "{}",
		"page": "1", "count": "50"} {
		if formVal(p, k) != want {
			t.Errorf("default %s = %q, want %q", k, formVal(p, k), want)
		}
	}
	if p.Has("account_type") || p.Has("sort_dir") {
		t.Errorf("default sent account_type/sort_dir: %v", p)
	}
	for at, want := range map[string]string{"owners": "0,1", "admins": "0,1,2", "members": "0,1,2,3,7", "guests": "4,5,8", "deactivated": "6"} {
		if _, err := a.BrowsePeople(ctx, PeopleBrowseOpts{AccountType: at}); err != nil {
			t.Fatal(err)
		}
		p := f.last("search.modules.people")
		hide := "1"
		if at == "deactivated" {
			hide = "0"
		}
		if formVal(p, "account_type") != want || formVal(p, "hide_deactivated_users") != hide {
			t.Errorf("%s: account_type=%q hide=%q", at, formVal(p, "account_type"), formVal(p, "hide_deactivated_users"))
		}
	}
	if _, err := a.BrowsePeople(ctx, PeopleBrowseOpts{Sort: "name", Desc: true, Query: "ca", Page: 2}); err != nil {
		t.Fatal(err)
	}
	if p := f.last("search.modules.people"); formVal(p, "sort") != "name" || formVal(p, "sort_dir") != "desc" || formVal(p, "query") != "ca" || formVal(p, "page") != "2" {
		t.Errorf("name sort: %v", p)
	}
}

func TestSearchUsergroups(t *testing.T) {
	a, f := testApp(t)
	f.reply["usergroups.search"] = `{"ok":true,"usergroups":[
		{"id":"S1","handle":"eng","name":"Engineering","description":"builders","user_count":"5"},
		{"id":"S2","handle":"ops","name":"Ops","user_count":7}],"num_found":2}`
	ctx := context.Background()
	gs, err := a.SearchUsergroups(ctx, UsergroupOpts{Query: "@en"})
	if err != nil || len(gs) != 2 {
		t.Fatalf("groups: %+v %v", gs, err)
	}
	if g := gs[0]; g.ID != "S1" || g.Handle != "eng" || g.Name != "Engineering" || g.Description != "builders" || g.UserCount != 5 || gs[1].UserCount != 7 {
		t.Errorf("groups: %+v", gs)
	}
	p := f.last("usergroups.search")
	for k, want := range map[string]string{"keyword": "en", "sort": "name", "sort_dir": "asc", "include_hidden": "false",
		"include_disabled": "true", "count": "50"} {
		if formVal(p, k) != want {
			t.Errorf("%s = %q, want %q", k, formVal(p, k), want)
		}
	}
	if _, err := a.SearchUsergroups(ctx, UsergroupOpts{Sort: "member_count", Desc: true, Count: 10}); err != nil {
		t.Fatal(err)
	}
	if p := f.last("usergroups.search"); formVal(p, "sort") != "member_count" || formVal(p, "sort_dir") != "desc" || formVal(p, "count") != "10" {
		t.Errorf("member sort: %v", p)
	}
	if _, err := a.SearchUsergroups(ctx, UsergroupOpts{Sort: "bogus"}); err != nil || formVal(f.last("usergroups.search"), "sort") != "name" {
		t.Errorf("bogus sort not normalized: %v", err)
	}
}

// tabsServer answers per-file files.info calls, which the shared fake
// (keyed by method only) cannot.
func tabsServer(t *testing.T, a *App) (calls func(method string) []url.Values) {
	var mu sync.Mutex
	seen := map[string][]url.Values{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		mu.Lock()
		seen[method] = append(seen[method], r.PostForm)
		mu.Unlock()
		body := `{"ok":false,"error":"not_in_test"}`
		switch method {
		case "conversations.info":
			body = `{"ok":true,"channel":{"id":"C1","properties":{"tabs":[
				{"id":"Ct1","type":"list","label":"","data":{"file_id":"FL1","shared_ts":"1.0"}},
				{"id":"Ct2","type":"canvas","label":"","data":{"file_id":"FGONE"}},
				{"id":"Ct3","type":"canvas","label":"","data":{"file_id":"FC3"}},
				{"id":"Ct4","type":"canvas","label":"","data":{"file_id":"FTOMB"}},
				{"id":"Ct5","type":"folder","label":""},
				{"id":"Ct6","type":"folder","label":"Links"},
				{"id":"Ct7","type":"canvas","label":"","data":{"file_id":"FERR"}},
				{"id":"bookmarks","type":"bookmarks","label":"","is_disabled":true},
				{"id":"files","type":"files","label":"","is_disabled":true},
				{"id":"workflows","type":"workflows","label":"","is_disabled":true},
				{"id":"channel_canvas","type":"channel_canvas","label":"Canvas","is_disabled":true}]},
				"properties_tabz":[]}}`
		case "files.info":
			switch r.PostForm.Get("file") {
			case "FL1":
				body = `{"ok":true,"file":{"id":"FL1","title":"Ideas","filetype":"list","mode":"list"}}`
			case "FC3":
				body = `{"ok":true,"file":{"id":"FC3","title":"","name":"Goals","filetype":"quip"}}`
			case "FGONE":
				body = `{"ok":false,"error":"file_not_found"}`
			case "FTOMB":
				body = `{"ok":true,"file":{"id":"FTOMB","mode":"tombstone"}}`
			case "FERR":
				body = `{"ok":false,"error":"internal_error"}`
			}
		case "bookmarks.list":
			body = `{"ok":true,"bookmarks":[
				{"id":"Bk1","title":"Docs","link":"https://x.example/docs","type":"link","rank":"b","parent_id":"Ct5"},
				{"id":"Bk2","title":"Board","link":"https://x.example/board","type":"link","rank":"a"}]}`
		case "conversations.removeTab":
			body = `{"ok":true}`
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	a.C = slack.New(slack.Creds{Token: "xoxc-test", UserID: "USELF", Team: "T1", TeamURL: srv.URL + "/"})
	return func(method string) []url.Values {
		mu.Lock()
		defer mu.Unlock()
		return seen[method]
	}
}

func TestTabs(t *testing.T) {
	a, _ := testApp(t)
	calls := tabsServer(t, a)
	ctx := context.Background()
	tabs, err := a.Tabs(ctx, "C1")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, tb := range tabs {
		got = append(got, tb.ID+":"+tb.Type+":"+tb.Title)
	}
	want := "Ct1:list:Ideas|Ct2:canvas:Deleted file|Ct3:canvas:Goals|Ct4:canvas:Deleted file|Ct5:folder:Bookmarks|Ct6:folder:Links|Ct7:canvas:Untitled"
	if strings.Join(got, "|") != want {
		t.Errorf("tabs:\n got %s\nwant %s", strings.Join(got, "|"), want)
	}
	if len(tabs) != 7 {
		t.Fatalf("tabs: %+v", tabs)
	}
	if tabs[0].FileID != "FL1" || tabs[0].Deleted || !tabs[1].Deleted || !tabs[3].Deleted || tabs[6].Deleted {
		t.Errorf("file flags: %+v", tabs)
	}
	// Ct5 owns Bk1 by parent_id; Ct6 has no children, so it gets all (rank order).
	if len(tabs[4].Bookmarks) != 1 || tabs[4].Bookmarks[0].ID != "Bk1" {
		t.Errorf("folder Ct5: %+v", tabs[4].Bookmarks)
	}
	if len(tabs[5].Bookmarks) != 2 || tabs[5].Bookmarks[0].ID != "Bk2" {
		t.Errorf("folder Ct6: %+v", tabs[5].Bookmarks)
	}
	if n := len(calls("bookmarks.list")); n != 1 {
		t.Errorf("bookmarks.list called %d times", n)
	}
	if n := len(calls("files.info")); n != 5 {
		t.Errorf("files.info called %d times", n)
	}
	if ci := calls("conversations.info"); len(ci) != 1 || ci[0].Get("channel") != "C1" {
		t.Errorf("conversations.info: %v", ci)
	}

	if err := a.RemoveTab(ctx, "C1", "Ct5"); err != nil {
		t.Fatal(err)
	}
	if rt := calls("conversations.removeTab"); len(rt) != 1 || rt[0].Get("id") != "Ct5" || rt[0].Get("channel_id") != "C1" {
		t.Errorf("removeTab: %v", rt)
	}
}

func TestTabsNoFolderSkipsBookmarks(t *testing.T) {
	a, f := testApp(t)
	f.reply["conversations.info"] = `{"ok":true,"channel":{"id":"C1","properties":{"tabs":[
		{"id":"files","type":"files","label":""},{"id":"bookmarks","type":"bookmarks","label":"","is_disabled":true}]}}}`
	tabs, err := a.Tabs(context.Background(), "C1")
	if err != nil || len(tabs) != 1 || tabs[0].Title != "Files" {
		t.Fatalf("tabs: %+v %v", tabs, err)
	}
	if f.last("bookmarks.list") != nil || f.last("files.info") != nil {
		t.Error("needless calls")
	}
	f.reply["conversations.info"] = `{"ok":true,"channel":{"id":"C1"}}`
	if tabs, err := a.Tabs(context.Background(), "C1"); err != nil || len(tabs) != 0 {
		t.Errorf("no properties: %+v %v", tabs, err)
	}
}

func TestMemberCountsParse(t *testing.T) {
	c, err := parseMemberCounts(json.RawMessage(`{"ok":true,"counts":{"everyone":34,"people":17,"members":17,"guests":1,"bots":17,"apps":16,"by_team":{"E1":17}}}`))
	if err != nil || c != (MemberCounts{People: 17, Everyone: 34, Bots: 17, Apps: 16, Guests: 1}) {
		t.Errorf("counts: %+v %v", c, err)
	}
	if c, _ := parseMemberCounts(json.RawMessage(`{"counts":{"members":9}}`)); c.People != 9 {
		t.Errorf("members fallback: %+v", c)
	}
	if _, err := parseMemberCounts(json.RawMessage(`x`)); err == nil {
		t.Error("bad json accepted")
	}
}

func TestManagers(t *testing.T) {
	a, f := testApp(t)
	f.reply["admin.roles.entity.listAssignments"] = `{"ok":true,"role_assignments":[
		{"role_id":"Rl0A","users":["U1","U2","U1",""]},{"role_id":"Rl0B","users":["USELF"]}]}`
	ms, err := a.Managers(context.Background(), "C1")
	if err != nil || strings.Join(ms, ",") != "U1,U2" {
		t.Fatalf("managers: %v %v", ms, err)
	}
	if formVal(f.last("admin.roles.entity.listAssignments"), "entity_id") != "C1" {
		t.Error("entity_id")
	}
	f.reply["admin.roles.entity.listAssignments"] = `{"ok":true,"role_assignments":[]}`
	if ms, err := a.Managers(context.Background(), "C1"); err != nil || ms == nil || len(ms) != 0 {
		t.Errorf("none: %#v %v", ms, err)
	}
}

func TestLooseJSON(t *testing.T) {
	for in, want := range map[string]int64{`5`: 5, `"7"`: 7, `"2.9"`: 2, `null`: 0, ``: 0, `"x"`: 0, `{}`: 0} {
		if got := looseInt(json.RawMessage(in)); got != want {
			t.Errorf("looseInt(%s) = %d", in, got)
		}
	}
	for in, want := range map[string]string{`"a"`: "a", `{"value":"b"}`: "b", `null`: "", `3`: "3"} {
		if got := looseText(json.RawMessage(in)); got != want {
			t.Errorf("looseText(%s) = %q", in, got)
		}
	}
}
