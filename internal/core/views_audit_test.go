package core

import (
	"context"
	"net/url"
	"sync"
	"testing"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// The views audit (2026-11-09) against internal/fakeslack's views
// fixtures: synthetic threads and drafts only.

// UnscheduleDraft is Slack's "Cancel schedule and save to drafts": the
// scheduled message is updated with date_scheduled 0 and stays as a
// draft with its text and destination.
func TestUnscheduleDraft(t *testing.T) {
	sz := fakeslack.Small
	sz.Views = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	var mu sync.Mutex
	var forms []url.Values
	fs.SetHook(func(method string, f url.Values) (any, bool) {
		if method == "drafts.update" {
			mu.Lock()
			forms = append(forms, f)
			mu.Unlock()
		}
		return nil, false
	})
	ctx := context.Background()
	a := New(slack.New(fs.Creds()), store.New(""))
	ds, err := a.Drafts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var sched *Draft
	for i := range ds {
		if ds[i].DateScheduled > 0 && ds[i].ThreadTS == "" {
			sched = &ds[i]
		}
	}
	if sched == nil {
		t.Fatalf("no scheduled message in %+v", ds)
	}
	d, err := a.UnscheduleDraft(ctx, sched.ID)
	if err != nil {
		t.Fatal(err)
	}
	if d.ID != sched.ID || d.DateScheduled != 0 || d.Channel != fakeslack.ViewsChannel {
		t.Fatalf("unscheduled draft %+v", d)
	}
	mu.Lock()
	if len(forms) != 1 || forms[0].Get("date_scheduled") != "0" || forms[0].Get("draft_id") != sched.ID {
		t.Fatalf("drafts.update forms %v, want one with date_scheduled 0", forms)
	}
	mu.Unlock()
	for _, sd := range fs.LiveDrafts() {
		if sd.ID == sched.ID && sd.DateScheduled != 0 {
			t.Fatalf("server still has it scheduled: %+v", sd)
		}
	}
	ds, _ = a.Drafts(ctx)
	for _, x := range ds {
		if x.ID == sched.ID && (x.DateScheduled != 0 || x.Text != "a synthetic scheduled update") {
			t.Fatalf("reloaded %+v", x)
		}
	}
}

// Activity asks activity.feed with the web client's params (captured from
// the Activity inbox on 2026-11-09): limit 20 and the automation and
// Salesforce flags off.
func TestActivityFeedParams(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	got := make(chan url.Values, 1)
	fs.SetHook(func(method string, f url.Values) (any, bool) {
		if method == "activity.feed" {
			select {
			case got <- f:
			default:
			}
			return map[string]any{"ok": true, "items": []any{}}, true
		}
		return nil, false
	})
	a := New(slack.New(fs.Creds()), store.New(""))
	if _, err := a.Activity(context.Background(), ActivityOpts{View: "all"}); err != nil {
		t.Fatal(err)
	}
	f := <-got
	want := map[string]string{"limit": "20", "mode": "chrono_v1", "archive_only": "false", "unread_only": "false",
		"priority_only": "false", "is_activity_inbox": "true", "only_salesforce_channels": "false",
		"exclude_automations": "false", "automations_only": "false"}
	for k, v := range want {
		if f.Get(k) != v {
			t.Errorf("activity.feed %s = %q, want %q", k, f.Get(k), v)
		}
	}
	if f.Get("types") != activityTypes["all"] {
		t.Errorf("types = %q", f.Get("types"))
	}
}
