package core

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/some-du6e/clack/internal/fakeslack"
)

// The saved views and the bulk writes (activity_views.go) against
// internal/fakeslack (Size.Views). Synthetic content only.

func actLastWrite(t *testing.T, fs *fakeslack.Server, method string) fakeslack.ActivityWrite {
	t.Helper()
	ws := fs.ActivityWrites()
	for i := len(ws) - 1; i >= 0; i-- {
		if ws[i].Method == method {
			return ws[i]
		}
	}
	t.Fatalf("no %s write in %+v", method, ws)
	return fakeslack.ActivityWrite{}
}

// createActivityViewsByApi, updateActivityViewsByApi (rename, save the
// filters), deleteActivityViewsByApi and hide / unhide round-trip through
// activity.views like Slack's server, with its errors.
func TestActivityViewsRoundTrip(t *testing.T) {
	a, fs := actFake(t)
	ctx := context.Background()
	st, err := a.ActivityViewsState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Views) != 4 || st.Views[0].Type != "all" || len(st.Hidden) != 0 || st.Sort != "newest" || st.Density != "compact" {
		t.Fatalf("first read %+v", st)
	}
	v, err := a.CreateActivityView(ctx, ActivityView{Name: "Synthetic reactions", Emoji: ":tada:", Density: "compact",
		Filters: ActivityViewFilters{EntryTypes: []string{"message_reaction"}, UnreadOnly: true}})
	if err != nil || v.ID == "" || v.Type != "custom" || v.Name != "Synthetic reactions" || v.Emoji != ":tada:" {
		t.Fatalf("create: %+v %v", v, err)
	}
	w := actLastWrite(t, fs, "activity.views.create")
	if w.Form.Get("sort") != "newest" || w.Form.Get("density") != "compact" || w.Form.Get("emoji") != ":tada:" ||
		w.Form.Get("filters") != `{"entry_types":["message_reaction"],"unread_only":true}` || w.Form.Get("reason") != "[Activity Views] Create views by API" {
		t.Errorf("create form %v", w.Form)
	}
	if !v.Filters.UnreadOnly || len(v.Filters.EntryTypes) != 1 {
		t.Errorf("created filters %+v", v.Filters)
	}
	if _, err := a.CreateActivityView(ctx, ActivityView{Name: "Synthetic reactions"}); err == nil || !strings.Contains(err.Error(), "duplicate_name") {
		t.Errorf("a taken name: %v, want duplicate_name", err)
	}
	if _, err := a.CreateActivityView(ctx, ActivityView{Name: "Mentions"}); err == nil || !strings.Contains(err.Error(), "cannot_use_default_view_name") {
		t.Errorf("a default name: %v", err)
	}
	r, err := a.RenameActivityView(ctx, v, "Synthetic party", "")
	if err != nil || r.Name != "Synthetic party" || r.Emoji != "" {
		t.Fatalf("rename: %+v %v", r, err)
	}
	if w := actLastWrite(t, fs, "activity.views.update"); w.Form.Get("view_id") != v.ID || w.Form.Get("name") != "Synthetic party" || !w.Form.Has("emoji") || w.Form.Has("filters") {
		t.Errorf("rename form %v", w.Form)
	}
	r.Filters = ActivityViewFilters{PriorityOnly: true}
	if _, err := a.SaveActivityViewFilters(ctx, r); err != nil {
		t.Fatal(err)
	}
	w = actLastWrite(t, fs, "activity.views.update")
	if got := w.Form.Get("filters"); got != `{"entry_types":null,"channel_section_ids":null,"channel_ids":null,"archive_only":false,"unread_only":false,"priority_only":true,"only_salesforce_channels":false,"exclude_automations":false,"automations_only":false}` {
		t.Errorf("save filters %s", got)
	}
	if err := a.HideActivityView(ctx, "threads"); err != nil {
		t.Fatal(err)
	}
	st, _ = a.ActivityViewsState(ctx)
	if len(st.Hidden) != 1 || st.Hidden[0] != "threads" || len(st.Views) != 4 || st.Views[3].ID != v.ID || !st.Views[3].Filters.PriorityOnly {
		t.Fatalf("after hide: %+v", st)
	}
	if err := a.UnhideActivityView(ctx, "threads"); err != nil {
		t.Fatal(err)
	}
	if err := a.DeleteActivityView(ctx, v.ID); err != nil {
		t.Fatal(err)
	}
	st, _ = a.ActivityViewsState(ctx)
	if len(st.Views) != 4 || len(st.Hidden) != 0 {
		t.Errorf("after unhide and delete: %+v", st)
	}
	if err := a.DeleteActivityView(ctx, st.Views[0].ID); err == nil {
		t.Error("deleting the All view worked")
	}
	if err := a.SetActivityPrefs(ctx, "vip_unreads_first", ""); err != nil {
		t.Fatal(err)
	}
	if w := actLastWrite(t, fs, "activity.prefs.update"); w.Form.Get("sort") != "vip_unreads_first" || w.Form.Has("density") {
		t.Errorf("prefs form %v", w.Form)
	}
	if st, _ := a.ActivityViewsState(ctx); st.Sort != "vip_unreads_first" {
		t.Errorf("sort after the write: %q", st.Sort)
	}
}

// activity_views_updated carries the views; one without any is ignored.
func TestActivityViewsEvent(t *testing.T) {
	raw := json.RawMessage(`{"type":"activity_views_updated","views":[{"id":"V1","view_type":"all","position":"0"},
		{"id":"V2","view_type":"custom","name":"Synthetic","emoji":":tada:","position":3,"filters":{"entry_types":["thread_v2"]}}]}`)
	st, ok := ActivityViewsEvent(raw)
	if !ok || len(st.Views) != 2 || st.Views[1].Position != "3" || st.Views[1].Filters.EntryTypes[0] != "thread_v2" {
		t.Fatalf("%+v %v", st, ok)
	}
	if strings.Join(st.Hidden, ",") != "dms,mentions,threads" {
		t.Errorf("hidden %v", st.Hidden)
	}
	if _, ok := ActivityViewsEvent(json.RawMessage(`{"type":"activity_views_updated"}`)); ok {
		t.Error("an event without views was read")
	}
}

// The bulk buttons and keys send one write with every entry; Clear
// everything sends the view's filters to activity.clearAll; the VIP
// filter and a view's sections reach activity.feed.
func TestActivityBulkAndFilters(t *testing.T) {
	a, fs := actFake(t)
	ctx := context.Background()
	page, err := a.Activity(ctx, ActivityOpts{View: "all", Priority: true})
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, it := range page.Items {
		keys = append(keys, it.Key)
		if !it.Priority {
			t.Errorf("%s: not a priority entry", it.Key)
		}
	}
	if strings.Join(keys, ",") != "fa-dm,fa-at-user" {
		t.Fatalf("priority_only: %v", keys)
	}
	if err := a.MarkActivityReadBulk(ctx, page.Items...); err != nil {
		t.Fatal(err)
	}
	w := actLastWrite(t, fs, "activity.markRead")
	if w.Form.Get("reason") != "bulk_mark_read" || w.Form.Get("entries") != `[{"type":"dm","key":"fa-dm","feed_ts":"1762700700.000100"},{"type":"at_user","key":"fa-at-user","feed_ts":"1762700100.000100"}]` {
		t.Errorf("bulk read %v", w.Form)
	}
	if err := a.MarkActivityUnreadBulk(ctx, page.Items[0]); err != nil {
		t.Fatal(err)
	}
	if w := actLastWrite(t, fs, "activity.markUnread"); w.Form.Get("reason") != "bulk_mark_unread" ||
		w.Form.Get("entries") != `[{"type":"dm","key":"fa-dm","ts":"1762700700.000100","feed_ts":"1762700700.000100"}]` {
		t.Errorf("bulk unread %v", w.Form)
	}
	if err := a.ClearActivityBulk(ctx, page.Items[1]); err != nil {
		t.Fatal(err)
	}
	if w := actLastWrite(t, fs, "activity.archive"); w.Form.Get("reason") != "bulk_archive" || !strings.Contains(w.Form.Get("entries"), `"key":"fa-at-user"`) {
		t.Errorf("bulk clear %v", w.Form)
	}
	if got := fs.ActivityArchived(); strings.Join(got, ",") != "fa-at-user" {
		t.Errorf("archived %v", got)
	}
	err = a.ClearAllActivity(ctx, ActivityClearFilters{EntryTypes: []string{"at_user_group", "at_channel"}, ReadState: "reads_and_unreads"})
	if err != nil {
		t.Fatal(err)
	}
	if w := actLastWrite(t, fs, "activity.clearAll"); w.Form.Get("reason") != "server-side-select-all-clear" ||
		w.Form.Get("filters") != `{"entry_types":["at_user_group","at_channel"],"read_state":"reads_and_unreads"}` {
		t.Errorf("clear all %v", w.Form)
	}
	if got := fs.ActivityArchived(); strings.Join(got, ",") != "fa-at-channel,fa-at-group,fa-at-user" {
		t.Errorf("archived after clearAll %v", got)
	}
	got := make(chan string, 1)
	fs.SetHook(func(method string, f url.Values) (any, bool) {
		if method == "activity.feed" {
			select {
			case got <- strings.Join(f["channel_section_ids"], "") + "|" + strings.Join(f["mode"], ""):
			default:
			}
		}
		return nil, false
	})
	if _, err := a.Activity(ctx, ActivityOpts{View: "all", SectionIDs: []string{"L0FAKE00"}, Mode: "priority_unreads_first_v1"}); err != nil {
		t.Fatal(err)
	}
	if s := <-got; s != "L0FAKE00|priority_unreads_first_v1" {
		t.Errorf("feed sections and mode %q", s)
	}
}
