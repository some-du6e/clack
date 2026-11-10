package core

// Adding to lists (lists_add.go) against internal/fakeslack: synthetic
// data only.

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"testing"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/lists"
)

// firstMessage is the newest message of ch's fake history.
func firstMessage(t *testing.T, a *App, ch string) string {
	t.Helper()
	var r struct {
		Messages []struct {
			TS string `json:"ts"`
		} `json:"messages"`
	}
	if err := a.C.Call(context.Background(), "conversations.history", url.Values{"channel": {ch}, "limit": {"1"}}, &r); err != nil || len(r.Messages) == 0 {
		t.Fatalf("history of %s: %v %+v", ch, err, r)
	}
	return r.Messages[0].TS
}

// "Add to list…": lists.addToList with Slack's arguments; the item holds
// the message, and a list without a message column gets one.
func TestAddToList(t *testing.T) {
	a, fs := canvasApp(t)
	ctx := context.Background()
	ch := fakeslack.ListTabChan
	ts := firstMessage(t, a, ch)

	rs, pages, err := a.SearchLists(ctx, "", 1)
	if err != nil || pages != 1 {
		t.Fatalf("SearchLists: %v pages=%d", err, pages)
	}
	seen := map[string]bool{}
	for _, r := range rs {
		seen[r.ID] = true
		if !r.IsList {
			t.Errorf("%s isn't a list", r.ID)
		}
	}
	if !seen[fakeslack.ListKitchen] || !seen[fakeslack.ListTodos] {
		t.Fatalf("explorer rows %v", seen)
	}

	rec, err := a.AddToList(ctx, fakeslack.ListKitchen, ch, ts)
	if err != nil {
		t.Fatal(err)
	}
	calls := fs.AddToListCalls()
	if len(calls) != 1 {
		t.Fatalf("lists.addToList calls %d", len(calls))
	}
	if f := calls[0]; f.Get("list_id") != fakeslack.ListKitchen || f.Get("channel_id") != ch || f.Get("message_ts") != ts || f.Get("reason") != "add-to-list" {
		t.Errorf("lists.addToList form %v", f)
	}
	fl, ok := fs.List(fakeslack.ListKitchen)
	if !ok {
		t.Fatal("no kitchen list")
	}
	var got *lists.Record
	for i := range fl.Records {
		if fl.Records[i].ID == rec.ID {
			got = &fl.Records[i]
		}
	}
	if got == nil {
		t.Fatalf("record %s not in the list", rec.ID)
	}
	var msg *lists.Field
	for i := range got.Fields {
		if len(got.Fields[i].Message) > 0 {
			msg = &got.Fields[i]
		}
	}
	if msg == nil || msg.Message[0].TS != ts || msg.Message[0].ChannelID != ch {
		t.Errorf("message cell %+v", msg)
	}

	// the to-do list has no message column: Slack adds one
	before, _ := fs.List(fakeslack.ListTodos)
	for _, c := range before.Meta.Schema {
		if c.Type == lists.Message {
			t.Fatal("the to-do fixture already has a message column")
		}
	}
	if _, err := a.AddToList(ctx, fakeslack.ListTodos, ch, ts); err != nil {
		t.Fatal(err)
	}
	after, _ := fs.List(fakeslack.ListTodos)
	hasMsg := false
	for _, c := range after.Meta.Schema {
		hasMsg = hasMsg || c.Type == lists.Message
	}
	if !hasMsg {
		t.Error("no message column after lists.addToList")
	}

	// a message that isn't there
	if _, err := a.AddToList(ctx, fakeslack.ListKitchen, ch, "1.000001"); err == nil {
		t.Error("lists.addToList of a missing message succeeded")
	}

	// openRecord's lists.records.open
	if err := a.OpenListRecord(ctx, fakeslack.ListKitchen, rec.ID); err != nil {
		t.Fatal(err)
	}
	if oc := fs.OpenRecordCalls(); len(oc) != 1 || oc[0].Get("id") != rec.ID || oc[0].Get("reason") != "open-record" {
		t.Errorf("lists.records.open %v", oc)
	}

	// "Create New List": the add_to_new_list template has a message column
	id, err := a.CreateListWith(ctx, CreateListOpts{TemplateID: "add_to_new_list", Reason: "create-list-from-add-to-new-list"})
	if err != nil {
		t.Fatal(err)
	}
	_, meta, err := a.ListHeader(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	hasMsg = false
	for _, c := range meta.Schema {
		hasMsg = hasMsg || c.Type == lists.Message
	}
	if !hasMsg {
		t.Errorf("add_to_new_list schema %+v", meta.Schema)
	}
	if _, _, err := a.ListHeader(ctx, "F0NOSUCHLIST"); err == nil {
		t.Error("ListHeader of a missing list succeeded")
	}
}

// The channel tab bar's + > List: conversations.addTab with the list,
// its default view and the access the channel gets.
func TestAddListTab(t *testing.T) {
	a, fs := canvasApp(t)
	ctx := context.Background()
	ch := fakeslack.ListTabChan
	info, _, err := a.ListHeader(ctx, fakeslack.ListKitchen)
	if err != nil {
		t.Fatal(err)
	}
	if ListSharedWith(info, ch) {
		t.Fatal("the kitchen list starts shared with the tab channel")
	}
	if err := a.AddListTab(ctx, ch, fakeslack.ListKitchen, "View0ALL001", "write", ""); err != nil {
		t.Fatal(err)
	}
	calls := fs.AddTabCalls()
	if len(calls) != 1 {
		t.Fatalf("conversations.addTab calls %d", len(calls))
	}
	f := calls[0]
	var data map[string]string
	if err := json.Unmarshal([]byte(f.Get("data")), &data); err != nil {
		t.Fatal(err)
	}
	if f.Get("channel_id") != ch || f.Get("type") != "list" || f.Get("reason") != "add-channel-tab" ||
		data["file_id"] != fakeslack.ListKitchen || data["access_level"] != "write" ||
		data["lists_channel_tab_default_refine_view_id"] != "View0ALL001" {
		t.Errorf("conversations.addTab form %v data %v", f, data)
	}
	tabs, err := a.Tabs(ctx, ch)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tb := range tabs {
		found = found || (tb.Type == "list" && tb.FileID == fakeslack.ListKitchen)
	}
	if !found {
		t.Errorf("tabs after addTab %+v", tabs)
	}
	if info, _, _ = a.ListHeader(ctx, fakeslack.ListKitchen); !ListSharedWith(info, ch) {
		t.Error("the tab didn't share the list with the channel")
	}

	// a new list for the tab, then a full tab bar
	id, err := a.CreateListWith(ctx, CreateListOpts{TemplateID: "blank", Reason: "create-list-from-channel-tabs"})
	if err != nil {
		t.Fatal(err)
	}
	fs.SetTabsFull(ch, true)
	if err := a.AddListTab(ctx, ch, id, "", "", ""); !errors.Is(err, ErrTooManyTabs) {
		t.Errorf("full tab bar: %v", err)
	}
}
