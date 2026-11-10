package core

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// The Activity inbox against internal/fakeslack's feed (Size.Views): one
// entry per item type, made-up content only.

func actFake(t *testing.T) (*App, *fakeslack.Server) {
	t.Helper()
	sz := fakeslack.Small
	sz.Views = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fs.Close() })
	return New(slack.New(fs.Creds()), store.New("")), fs
}

// Every fixture entry comes back located and, when it is about a message,
// hydrated through messages.list.
func TestActivityFakeslackFeed(t *testing.T) {
	a, _ := actFake(t)
	ctx := context.Background()
	page, err := a.Activity(ctx, ActivityOpts{View: "all"})
	if err != nil {
		t.Fatal(err)
	}
	keys := map[string]*ActivityItem{}
	for i := range page.Items {
		keys[page.Items[i].Key] = &page.Items[i]
	}
	// generic_system_alert and team_joiner_invite aren't in the all view's
	// types (hgTi adds team_joiner_invite only behind an experiment)
	for _, k := range fakeslack.ActivityKeys() {
		if keys[k] == nil && k != "fa-alert" && k != "fa-joiner" {
			t.Errorf("%s missing from the all view", k)
		}
	}
	every, err := a.Activity(ctx, ActivityOpts{Types: "generic_system_alert,team_joiner_invite"})
	if err != nil || len(every.Items) != 2 {
		t.Errorf("explicit types: %d items (%v), want the alert and the joiner", len(every.Items), err)
	}
	for _, k := range []string{"fa-at-user", "fa-at-channel", "fa-at-group", "fa-keyword", "fa-reaction", "fa-channel", "fa-dm", "fa-invite", "fa-reminder"} {
		if it := keys[k]; it != nil && it.Msg == nil {
			t.Errorf("%s: message not hydrated (%s %s)", k, it.Channel, it.TS)
		}
	}
	if it := keys["fa-reaction"]; it != nil && (it.Reaction == nil || it.Reaction.User != "U0FAKE00001" || it.Msg == nil || len(it.Msg.Reactions) == 0) {
		t.Errorf("fa-reaction: %+v", it)
	}
	if it := keys["fa-ext-invite"]; it != nil && (it.Team != "Partner Co" || it.Title != "synthetic-partners") {
		t.Errorf("fa-ext-invite: team %q title %q", it.Team, it.Title)
	}
	if it := keys["fa-reminder-plain"]; it != nil && (it.ReminderItem != "reminder" || it.Text != "water the synthetic plants") {
		t.Errorf("fa-reminder-plain: %q %q", it.ReminderItem, it.Text)
	}
}

// The tabs' types, Unreads and Mark as read / unread round-trip through
// the fixture like Slack's server.
func TestActivityFakeslackFiltersAndReads(t *testing.T) {
	a, fs := actFake(t)
	ctx := context.Background()
	page, err := a.Activity(ctx, ActivityOpts{View: "mentions"})
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range page.Items {
		if !strings.Contains(activityTypes["mentions"], it.Type) {
			t.Errorf("mentions view returned %s (%s)", it.Key, it.Type)
		}
	}
	if len(page.Items) != 4 {
		t.Errorf("mentions: %d items, want 4", len(page.Items))
	}
	unread, err := a.Activity(ctx, ActivityOpts{View: "all", Unread: true})
	if err != nil {
		t.Fatal(err)
	}
	var target ActivityItem
	for _, it := range unread.Items {
		if !it.Unread {
			t.Errorf("unread_only returned read %s", it.Key)
		}
		if it.Key == "fa-at-user" {
			target = it
		}
	}
	if target.Key == "" {
		t.Fatal("fa-at-user isn't unread")
	}
	if err := a.MarkActivityRead(ctx, target); err != nil {
		t.Fatal(err)
	}
	again, _ := a.Activity(ctx, ActivityOpts{View: "all", Unread: true})
	for _, it := range again.Items {
		if it.Key == target.Key {
			t.Error("fa-at-user still unread after activity.markRead")
		}
	}
	if err := a.MarkActivityUnread(ctx, target); err != nil {
		t.Fatal(err)
	}
	ws := fs.ActivityWrites()
	if len(ws) != 2 || ws[0].Method != "activity.markRead" || ws[0].Form.Get("key") != "fa-at-user" || ws[0].Form.Get("feed_ts") != target.FeedTS ||
		ws[1].Method != "activity.markUnread" {
		t.Errorf("writes %+v", ws)
	}
}

// "Clear all before…" is activity.clearBefore {before_ts, filters}: the
// older entries leave the inbox and show under Cleared items.
func TestActivityClearBefore(t *testing.T) {
	a, fs := actFake(t)
	ctx := context.Background()
	if err := a.ClearActivityBefore(ctx, "1762700400.000100", nil); err != nil {
		t.Fatal(err)
	}
	if err := a.ClearActivityBefore(ctx, "1762700400.000100", &ActivityFilters{EntryTypes: []string{"at_user"}, UnreadOnly: true}); err != nil {
		t.Fatal(err)
	}
	ws := fs.ActivityWrites()
	if len(ws) != 2 || ws[0].Method != "activity.clearBefore" || ws[0].Form.Get("before_ts") != "1762700400.000100" || ws[0].Form.Has("filters") {
		t.Fatalf("writes %+v", ws)
	}
	var f map[string]any
	if err := json.Unmarshal([]byte(ws[1].Form.Get("filters")), &f); err != nil || f["unread_only"] != true || f["entry_types"] == nil {
		t.Errorf("filters %q (%v)", ws[1].Form.Get("filters"), err)
	}
	page, _ := a.Activity(ctx, ActivityOpts{View: "all"})
	for _, it := range page.Items {
		if it.FeedTS < "1762700400.000100" {
			t.Errorf("%s (%s) still in the inbox", it.Key, it.FeedTS)
		}
	}
	cleared, _ := a.Activity(ctx, ActivityOpts{View: "all", Archived: true})
	if len(cleared.Items) == 0 || len(cleared.Items) != len(fs.ActivityArchived()) {
		t.Errorf("cleared items %d, archived %v", len(cleared.Items), fs.ActivityArchived())
	}
}

// The layout toggle round-trips through Slack's global prefs:
// activity.views' prefs.density, activity.prefs.update {density}.
func TestActivityDensityRoundTrip(t *testing.T) {
	a, fs := actFake(t)
	ctx := context.Background()
	dense, ok, err := a.ActivityDensity(ctx)
	if err != nil || !ok || dense {
		t.Fatalf("first read: dense %v ok %v err %v, want Detailed", dense, ok, err)
	}
	saved, err := a.SetActivityDensity(ctx, true)
	if err != nil || !saved {
		t.Fatalf("SetActivityDensity: saved %v err %v", saved, err)
	}
	ws := fs.ActivityWrites()
	if len(ws) != 1 || ws[0].Method != "activity.prefs.update" || ws[0].Form.Get("density") != "dense" {
		t.Fatalf("writes %+v", ws)
	}
	if dense, ok, _ := a.ActivityDensity(ctx); !ok || !dense {
		t.Errorf("after saving Dense: dense %v ok %v", dense, ok)
	}
	// without activity_global_settings Slack keeps the All view's density
	// in the page only, and reads the All view's own density
	a.SetExperimentGroup("activity_global_settings", "off")
	if saved, err := a.SetActivityDensity(ctx, false); saved || err != nil {
		t.Errorf("flag off: saved %v err %v, want nothing written", saved, err)
	}
	if len(fs.ActivityWrites()) != 1 {
		t.Errorf("flag off wrote: %+v", fs.ActivityWrites())
	}
}
