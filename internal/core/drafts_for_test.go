package core

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// DraftsFor is getDraftsByDestinationIdSortedByRecency (boot-shared 4gFp),
// the composer drafts button's list: a conversation's unscheduled drafts
// with text past whitespace or files, newest first, composer drafts
// included, a thread's apart, a DM's taking in those addressed to its
// person. DraftFor, what the composer restores, leaves composer drafts
// out (Slack keys them by client_msg_id, not by their destination).
// "Save as draft" saves a composer draft: is_from_composer and
// date_stashed (createDraftApi / updateDraftApi). Synthetic drafts.
func TestDraftsForLikeSlack(t *testing.T) {
	a, f := testApp(t)
	a.S.PutConv(slack.Conversation{ID: "D1", IsIM: true, User: "U2"})
	text := func(s string) string {
		return `[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"` + s + `"}]}]}]`
	}
	f.reply["drafts.list"] = `{"ok":true,"drafts":[
		{"id":"Dr1","destinations":[{"channel_id":"C1"}],"last_updated_ts":"1700000001","blocks":` + text("attached") + `},
		{"id":"Dr2","destinations":[{"channel_id":"C1"}],"last_updated_ts":"1700000003","is_from_composer":true,"date_stashed":1700000003000,"blocks":` + text("stashed") + `},
		{"id":"Dr3","destinations":[{"channel_id":"C1"}],"last_updated_ts":"1700000000","blocks":` + text("   ") + `},
		{"id":"Dr4","destinations":[{"channel_id":"C1"}],"last_updated_ts":"1700000002","file_ids":["F0SYNTH1"],"blocks":[]},
		{"id":"Dr5","destinations":[{"channel_id":"C1","thread_ts":"1.0"}],"last_updated_ts":"1700000005","blocks":` + text("reply") + `},
		{"id":"Dr6","destinations":[{"channel_id":"C1"}],"last_updated_ts":"1700000007","date_scheduled":4102444800,"blocks":` + text("later") + `},
		{"id":"Dr7","destinations":[{"user_ids":["U2"]}],"last_updated_ts":"1700000006","blocks":` + text("to U2") + `},
		{"id":"Dr8","destinations":[{"channel_id":"C2"}],"last_updated_ts":"1700000008","blocks":` + text("elsewhere") + `}]}`
	ctx := context.Background()
	if _, err := a.Drafts(ctx); err != nil {
		t.Fatal(err)
	}
	ids := func(ds []Draft) []string {
		out := []string{}
		for _, d := range ds {
			out = append(out, d.ID)
		}
		return out
	}
	for _, c := range []struct {
		ch, ts string
		want   []string
	}{
		{"C1", "", []string{"Dr2", "Dr4", "Dr1"}}, // no whitespace-only, reply, scheduled or other channel's
		{"C1", "1.0", []string{"Dr5"}},
		{"C1", "2.0", []string{}},
		{"D1", "", []string{"Dr7"}},
		{"D1", "9.0", []string{"Dr7"}}, // member-addressed drafts join each of the DM's lists
		{"C9", "", []string{}},
	} {
		if got := ids(a.DraftsFor(c.ch, c.ts)); !reflect.DeepEqual(got, c.want) {
			t.Errorf("DraftsFor(%q, %q) = %v, want %v", c.ch, c.ts, got, c.want)
		}
	}
	if d, ok := a.DraftFor("C1", ""); !ok || d.ID != "Dr4" {
		t.Errorf("DraftFor(C1) = %s %v, want Dr4: the newest attached draft, not the composer draft Dr2", d.ID, ok)
	}

	// the reply carries the draft's blocks, as Slack's and the fake's do,
	// but not is_from_composer or date_stashed: those come from what was sent
	f.reply["drafts.create"] = `{"ok":true,"draft":{"id":"Dr9","destinations":[{"channel_id":"C1"}],"last_updated_ts":"1700000009","blocks":` +
		text("saved from the composer") + `}}`
	d, err := a.SaveDraft(ctx, DraftOpts{Channel: "C1", Text: "saved from the composer", DateStashed: 1700000009000})
	if err != nil {
		t.Fatal(err)
	}
	p := f.last("drafts.create")
	if p.Get("is_from_composer") != "true" || p.Get("date_stashed") != "1700000009000" {
		t.Errorf("Save as draft's drafts.create: is_from_composer %q date_stashed %q", p.Get("is_from_composer"), p.Get("date_stashed"))
	}
	if !d.FromComposer || d.DateStashed != 1700000009000 {
		t.Errorf("the saved draft: from composer %v, stashed %d", d.FromComposer, d.DateStashed)
	}
	if got := ids(a.DraftsFor("C1", "")); !reflect.DeepEqual(got, []string{"Dr9", "Dr2", "Dr4", "Dr1"}) {
		t.Errorf("after Save as draft: DraftsFor(C1) = %v", got)
	}
	if _, err := a.SaveDraft(ctx, DraftOpts{Channel: "C1", Text: "typed"}); err != nil {
		t.Fatal(err)
	}
	if p := f.last("drafts.create"); p.Get("is_from_composer") != "false" || p.Has("date_stashed") {
		t.Errorf("a composer's own save: is_from_composer %q, date_stashed %q", p.Get("is_from_composer"), p.Get("date_stashed"))
	}
}

// wasRecentlyOnline (client-boot Vdrr TP): online, or the client's
// connection changed under 10s ago; until says when a lost one stops
// counting.
func TestRecentlyOnline(t *testing.T) {
	oldProbe, oldPoll := clientOnlineProbe, clientOnlinePoll
	clientOnlineProbe, clientOnlinePoll = func() bool { return true }, time.Hour
	t.Cleanup(func() { clientOnlineProbe, clientOnlinePoll = oldProbe, oldPoll })
	a := New(slack.New(slack.Creds{}), store.New(""))
	t.Cleanup(a.Shutdown)
	if on, until := a.RecentlyOnline(); !on || !until.IsZero() {
		t.Fatalf("online: RecentlyOnline %v until %v", on, until)
	}
	a.setClientOnline(false)
	on, until := a.RecentlyOnline()
	if !on || until.Before(time.Now().Add(9*time.Second)) || until.After(time.Now().Add(10*time.Second)) {
		t.Errorf("just offline: RecentlyOnline %v until %v, want true for 10s", on, time.Until(until))
	}
	a.online.mu.Lock()
	a.online.changed = time.Now().Add(-11 * time.Second)
	a.online.mu.Unlock()
	if on, _ := a.RecentlyOnline(); on {
		t.Error("offline for 11s: still recently online")
	}
	a.setClientOnline(true)
	if on, until := a.RecentlyOnline(); !on || !until.IsZero() {
		t.Errorf("back online: RecentlyOnline %v until %v", on, until)
	}
}
