package core

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/store"
)

// All fixtures are synthetic. A fake Slack API records calls; nothing here
// talks to the network.

type fakeSlack struct {
	srv   *httptest.Server
	mu    sync.Mutex
	calls []string     // method names
	forms []url.Values // their params
	reply map[string]string
	hooks map[string]func(url.Values) // run before replying (e.g. inject a realtime echo)
}

func newFake(t *testing.T) *fakeSlack {
	f := &fakeSlack{reply: map[string]string{}, hooks: map[string]func(url.Values){}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		f.mu.Lock()
		f.calls = append(f.calls, method)
		f.forms = append(f.forms, r.PostForm)
		body, ok := f.reply[method]
		hook := f.hooks[method]
		f.mu.Unlock()
		if hook != nil {
			hook(r.PostForm)
		}
		if !ok {
			body = `{"ok":false,"error":"not_in_test"}`
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(body))
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeSlack) last(method string) url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.calls) - 1; i >= 0; i-- {
		if f.calls[i] == method {
			return f.forms[i]
		}
	}
	return nil
}

func testApp(t *testing.T) (*App, *fakeSlack) {
	f := newFake(t)
	c := slack.New(slack.Creds{Token: "xoxc-test", UserID: "USELF", Team: "T1", TeamURL: f.srv.URL + "/"})
	s := store.New("")
	s.PutUsers([]slack.User{
		{ID: "USELF", Name: "me", Profile: slack.Profile{DisplayName: "Me", AvatarHash: "h1"}},
		{ID: "U1", Name: "alice", Profile: slack.Profile{DisplayName: "Alice"}},
		{ID: "U2", Name: "bob", Profile: slack.Profile{RealName: "Bob B"}},
		{ID: "UB", Name: "bot", IsBot: true},
	})
	s.PutConv(slack.Conversation{ID: "C1", Name: "general", IsChannel: true, NumMembers: 3})
	s.PutConv(slack.Conversation{ID: "D1", IsIM: true, User: "U1"})
	a := New(c, s)
	a.S.Self = *mustUser(s, "USELF")
	return a, f
}

func mustUser(s *store.Store, id string) *slack.User {
	u, _ := s.User(id)
	return u
}

func raw(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func rtm(typ string, fields map[string]any) slack.Event {
	fields["type"] = typ
	ev := slack.Event{Type: typ, Raw: raw(fields)}
	if ch, ok := fields["channel"]; ok {
		ev.Channel = raw(ch)
	}
	return ev
}

// next waits for the next event of a type (skipping others).
func next(t *testing.T, ch <-chan Event, typ string) Event {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-ch:
			if ev.Type == typ {
				return ev
			}
		case <-deadline:
			t.Fatalf("no %q event", typ)
		}
	}
}

func TestMessageConversion(t *testing.T) {
	a, _ := testApp(t)
	m := slack.Message{TS: "1700000000.000100", User: "U1", Text: "hello world",
		Edited:     &slack.Edited{User: "U1", TS: "1700000001.000000"},
		Reactions:  []slack.Reaction{{Name: "+1", Count: 2, Users: []string{"U1", "USELF"}}, {Name: "eyes", Count: 1, Users: []string{"U2"}}},
		ReplyCount: 2, ThreadTS: "1700000000.000100", PinnedTo: []string{"C1"}}
	v := a.message("C1", &m)
	if !v.Edited || v.EditedTS != "1700000001.000000" || !v.Pinned || v.IsBot || v.ReplyCount != 2 || v.Channel != "C1" {
		t.Errorf("flags: %+v", v)
	}
	if !v.Reactions[0].Self || v.Reactions[1].Self || !strings.Contains(v.Reactions[0].URL, "1f44d") {
		t.Errorf("reactions: %+v", v.Reactions)
	}
	if !strings.Contains(richtext.PlainText(v.Doc), "hello world") {
		t.Errorf("doc: %q", richtext.PlainText(v.Doc))
	}
	bot := slack.Message{TS: "2", BotID: "B1", Subtype: "bot_message", Username: "Deploy",
		BotProfile: &slack.BotProfile{Name: "deploybot", Icons: slack.Icons{Image72: "https://img.example/b.png"}}}
	if bv := a.message("C1", &bot); !bv.IsBot || bv.Name != "Deploy" || bv.Avatar != "https://img.example/b.png" {
		t.Errorf("bot: %+v", bv)
	}
	if !a.message("C1", &slack.Message{TS: "3", Subtype: "channel_join", User: "U1"}).System {
		t.Error("channel_join should be System")
	}
	icon := slack.Message{TS: "4", BotID: "B2", Icons: &slack.Icons{Emoji: ":tada:"}}
	if iv := a.message("C1", &icon); !strings.Contains(iv.Avatar, "1f389") {
		t.Errorf("icon_emoji avatar: %q", iv.Avatar)
	}
	// timeline hides thread replies but keeps broadcasts
	tl := a.timeline("C1", []slack.Message{{TS: "1.0"}, {TS: "1.1", ThreadTS: "1.0"}, {TS: "1.2", ThreadTS: "1.0", Subtype: "thread_broadcast"}})
	if len(tl) != 2 || tl[1].TS != "1.2" || !tl[1].Broadcast {
		t.Errorf("timeline: %+v", tl)
	}
}

func TestUsersAndConvs(t *testing.T) {
	a, _ := testApp(t)
	u, ok := a.User("USELF")
	if !ok || u.Name != "Me" || u.Avatar != "https://ca.slack-edge.com/T0266FRGM-USELF-h1-72" || u.Avatar512 == u.Avatar {
		t.Errorf("user: %+v", u)
	}
	if b, _ := a.User("UB"); !b.IsBot {
		t.Error("bot user")
	}
	if c, _ := a.Conv("D1"); c.Kind != "im" || c.Name != "Alice" || c.User != "U1" {
		t.Errorf("im conv: %+v", c)
	}
	if r := a.ConvRef("G9"); r.Kind != "private" || r.Name != "G9" {
		t.Errorf("ref: %+v", r)
	}
	a.S.PutName("C9", "elsewhere")
	if r := a.ConvRef("C9"); r.Name != "elsewhere" || r.Kind != "channel" {
		t.Errorf("named ref: %+v", r)
	}
}

func TestSnapshotAndSections(t *testing.T) {
	a, _ := testApp(t)
	s := a.S
	s.PutConv(slack.Conversation{ID: "C4", Name: "alpha", IsChannel: true})
	s.PutConv(slack.Conversation{ID: "C2", Name: "starred", IsChannel: true})
	s.PutConv(slack.Conversation{ID: "G3", Name: "proj", IsPrivate: true})
	s.PutConv(slack.Conversation{ID: "D2", IsIM: true, User: "UB"})
	sec := func(id, typ, name, next string, chans ...string) slack.Section {
		x := slack.Section{ID: id, Type: typ, Name: name, Next: next}
		x.ChannelIDsPage.ChannelIDs = chans
		return x
	}
	s.Sections = []slack.Section{
		sec("L4", "direct_messages", "", ""), sec("L2", "standard", "Proj", "L3", "G3", "CGONE"),
		sec("L1", "stars", "", "L2", "C2"), sec("L3", "channels", "", "L4"), sec("L5", "slack_connect", "", ""),
		sec("L6", "standard", "Orphan", "XX"), // broken link: still shown, last
	}
	s.SetCounts(&slack.Counts{Channels: []slack.CountEntry{{ID: "C1", HasUnreads: true, MentionCount: 2}}})
	close(a.ready)
	snap := a.Snapshot()
	var got []string
	for _, v := range snap.Sections {
		got = append(got, v.Type+":"+v.Name+"="+strings.Join(v.Channels, ","))
	}
	want := "stars:Starred=C2|standard:Proj=G3|channels:Channels=C4,C1|direct_messages:Direct messages=D1|standard:Orphan=|apps:Apps=D2"
	if strings.Join(got, "|") != want {
		t.Errorf("sections:\n got %s\nwant %s", strings.Join(got, "|"), want)
	}
	if snap.Self.ID != "USELF" || snap.Counts["C1"].Mentions != 2 || !snap.Counts["C1"].Unread || len(snap.Convs) != 6 {
		t.Errorf("snapshot: self=%v counts=%v convs=%d", snap.Self.ID, snap.Counts, len(snap.Convs))
	}
	if _, ok := snap.Users["U1"]; !ok {
		t.Error("snapshot users missing IM partner")
	}
	for i := 1; i < len(snap.Convs); i++ {
		if snap.Convs[i-1].Name > snap.Convs[i].Name {
			t.Error("convs not sorted")
		}
	}
}

func TestRealtimeEvents(t *testing.T) {
	a, _ := testApp(t)
	a.S.SetLatest("C1", []slack.Message{{TS: "100.000001", User: "U1", Text: "root"}}, false)
	a.S.SetCounts(&slack.Counts{Channels: []slack.CountEntry{{ID: "C1", LastRead: "100.000001", Latest: "100.000001"}}})
	evs, stop := a.Subscribe()
	defer stop()

	a.handle(rtm("message", map[string]any{"channel": "C1", "ts": "101.000000", "user": "U1", "text": "hi <@USELF>"}))
	ev := next(t, evs, "message")
	if !ev.Mention || ev.Reply || ev.Msg == nil || ev.Msg.TS != "101.000000" || ev.Count == nil || ev.Count.Mentions != 1 || !ev.Count.Unread || ev.ConvKind != "channel" || ev.User == nil || ev.User.Name != "Alice" {
		t.Fatalf("message event: %+v", ev)
	}
	if tl := a.S.Timeline("C1"); len(tl.Msgs) != 2 {
		t.Error("message not appended to the timeline")
	}

	a.handle(rtm("message", map[string]any{"channel": "C1", "ts": "102.000000", "user": "U2", "text": "reply", "thread_ts": "100.000001"}))
	up := next(t, evs, "update")
	if up.Msg.TS != "100.000001" || up.Msg.ReplyCount != 1 || up.Msg.LatestReply != "102.000000" {
		t.Errorf("parent update: %+v", up.Msg)
	}
	if rep := next(t, evs, "message"); !rep.Reply {
		t.Error("reply not flagged")
	}

	a.handle(rtm("reaction_added", map[string]any{"user": "U2", "reaction": "eyes", "item": map[string]any{"channel": "C1", "ts": "101.000000"}}))
	if up := next(t, evs, "update"); len(up.Msg.Reactions) != 1 || up.Msg.Reactions[0].Name != "eyes" {
		t.Errorf("reaction update: %+v", up.Msg)
	}

	a.handle(rtm("message", map[string]any{"channel": "C1", "subtype": "message_changed",
		"message": map[string]any{"ts": "101.000000", "user": "U1", "text": "edited", "edited": map[string]any{"user": "U1", "ts": "105.0"}}}))
	if up := next(t, evs, "update"); !up.Msg.Edited || up.Msg.Text != "edited" {
		t.Errorf("edit update: %+v", up.Msg)
	}

	a.handle(rtm("pin_added", map[string]any{"channel_id": "C1", "item": map[string]any{"type": "message", "channel": "C1", "message": map[string]any{"ts": "101.000000"}}}))
	if up := next(t, evs, "update"); !up.Msg.Pinned {
		t.Error("pin_added (channel_id payload) did not pin")
	}

	a.handle(rtm("message", map[string]any{"channel": "C1", "subtype": "message_deleted", "deleted_ts": "101.000000"}))
	if d := next(t, evs, "delete"); d.TS != "101.000000" || d.Channel != "C1" {
		t.Errorf("delete: %+v", d)
	}
	if _, ok := a.S.Find("C1", "101.000000"); ok {
		t.Error("deleted message still cached")
	}

	a.handle(rtm("channel_marked", map[string]any{"channel": "C1", "ts": "102.000000", "unread_count_display": 0, "mention_count_display": 0}))
	if m := next(t, evs, "marked"); m.Count.Unread || m.Count.Mentions != 0 || m.Count.LastRead != "102.000000" {
		t.Errorf("marked: %+v", m.Count)
	}

	a.handle(rtm("presence_change", map[string]any{"users": []string{"U1", "U2"}, "presence": "away"}))
	if p := next(t, evs, "presence"); len(p.Users) != 2 || p.Presence != "away" {
		t.Errorf("presence: %+v", p)
	}
	if u, _ := a.User("U2"); u.Presence != "away" {
		t.Error("presence not stored")
	}

	a.handle(rtm("user_typing", map[string]any{"channel": "C1", "user": "USELF"})) // own typing: ignored
	a.handle(rtm("user_typing", map[string]any{"channel": "C1", "user": "U1", "thread_ts": "100.000001"}))
	if ty := next(t, evs, "typing"); ty.UserID != "U1" || ty.Name != "Alice" || ty.ThreadTS != "100.000001" {
		t.Errorf("typing: %+v", ty)
	}

	a.handle(rtm("message", map[string]any{"channel": "D1", "ts": "200.000000", "user": "U1", "text": "dm"}))
	if dm := next(t, evs, "message"); dm.Count.Mentions != 1 || dm.ConvKind != "im" {
		t.Errorf("DM should badge: %+v", dm.Count)
	}
	a.handle(rtm("channel_left", map[string]any{"channel": "C1"}))
	if r := next(t, evs, "conv_removed"); r.Channel != "C1" {
		t.Error("conv_removed")
	}
}

func TestSubscribeOverflowResync(t *testing.T) {
	a, _ := testApp(t)
	evs, stop := a.Subscribe()
	for i := 0; i < SubscriberBuffer+10; i++ {
		a.emit(Event{Type: "counts"})
	}
	for i := 0; i < SubscriberBuffer; i++ {
		<-evs
	}
	a.emit(Event{Type: "connection"})
	if ev := <-evs; ev.Type != "resync" {
		t.Errorf("expected resync after overflow, got %q", ev.Type)
	}
	if ev := <-evs; ev.Type != "connection" {
		t.Errorf("expected the new event after resync, got %q", ev.Type)
	}
	stop()
	stop() // idempotent
	if _, ok := <-evs; ok {
		t.Error("channel not closed after stop")
	}
	a.emit(Event{Type: "counts"}) // no subscribers: no panic
}

func TestHistoryFromCacheAndMarks(t *testing.T) {
	a, f := testApp(t)
	f.reply["conversations.mark"] = `{"ok":true}`
	a.S.SetLatest("C1", []slack.Message{{TS: "1.000001", User: "U1", Text: "a"}, {TS: "1.000002", User: "U2", Text: "b", ThreadTS: "1.000001"}, {TS: "1.000003", User: "U1", Text: "c"}}, true)
	a.S.SetCounts(&slack.Counts{Channels: []slack.CountEntry{{ID: "C1", LastRead: "1.000003", Latest: "1.000003"}}})
	p, err := a.History(context.Background(), "C1", "")
	if err != nil || len(p.Messages) != 2 || !p.HasMore || p.Conv == nil || p.Conv.Name != "general" || p.LastRead != "1.000003" {
		t.Fatalf("history: %+v %v", p, err)
	}
	if len(f.calls) != 0 {
		t.Errorf("cached history made calls: %v", f.calls)
	}
	cv, err := a.MarkUnread(context.Background(), "C1", "1.000003")
	if err != nil || !cv.Unread {
		t.Fatalf("mark unread: %+v %v", cv, err)
	}
	if got := f.last("conversations.mark")["ts"]; len(got) != 1 || got[0] != "1.000002" {
		t.Errorf("mark unread sent ts %v, want the previous message", got)
	}
	cv, _ = a.Mark(context.Background(), "C1", "1.000003")
	if cv.Unread {
		t.Error("mark read left it unread")
	}
	for in, want := range map[string]string{"5.000010": "5.000009", "5.000000": "4.999999", "bad": "bad"} {
		if got := tsBefore(nil, in); got != want {
			t.Errorf("tsBefore(%s) = %s", in, got)
		}
	}
}

func TestWritesHitTheRightMethods(t *testing.T) {
	a, f := testApp(t)
	for _, m := range []string{"reactions.add", "pins.add", "saved.add", "saved.delete", "chat.update", "chat.delete"} {
		f.reply[m] = `{"ok":true}`
	}
	f.reply["reactions.remove"] = `{"ok":false,"error":"no_reaction"}`
	f.reply["pins.remove"] = `{"ok":false,"error":"no_pin"}`
	f.reply["chat.postMessage"] = `{"ok":true,"ts":"9.000001","message":{"ts":"9.000001","user":"USELF","text":"x"}}`
	ctx := context.Background()
	if m, err := a.Send(ctx, SendOpts{Channel: "C1", Text: "x", ThreadTS: "1.0", Broadcast: true}); err != nil || m.TS != "9.000001" {
		t.Fatalf("send: %+v %v", m, err)
	}
	if p := f.last("chat.postMessage"); p["reply_broadcast"][0] != "true" || p["client_msg_id"][0] == "" {
		t.Errorf("send params: %v", p)
	}
	if err := a.React(ctx, "C1", "1.0", ":eyes:", true); err != nil || f.last("reactions.add")["name"][0] != "eyes" {
		t.Errorf("react add: %v", err)
	}
	if err := a.React(ctx, "C1", "1.0", "eyes", false); err != nil {
		t.Errorf("react remove should swallow no_reaction: %v", err)
	}
	if err := a.Pin(ctx, "C1", "1.0"); err != nil || f.last("pins.add")["timestamp"][0] != "1.0" {
		t.Errorf("pin: %v", err)
	}
	if err := a.Unpin(ctx, "C1", "1.0"); err != nil {
		t.Errorf("unpin should swallow no_pin: %v", err)
	}
	if err := a.Save(ctx, "C1", "1.0", 0); err != nil || f.last("saved.add")["item_id"][0] != "C1" {
		t.Errorf("save: %v", err)
	}
	if err := a.Unsave(ctx, "C1", "1.0"); err != nil {
		t.Errorf("unsave: %v", err)
	}
	if err := a.Edit(ctx, "C1", "1.0", "new", nil); err != nil {
		t.Errorf("edit: %v", err)
	}
	if err := a.Delete(ctx, "C1", "1.0"); err != nil {
		t.Errorf("delete: %v", err)
	}
	if _, err := a.Send(ctx, SendOpts{}); err == nil {
		t.Error("send without channel should fail")
	}
	if err := a.SetPresence(ctx, "busy"); err == nil {
		t.Error("bad presence accepted")
	}
}

func TestFetchMemoryCache(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Header.Get("Cookie") != "d=xoxd-test" {
			http.Error(w, "no cookie", 403)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Write([]byte("PNGDATA" + r.URL.Path))
	}))
	defer srv.Close()
	testAllowHTTP = true
	oldAllowed := fetchHostAllowed
	fetchHostAllowed = func(h string) bool { return h == "127.0.0.1" }
	defer func() { testAllowHTTP = false; fetchHostAllowed = oldAllowed }()

	a := New(slack.New(slack.Creds{Cookie: "xoxd-test", UserID: "USELF"}), store.New(""))
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		b, mime, err := a.Fetch(ctx, srv.URL+"/a.png")
		if err != nil || string(b) != "PNGDATA/a.png" || mime != "image/png" {
			t.Fatalf("fetch: %q %q %v", b, mime, err)
		}
	}
	if hits.Load() != 1 {
		t.Errorf("cache miss: %d server hits", hits.Load())
	}
	if _, _, err := a.Fetch(ctx, "https://evil.example/x.png"); err == nil {
		t.Error("foreign host fetched")
	}

	c := newFileCache(10)
	c.put("a", fileEntry{data: []byte("12345")})
	c.put("b", fileEntry{data: []byte("12345")})
	c.get("a")
	c.put("c", fileEntry{data: []byte("12345")}) // evicts b (least recent)
	if _, ok := c.get("b"); ok {
		t.Error("LRU kept the least recently used entry")
	}
	if _, ok := c.get("a"); !ok {
		t.Error("LRU evicted a recently used entry")
	}
	c.put("huge", fileEntry{data: make([]byte, 11)})
	if _, ok := c.get("huge"); ok {
		t.Error("oversized entry cached")
	}
}

func TestMentionsAndHelpers(t *testing.T) {
	for text, want := range map[string]bool{"hi <@USELF>": true, "<!here>": true, "<!channel> x": true, "hi <@U2>": false, "USELF": false} {
		if mentionsSelf(&slack.Message{Text: text}, "USELF") != want {
			t.Errorf("mentionsSelf(%q)", text)
		}
	}
	m := &slack.Message{}
	applyReaction(m, "x", "U1", true)
	applyReaction(m, "x", "U1", true)
	applyReaction(m, "x", "U1", false)
	if len(m.Reactions) != 0 {
		t.Errorf("reactions: %+v", m.Reactions)
	}
	if !needsFileInfo(&slack.Message{Files: []slack.File{{ID: "F1", FileAccess: "check_file_info"}}}) || needsFileInfo(&slack.Message{Files: []slack.File{{ID: "F1", Mode: "tombstone"}}}) {
		t.Error("needsFileInfo")
	}
	a, _ := testApp(t)
	ids := strings.Join(a.presenceIDs([]string{"U9", "B1", "U9"}), ",")
	if !strings.Contains(ids, "USELF") || !strings.Contains(ids, "U1") || strings.Count(ids, "U9") != 1 || strings.Contains(ids, "B1") {
		t.Errorf("presenceIDs = %s", ids)
	}
}
