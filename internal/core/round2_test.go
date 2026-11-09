package core

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
)

// Synthetic fixtures only.

func TestOptimisticSendConfirmedByReply(t *testing.T) {
	a, f := testApp(t)
	a.S.SetLatest("C1", []slack.Message{{TS: "100.000001", User: "U1", Text: "x"}}, false)
	f.reply["chat.postMessage"] = `{"ok":true,"ts":"200.000001","message":{"ts":"200.000001","user":"USELF","text":"hi"}}`
	evs, stop := a.Subscribe()
	defer stop()
	m, err := a.Send(context.Background(), SendOpts{Channel: "C1", Text: "hi"})
	if err != nil || m.TS != "200.000001" || m.Pending {
		t.Fatalf("send: %+v %v", m, err)
	}
	p := next(t, evs, "message")
	if !p.Msg.Pending || p.ClientMsgID == "" || p.Msg.ClientMsgID != p.ClientMsgID || !strings.Contains(p.TS, ".") {
		t.Fatalf("pending event: %+v", p.Msg)
	}
	s := next(t, evs, "sent")
	if s.ReplacesTS != p.TS || s.TS != "200.000001" || s.ClientMsgID != p.ClientMsgID || s.Msg.Pending {
		t.Errorf("sent event: %+v", s)
	}
	tl := a.S.Timeline("C1")
	if len(tl.Msgs) != 2 || tl.Msgs[1].TS != "200.000001" {
		t.Errorf("timeline after confirm: %v", tss(tl.Msgs))
	}
	if got := f.last("chat.postMessage").Get("client_msg_id"); got != p.ClientMsgID {
		t.Errorf("client_msg_id sent %q, pending %q", got, p.ClientMsgID)
	}
	// A late realtime echo becomes an update, not a second message.
	a.handle(rtm("message", map[string]any{"channel": "C1", "ts": "200.000001", "user": "USELF", "text": "hi", "client_msg_id": p.ClientMsgID}))
	if u := next(t, evs, "update"); u.TS != "200.000001" {
		t.Errorf("late echo: %+v", u)
	}
}

func TestOptimisticSendConfirmedByEcho(t *testing.T) {
	a, f := testApp(t)
	a.S.SetLatest("C1", []slack.Message{{TS: "100.000001", User: "U1"}}, false)
	f.hooks["chat.postMessage"] = func(form url.Values) {
		// the realtime echo arrives before the HTTP reply
		a.handle(rtm("message", map[string]any{"channel": "C1", "ts": "300.000001", "user": "USELF", "text": "yo", "client_msg_id": form.Get("client_msg_id")}))
	}
	f.reply["chat.postMessage"] = `{"ok":true,"ts":"300.000001","message":{"ts":"300.000001","user":"USELF","text":"yo"}}`
	evs, stop := a.Subscribe()
	defer stop()
	if _, err := a.Send(context.Background(), SendOpts{Channel: "C1", Text: "yo"}); err != nil {
		t.Fatal(err)
	}
	p := next(t, evs, "message")
	s := next(t, evs, "sent")
	if s.ReplacesTS != p.TS || s.TS != "300.000001" {
		t.Errorf("sent: %+v", s)
	}
	select {
	case ev := <-evs:
		if ev.Type == "message" {
			t.Errorf("duplicate message event for own echo: %+v", ev)
		}
	case <-time.After(100 * time.Millisecond):
	}
	if n := len(a.S.Timeline("C1").Msgs); n != 2 {
		t.Errorf("timeline has %d messages", n)
	}
}

func TestOptimisticSendFailureResendDiscard(t *testing.T) {
	a, f := testApp(t)
	a.S.SetLatest("C1", []slack.Message{{TS: "100.000001", User: "U1"}}, false)
	f.reply["chat.postMessage"] = `{"ok":false,"error":"msg_too_long"}`
	evs, stop := a.Subscribe()
	defer stop()
	if _, err := a.Send(context.Background(), SendOpts{Channel: "C1", Text: "x"}); err == nil {
		t.Fatal("expected error")
	}
	p := next(t, evs, "message")
	u := next(t, evs, "update")
	if !u.Msg.Failed || u.Msg.Pending || u.TS != p.TS {
		t.Fatalf("failed update: %+v", u.Msg)
	}
	if tl := a.S.Timeline("C1"); len(tl.Msgs) != 2 {
		t.Error("failed message should stay in the timeline")
	}
	if pg, _ := a.History(context.Background(), "C1", ""); !pg.Messages[1].Failed {
		t.Error("History lost the Failed flag")
	}
	// Mark on a local ts must not hit Slack with a fake ts.
	f.reply["conversations.mark"] = `{"ok":true}`
	a.Mark(context.Background(), "C1", p.TS)
	if got := f.last("conversations.mark").Get("ts"); got != "100.000001" {
		t.Errorf("marked %q, want the last confirmed message", got)
	}
	f.reply["chat.postMessage"] = `{"ok":true,"ts":"400.000001","message":{"ts":"400.000001","user":"USELF","text":"x"}}`
	m, err := a.Resend(context.Background(), "C1", p.ClientMsgID)
	if err != nil || m.TS != "400.000001" {
		t.Fatalf("resend: %+v %v", m, err)
	}
	if got := f.last("chat.postMessage").Get("client_msg_id"); got != p.ClientMsgID {
		t.Error("resend changed client_msg_id")
	}
	if tl := a.S.Timeline("C1"); len(tl.Msgs) != 2 || tl.Msgs[1].TS != "400.000001" {
		t.Errorf("after resend: %v", tss(tl.Msgs))
	}
	// Discard path
	f.reply["chat.postMessage"] = `{"ok":false,"error":"x"}`
	_, _ = a.Send(context.Background(), SendOpts{Channel: "C1", Text: "y"})
	p2 := next(t, evs, "message")
	for p2.ClientMsgID == p.ClientMsgID {
		p2 = next(t, evs, "message")
	}
	a.DiscardPending("C1", p2.ClientMsgID)
	d := next(t, evs, "delete")
	for d.TS != p2.TS && d.TS == p.TS { // Resend's own delete of the old row comes first
		d = next(t, evs, "delete")
	}
	if d.TS != p2.TS {
		t.Errorf("discard: %+v", d)
	}
	if _, ok := a.S.Find("C1", p2.TS); ok {
		t.Error("discarded message still cached")
	}
}

func TestPendingReplyAndTS(t *testing.T) {
	if got := tsAfter("5.999999"); got != "6.000000" {
		t.Errorf("tsAfter = %s", got)
	}
	a, f := testApp(t)
	a.S.SetLatest("C1", []slack.Message{{TS: "9999999999.000001", User: "U1", ReplyCount: 0, ThreadTS: "9999999999.000001"}}, false)
	a.S.SetThread("C1", "9999999999.000001", []slack.Message{{TS: "9999999999.000001", ThreadTS: "9999999999.000001"}})
	if ts := a.pendingTS("C1", ""); ts != "9999999999.000002" {
		t.Errorf("pendingTS after a future message = %s", ts)
	}
	f.reply["chat.postMessage"] = `{"ok":true,"ts":"9999999999.000005","message":{"ts":"9999999999.000005","user":"USELF","thread_ts":"9999999999.000001"}}`
	if _, err := a.Send(context.Background(), SendOpts{Channel: "C1", Text: "r", ThreadTS: "9999999999.000001"}); err != nil {
		t.Fatal(err)
	}
	th, _ := a.S.Thread("C1", "9999999999.000001")
	if len(th) != 2 || th[1].TS != "9999999999.000005" {
		t.Errorf("thread after reply: %v", tss(th))
	}
	if root, _ := a.S.Find("C1", "9999999999.000001"); root.ReplyCount != 1 {
		t.Errorf("parent not bumped: %d", root.ReplyCount)
	}
	if tl := a.S.Timeline("C1"); len(tl.Msgs) != 1 {
		t.Error("thread reply leaked into the channel timeline")
	}
}

func TestScheduleAndDrafts(t *testing.T) {
	a, f := testApp(t)
	f.reply["drafts.create"] = `{"ok":true,"draft":{"id":"Dr1","destinations":[{"channel_id":"C1"}],"blocks":[]}}`
	f.reply["drafts.update"] = `{"ok":true,"draft":{"id":"Dr1","destinations":[{"channel_id":"C1"}],"date_scheduled":4102444800,"blocks":[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"later"}]}]}]}}`
	sm, err := a.ScheduleMessage(context.Background(), SendOpts{Channel: "C1", Text: "later"}, 4102444800)
	if err != nil || sm.ID != "Dr1" || sm.PostAt != 4102444800 || sm.Channel != "C1" {
		t.Fatalf("schedule: %+v %v", sm, err)
	}
	up := f.last("drafts.update")
	if up.Get("date_scheduled") != "4102444800" || up.Get("draft_id") != "Dr1" || !strings.Contains(up.Get("blocks"), `"later"`) || !strings.Contains(up.Get("destinations"), `"C1"`) {
		t.Errorf("drafts.update params: %v", up)
	}
	if _, err := a.ScheduleMessage(context.Background(), SendOpts{Channel: "C1"}, 1); err == nil {
		t.Error("past postAt accepted")
	}
	f.reply["drafts.list"] = `{"ok":true,"drafts":[
		{"id":"Dr1","destinations":[{"channel_id":"C1"}],"date_scheduled":4102444800,"blocks":[]},
		{"id":"Dr2","destinations":[{"channel_id":"C1","thread_ts":"1.0"}],"date_scheduled":0,"blocks":[]},
		{"id":"Dr3","destinations":[{"channel_id":"C1"}],"is_sent":true}]}`
	ds, err := a.Drafts(context.Background())
	if err != nil || len(ds) != 2 || ds[1].ThreadTS != "1.0" {
		t.Fatalf("drafts: %+v %v", ds, err)
	}
	sms, _ := a.ScheduledMessages(context.Background(), "C1")
	if len(sms) != 1 || sms[0].ID != "Dr1" {
		t.Errorf("scheduled: %+v", sms)
	}
	f.reply["drafts.delete"] = `{"ok":true}`
	if err := a.DeleteScheduled(context.Background(), "C1", "Dr1"); err != nil || f.last("drafts.delete").Get("draft_id") != "Dr1" {
		t.Errorf("delete scheduled: %v", err)
	}
	// realtime
	evs, stop := a.Subscribe()
	defer stop()
	a.handle(rtm("draft_delete", map[string]any{"draft_id": "Dr2"}))
	if e := next(t, evs, "draft"); e.Name != "delete" || e.ID != "Dr2" {
		t.Errorf("draft event: %+v", e)
	}
	a.handle(rtm("draft_update", map[string]any{"draft_id": "Dr2", "draft": map[string]any{"id": "Dr2", "destinations": []any{map[string]any{"channel_id": "C1"}}}}))
	if e := next(t, evs, "draft"); e.Name != "update" || e.Draft == nil || e.Draft.Channel != "C1" {
		t.Errorf("draft update event: %+v", e)
	}
}

func TestSavedStarsAndEvents(t *testing.T) {
	a, f := testApp(t)
	a.S.SetLatest("C1", []slack.Message{{TS: "1.000001", User: "U1", Subscribed: true, PinnedInfo: &slack.PinnedInfo{PinnedBy: "U2"}}}, false)
	f.reply["stars.list"] = `{"ok":true,"items":[{"type":"channel","channel":"C1"}],"paging":{"pages":1}}`
	f.reply["saved.list"] = `{"ok":true,"saved_items":[{"item_id":"C1","item_type":"message","ts":"1.000001","todo_state":"saved"}]}`
	a.loadExtras(context.Background())
	v := a.message("C1", &a.S.Timeline("C1").Msgs[0])
	if !v.Saved || !v.FollowingThread || !v.Pinned || v.PinnedBy != "U2" {
		t.Errorf("flags: saved=%v follow=%v pinned=%v by=%q", v.Saved, v.FollowingThread, v.Pinned, v.PinnedBy)
	}
	if c, _ := a.Conv("C1"); !c.Starred {
		t.Error("C1 not starred")
	}
	close(a.ready)
	snap := a.Snapshot()
	var starred []string
	for _, s := range snap.Sections {
		if s.Type == "stars" {
			starred = s.Channels
		}
	}
	_ = starred // no stars section configured in this fixture: Starred only shows via Conv.Starred
	evs, stop := a.Subscribe()
	defer stop()
	f.reply["messages.list"] = `{"ok":true,"messages":{}}`
	a.handle(rtm("saved_deleted", map[string]any{"saved": map[string]any{"item_id": "C1", "item_type": "message", "ts": "1.000001"}}))
	if e := next(t, evs, "update"); e.Msg.Saved {
		t.Error("saved_deleted did not clear Saved")
	}
	if e := next(t, evs, "saved"); e.Name != "delete" || e.Saved.Channel != "C1" {
		t.Errorf("saved event: %+v", e)
	}
	a.handle(rtm("thread_unsubscribed", map[string]any{"subscription": map[string]any{"channel": "C1", "thread_ts": "1.000001"}}))
	if e := next(t, evs, "update"); e.Msg.FollowingThread {
		t.Error("thread_unsubscribed did not clear FollowingThread")
	}
	a.handle(rtm("star_removed", map[string]any{"item": map[string]any{"type": "channel", "channel": "C1"}}))
	next(t, evs, "sidebar_changed")
	if a.Starred("C1") {
		t.Error("star_removed ignored")
	}
	a.handle(rtm("bookmark_added", map[string]any{"channel": "C1"}))
	if e := next(t, evs, "tabs"); e.Channel != "C1" {
		t.Errorf("tabs event: %+v", e)
	}
	f.reply["stars.add"] = `{"ok":true}`
	if err := a.Star(context.Background(), "C1", true); err != nil || !a.Starred("C1") || f.last("stars.add").Get("channel") != "C1" {
		t.Errorf("star: %v", err)
	}
}

func TestStarredSectionAndAppDMs(t *testing.T) {
	a, _ := testApp(t)
	a.S.PutUsers([]slack.User{{ID: "USLACK", Name: "slack"}})
	a.S.PutConv(slack.Conversation{ID: "D3", IsIM: true, User: "USLACK"})
	a.S.PutConv(slack.Conversation{ID: "C5", Name: "fav", IsChannel: true})
	sec := func(id, typ, next string, chans ...string) slack.Section {
		x := slack.Section{ID: id, Type: typ, Next: next}
		x.ChannelIDsPage.ChannelIDs = chans
		return x
	}
	a.S.Sections = []slack.Section{sec("L1", "stars", "L2"), sec("L2", "channels", "L3"), sec("L3", "direct_messages", "", "D3", "D1")}
	a.setStarred("C5", true)
	close(a.ready)
	got := map[string]string{}
	for _, s := range a.Snapshot().Sections {
		got[s.Type] = strings.Join(s.Channels, ",")
	}
	if got["stars"] != "C5" || got["direct_messages"] != "D1" || got["apps"] != "D3" || strings.Contains(got["channels"], "C5") {
		t.Errorf("sections: %v", got)
	}
}

func TestNotifyPrefsAndMute(t *testing.T) {
	a, f := testApp(t)
	f.reply["users.prefs.setNotifications"] = `{"ok":true}`
	notif, _ := json.Marshal(`{"channels":{"C1":{"muted":true,"desktop":"everything","follow_all_threads":"true"}},"global":{"global_desktop":"mentions_dms"}}`)
	f.reply["users.prefs.get"] = `{"ok":true,"prefs":{"all_notifications_prefs":` + string(notif) + `}}`
	if err := a.Mute(context.Background(), "C1", true); err != nil {
		t.Fatal(err)
	}
	p := f.last("users.prefs.setNotifications")
	if p.Get("channel_ids") != "C1" || p.Get("global") != "false" || !strings.Contains(p.Get("prefs"), `"name":"muted","value":"true"`) {
		t.Errorf("mute params: %v", p)
	}
	n := a.NotifyPrefs("C1")
	if !n.Muted || n.Desktop != "everything" || !n.FollowAllThreads || a.Prefs().GlobalDesktop != "mentions_dms" {
		t.Errorf("notify prefs: %+v", n)
	}
	if c, _ := a.Conv("C1"); !c.Muted {
		t.Error("conv not muted")
	}
}

func TestUploadProgressAndPrepared(t *testing.T) {
	a, f := testApp(t)
	f.reply["files.getUploadURLExternal"] = `{"ok":true,"upload_url":"` + f.srv.URL + `/upload/x","file_id":"F9"}`
	f.reply["x"] = `ok`
	f.reply["files.completeUploadExternal"] = `{"ok":true,"files":[{"id":"F8","title":"pre"},{"id":"F9","title":"a.txt"}]}`
	var calls int
	var last, total int64
	out, err := a.Upload(context.Background(), UploadOpts{Channel: "C1", Comment: "c",
		Files: []UploadFile{{Name: "a.txt", Data: make([]byte, 200<<10)}}, Prepared: []PreparedUpload{{ID: "F8", Title: "pre"}},
		Progress: func(s, t int64) { calls++; last, total = s, t }})
	if err != nil || len(out) != 2 {
		t.Fatalf("upload: %+v %v", out, err)
	}
	if calls < 2 || last != total || total != 200<<10 {
		t.Errorf("progress: calls=%d last=%d total=%d", calls, last, total)
	}
	files := f.last("files.completeUploadExternal").Get("files")
	if !strings.Contains(files, `"F8"`) || !strings.Contains(files, `"F9"`) {
		t.Errorf("complete files param: %s", files)
	}
	pu, err := a.PrepareUpload(context.Background(), UploadFile{Name: "b.txt", Data: []byte("hi")}, nil)
	if err != nil || pu.ID != "F9" || pu.Size != 2 {
		t.Errorf("prepare: %+v %v", pu, err)
	}
}

func TestCustomEmojiSnapshotIsStable(t *testing.T) {
	a, _ := testApp(t)
	a.S.CustomEmoji.Set(map[string]string{"p": "https://e.example/p.gif"})
	snap := a.CustomEmoji()
	a.handle(rtm("emoji_changed", map[string]any{"subtype": "add", "name": "q", "value": "https://e.example/q.gif"}))
	if len(snap) != 1 || len(a.CustomEmoji()) != 2 {
		t.Errorf("snapshot %d, now %d", len(snap), len(a.CustomEmoji()))
	}
}

func tss(ms []slack.Message) string {
	var out []string
	for _, m := range ms {
		out = append(out, m.TS)
	}
	return strings.Join(out, ",")
}

func TestCommandsAppInfo(t *testing.T) {
	a, f := testApp(t)
	f.reply["commands.list"] = `{"ok":true,"commands":{"/give":{"name":"/give","type":"app","app":"A1","app_name":"Giver","icons":{"image_36":"https://a.example/36.png","image_72":"https://a.example/72.png","image_default":true}},"/me":{"name":"/me","type":"core"}}}`
	cs, err := a.Commands(context.Background(), "")
	if err != nil || len(cs) != 2 || cs[0].Name != "/give" || cs[0].AppName != "Giver" || cs[0].IconURL != "https://a.example/72.png" || cs[1].IconURL != "" {
		t.Errorf("commands: %+v %v", cs, err)
	}
}

func TestSetNotifyPrefsSyncRule(t *testing.T) {
	a, f := testApp(t)
	f.reply["users.prefs.setNotifications"] = `{"ok":true}`
	f.reply["users.prefs.get"] = `{"ok":true,"prefs":{}}`
	ctx := context.Background()
	_ = a.SetNotifyPrefs(ctx, "C1", ChannelNotify{Desktop: "everything"})
	p := f.last("users.prefs.setNotifications")
	if p.Get("sync") != "true" || strings.Contains(p.Get("prefs"), `"mobile"`) || !strings.Contains(p.Get("prefs"), `"name":"desktop","value":"everything","sync":true`) {
		t.Errorf("synced: %v", p)
	}
	_ = a.SetNotifyPrefs(ctx, "C1", ChannelNotify{Desktop: "everything", Mobile: "nothing"})
	p = f.last("users.prefs.setNotifications")
	if p.Get("sync") != "false" || !strings.Contains(p.Get("prefs"), `"name":"mobile","value":"nothing"`) || strings.Contains(p.Get("prefs"), `"sync":true`) {
		t.Errorf("unsynced: %v", p)
	}
}

func TestJoinedBrowseSortsByName(t *testing.T) {
	p := channelBrowseParams(ChannelBrowseOpts{Membership: "joined"})
	if p.Get("search_only_my_channels") != "true" || p.Get("sort") != "name" {
		t.Errorf("joined params: %v", p)
	}
	if p := channelBrowseParams(ChannelBrowseOpts{Membership: "joined", Sort: "member_count", Desc: true}); p.Get("sort") != "member_count" {
		t.Errorf("explicit sort overridden: %v", p)
	}
}

func TestAddToDM(t *testing.T) {
	a, f := testApp(t)
	f.reply["conversations.open"] = `{"ok":true,"channel":{"id":"G9","is_mpim":true,"members":["USELF","U1","U2"]}}`
	c, err := a.AddToDM(context.Background(), "D1", "U2", "USELF")
	if err != nil || c.ID != "G9" || c.Kind != "mpim" {
		t.Fatalf("AddToDM: %+v %v", c, err)
	}
	if got := f.last("conversations.open").Get("users"); got != "U1,U2" {
		t.Errorf("opened with %q", got)
	}
	if _, err := a.AddToDM(context.Background(), "C1", "U2"); err == nil {
		t.Error("AddToDM on a channel should fail")
	}
}

// A star or save made while the boot's stars.list / saved.list is in
// flight survives the (older) list landing after it. TestCreateChannel
// flaked on this: "Starred" stars the new channel right after Boot, and
// the boot extras then replaced the starred set with one read before.
func TestExtrasKeepEditsMadeMeanwhile(t *testing.T) {
	a, f := testApp(t)
	f.reply["stars.list"] = `{"ok":true,"items":[{"type":"channel","channel":"C9"}],"paging":{"pages":1}}`
	f.reply["saved.list"] = `{"ok":true,"saved_items":[{"item_id":"C9","item_type":"message","ts":"9.000001"}]}`
	f.reply["stars.add"] = `{"ok":true}`
	f.reply["stars.remove"] = `{"ok":true}`
	entered, release := make(chan string, 4), make(chan struct{})
	hold := func(url.Values) { entered <- "list"; <-release }
	f.hooks["stars.list"], f.hooks["saved.list"] = hold, hold
	done := make(chan struct{})
	go func() { a.loadStars(context.Background()); close(done) }()
	<-entered
	if err := a.Star(context.Background(), "C1", true); err != nil {
		t.Fatal(err)
	}
	if err := a.Star(context.Background(), "C9", false); err != nil {
		t.Fatal(err)
	}
	release <- struct{}{}
	<-done
	if !a.Starred("C1") || a.Starred("C9") {
		t.Errorf("after stars.list: C1 starred=%v (want true), C9 starred=%v (want false)", a.Starred("C1"), a.Starred("C9"))
	}

	done = make(chan struct{})
	go func() { a.loadSaved(context.Background()); close(done) }()
	<-entered
	a.markSaved("C1", "1.000001", true)
	a.markSaved("C9", "9.000001", false)
	close(release) // both saved.list pages
	<-done
	a.mu.Lock()
	s1, s9 := a.saved[savedKey("C1", "1.000001")], a.saved[savedKey("C9", "9.000001")]
	a.mu.Unlock()
	if !s1 || s9 {
		t.Errorf("after saved.list: C1 saved=%v (want true), C9 saved=%v (want false)", s1, s9)
	}
}
