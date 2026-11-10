package core

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
)

// The Activity rows' own actions (activity_rows.go) against
// internal/fakeslack's feed: each write's method and arguments as Slack's
// fetchers send them, and the state fakeslack keeps after it.

func actWrite(t *testing.T, fs *fakeslack.Server, method string) fakeslack.ActivityWrite {
	t.Helper()
	ws := fs.ActivityWrites()
	for i := len(ws) - 1; i >= 0; i-- {
		if ws[i].Method == method {
			return ws[i]
		}
	}
	t.Fatalf("no %s write (have %d writes)", method, len(ws))
	return fakeslack.ActivityWrite{}
}

func actItems(t *testing.T, a *App) map[string]*ActivityItem {
	t.Helper()
	page, err := a.Activity(context.Background(), ActivityOpts{View: "all"})
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]*ActivityItem{}
	for i := range page.Items {
		out[page.Items[i].Key] = &page.Items[i]
	}
	return out
}

// The feed's Slack Connect invites carry what v+N2 FI reads: a received DM
// invite offers Ignore then Accept, a channel invite Ignore then Accept
// (the channel accept modal is left out by the view).
func TestActivityInviteActionsLikeSlack(t *testing.T) {
	a, _ := actFake(t)
	items := actItems(t, a)
	now := time.Unix(1762702100, 0)
	dm := items["fa-ext-dm-invite"]
	if dm == nil || dm.Invite == nil {
		t.Fatal("fa-ext-dm-invite has no invite")
	}
	if dm.Invite.Kind != "im" || dm.Invite.Sig != fakeslack.ActivityInviteSig || dm.Invite.TeamVerified || dm.Invite.TeamIcon == "" || dm.Invite.InviterEmail != "pat@partner.example" {
		t.Errorf("dm invite %+v", *dm.Invite)
	}
	if got := dm.Invite.InviteActions(a.S.SelfID(), now); len(got) != 2 || got[0] != InviteActIgnore || got[1] != InviteActAcceptDM {
		t.Errorf("dm invite actions %v", got)
	}
	ch := items["fa-ext-invite"]
	if ch == nil || ch.Invite == nil || !ch.Invite.TeamVerified {
		t.Fatalf("fa-ext-invite %+v", ch)
	}
	if got := ch.Invite.InviteActions(a.S.SelfID(), now); len(got) != 2 || got[0] != InviteActIgnore || got[1] != InviteActAcceptChannel {
		t.Errorf("channel invite actions %v", got)
	}
	// a revoked or expired invite has nothing to act on
	rev := *ch.Invite
	rev.Status = "revoked"
	if got := rev.InviteActions(a.S.SelfID(), now); len(got) != 0 {
		t.Errorf("revoked invite actions %v", got)
	}
	exp := *dm.Invite
	exp.DateInvalid = now.Unix() - 1
	if got := exp.InviteActions(a.S.SelfID(), now); len(got) != 0 {
		t.Errorf("expired invite actions %v", got)
	}
	// an outgoing invite isn't the inbox's to answer
	out := *dm.Invite
	out.Direction = "outgoing"
	if got := out.InviteActions(a.S.SelfID(), now); len(got) != 0 {
		t.Errorf("outgoing invite actions %v", got)
	}
}

// Ignore: conversations.ignoreInvite for a channel invite,
// sharedInvites.ignoreInvite for a DM; Block: *.ignoreUser; Accept on a
// DM: sharedInvites.canAccept {sig} then sharedInvites.accept {sig,
// invite}. An answered invite leaves the feed.
func TestActivityInviteWrites(t *testing.T) {
	a, fs := actFake(t)
	ctx := context.Background()
	if err := a.IgnoreConnectInvite(ctx, "I0FAKE1", true); err != nil {
		t.Fatal(err)
	}
	if w := actWrite(t, fs, "conversations.ignoreInvite"); w.Form.Get("invite") != "I0FAKE1" || w.Form.Get("reason") != "ignore_this_modal__ignore" {
		t.Errorf("conversations.ignoreInvite %v", w.Form)
	}
	if err := a.IgnoreConnectInvite(ctx, fakeslack.ActivityInviteDM, false); err != nil {
		t.Fatal(err)
	}
	actWrite(t, fs, "sharedInvites.ignoreInvite")
	if err := a.BlockConnectInvites(ctx, "I0FAKE1", true); err != nil {
		t.Fatal(err)
	}
	if w := actWrite(t, fs, "conversations.ignoreUser"); w.Form.Get("reason") != "ignore_all_modal__ignore" {
		t.Errorf("conversations.ignoreUser %v", w.Form)
	}
	if err := a.BlockConnectInvites(ctx, fakeslack.ActivityInviteDM, false); err != nil {
		t.Fatal(err)
	}
	actWrite(t, fs, "sharedInvites.ignoreUser")
	ok, err := a.ConnectDMInviteEligible(ctx, fakeslack.ActivityInviteSig)
	if err != nil || !ok {
		t.Fatalf("canAccept: %v %v", ok, err)
	}
	if w := actWrite(t, fs, "sharedInvites.canAccept"); w.Form.Get("sig") != fakeslack.ActivityInviteSig {
		t.Errorf("canAccept %v", w.Form)
	}
	if err := a.AcceptConnectDMInvite(ctx, fakeslack.ActivityInviteSig, fakeslack.ActivityInviteDM); err != nil {
		t.Fatal(err)
	}
	if w := actWrite(t, fs, "sharedInvites.accept"); w.Form.Get("sig") != fakeslack.ActivityInviteSig || w.Form.Get("invite") != fakeslack.ActivityInviteDM {
		t.Errorf("accept %v", w.Form)
	}
	items := actItems(t, a)
	if items["fa-ext-invite"] != nil || items["fa-ext-dm-invite"] != nil {
		t.Error("answered invites are still in the feed")
	}
}

// The reminder rows' saved items: saved.get by Gjwd qy's keys, Mark
// Complete (saved.update mark/todo_state completed, date_due 0) and
// Reschedule (saved.update date_due).
func TestActivityReminderWrites(t *testing.T) {
	a, fs := actFake(t)
	ctx := context.Background()
	items := actItems(t, a)
	var keys []SavedKey
	for _, k := range []string{"fa-reminder", "fa-reminder-file", "fa-reminder-plain", "fa-reminder-done"} {
		it := items[k]
		if it == nil {
			t.Fatalf("%s missing", k)
		}
		key, ok := ReminderKey(it)
		if !ok {
			t.Fatalf("%s: no saved key", k)
		}
		keys = append(keys, key)
	}
	if keys[0] != (SavedKey{ItemID: fakeslack.ViewsChannel, ItemType: "message", TS: "1762700100.000100"}) || keys[1].ItemID != fakeslack.ActivityReminderFile {
		t.Errorf("keys %+v", keys)
	}
	got, err := a.SavedGet(ctx, keys)
	if err != nil || len(got) != 4 {
		t.Fatalf("saved.get: %d items, %v", len(got), err)
	}
	by := map[string]SavedItem{}
	for _, s := range got {
		by[s.ItemID] = s
	}
	if by["Rm0FAKE2"].Completed() == false || by[fakeslack.ViewsChannel].Completed() {
		t.Errorf("states %+v", by)
	}
	if err := a.CompleteSavedItem(ctx, by[fakeslack.ViewsChannel]); err != nil {
		t.Fatal(err)
	}
	w := actWrite(t, fs, "saved.update")
	for k, want := range map[string]string{"item_type": "message", "item_id": fakeslack.ViewsChannel, "ts": "1762700100.000100",
		"mark": "completed", "todo_state": "completed", "date_due": "0"} {
		if w.Form.Get(k) != want {
			t.Errorf("Mark Complete %s = %q, want %q", k, w.Form.Get(k), want)
		}
	}
	if err := a.SetSavedItemDue(ctx, by[fakeslack.ActivityReminderFile], 1762800000); err != nil {
		t.Fatal(err)
	}
	w = actWrite(t, fs, "saved.update")
	if w.Form.Get("item_type") != "file" || w.Form.Get("date_due") != "1762800000" || w.Form.Has("ts") || w.Form.Has("mark") {
		t.Errorf("Reschedule %v", w.Form)
	}
	again, _ := a.SavedGet(ctx, keys[:2])
	for _, s := range again {
		if s.ItemType == "message" && !s.Completed() || s.ItemType == "file" && s.DateDue != 1762800000 {
			t.Errorf("after the writes: %+v", s)
		}
	}
}

// The clear-all toast's Undo (activity.clearAll {undo_key}), the
// realtime event that raises it, saved views' density
// (activity.views.update), and "Don't notify for @here and @channel".
func TestActivityClearAllViewsAndSuppress(t *testing.T) {
	a, fs := actFake(t)
	ctx := context.Background()
	evs, stop := a.Subscribe()
	defer stop()
	a.handle(slack.Event{Type: "activity_clear_all_completed", Raw: json.RawMessage(`{"type":"activity_clear_all_completed","undo_key":"uk-1"}`)})
	select {
	case ev := <-evs:
		if ev.Type != "activity" || ev.Name != "activity_clear_all_completed" || ev.ID != "uk-1" {
			t.Errorf("event %+v", ev)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no activity event")
	}
	if err := a.UndoClearAll(ctx, "uk-1"); err != nil {
		t.Fatal(err)
	}
	if w := actWrite(t, fs, "activity.clearAll"); w.Form.Get("undo_key") != "uk-1" || w.Form.Get("reason") != "undo-clear-all" {
		t.Errorf("clearAll %v", w.Form)
	}
	views, err := a.ActivityViews(ctx)
	if err != nil || len(views) != 4 {
		t.Fatalf("views %v %v", views, err)
	}
	var mentions ActivityView
	for _, v := range views {
		if v.Type == "mentions" {
			mentions = v
		}
	}
	if err := a.UpdateActivityView(ctx, mentions, true); err != nil {
		t.Fatal(err)
	}
	if w := actWrite(t, fs, "activity.views.update"); w.Form.Get("view_id") != mentions.ID || w.Form.Get("density") != "dense" || w.Form.Get("sort") != "newest" {
		t.Errorf("views.update %v", w.Form)
	}
	views, _ = a.ActivityViews(ctx)
	for _, v := range views {
		if want := map[bool]string{true: "dense", false: "compact"}[v.ID == mentions.ID]; v.Density != want {
			t.Errorf("view %s density %s, want %s", v.Type, v.Density, want)
		}
	}
	if err := a.SetSuppressAtChannel(ctx, fakeslack.ViewsChannel, true); err != nil {
		t.Fatal(err)
	}
	if !a.NotifyPrefs(fakeslack.ViewsChannel).SuppressAtChannel {
		t.Error("suppress_at_channel not set")
	}
}

// The read rows' marks: getYouRepliedMessageTs and the reactions you
// added, from the cached conversations.
func TestActivityRepliedReacted(t *testing.T) {
	a, _ := actFake(t)
	items := actItems(t, a)
	self := a.S.SelfID()
	msg := func(ts, user, thread string) slack.Message {
		return slack.Message{Type: "message", TS: ts, User: user, ThreadTS: thread}
	}
	a.S.SetLatest(fakeslack.ViewsChannel, []slack.Message{
		msg("1762700100.000100", "U0FAKE00001", ""), msg("1762700200.000100", "U0FAKE00002", ""),
		msg("1762700900.000100", self, ""),
	}, false)
	keyword := items["fa-keyword"]
	if got := a.YouRepliedTS(keyword); got != "1762700900.000100" {
		t.Errorf("keyword replied %q", got)
	}
	// someone wrote after you: not replied
	a.S.SetLatest(fakeslack.ViewsChannel, []slack.Message{msg("1762700900.000100", self, ""), msg("1762700950.000100", "U0FAKE00002", "")}, false)
	if got := a.YouRepliedTS(keyword); got != "" {
		t.Errorf("replied after someone else wrote: %q", got)
	}
	// a thread entry whose latest reply isn't the one that notified you
	th := *items["fa-thread"]
	th.LatestTS, th.FeedTS = "1762600300.000100", "1762600200.000100"
	if got := a.YouRepliedTS(&th); got != th.LatestTS {
		t.Errorf("thread replied %q", got)
	}
	dm := items["fa-dm-reacted"]
	m := slack.Message{Type: "message", TS: "1762700750.000100", User: "U0FAKE00001",
		Reactions: []slack.Reaction{{Name: "eyes", Count: 1, Users: []string{self}}, {Name: "tada", Count: 1, Users: []string{"U0FAKE00002"}}}}
	a.S.SetLatest("D0FAKE00000", []slack.Message{m}, false)
	if got := a.YouReacted(dm); len(got) != 1 || got[0] != "eyes" {
		t.Errorf("reacted %v", got)
	}
	if got := a.YouRepliedTS(dm); got != "" {
		t.Errorf("dm replied %q", got)
	}
}
