package core

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"testing"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// The message change table (msg_changes.go) keeps each message's latest
// "update" or "delete", stamps every event with its place in the order
// before any subscriber gets it, keeps a reactions update's rest from the
// change before, and says when a reader is further behind than it names.
func TestMessageChangeTable(t *testing.T) {
	a := New(slack.New(slack.Creds{Token: "xoxc-test"}), store.New(""))
	evs, stop := a.Subscribe()
	defer stop()
	const ch, ts = "C1", "1.000001"
	start := a.MessageChangeSeq()
	a.emit(Event{Type: "update", Channel: ch, TS: ts, Msg: &Message{TS: ts, Text: "a"}})
	a.emit(Event{Type: "message", Channel: ch, TS: "2.000001"}) // a new message isn't a change
	a.emit(Event{Type: "update", Name: UpdateReactions, Channel: ch, TS: ts,
		Msg: &Message{TS: ts, Text: "the channel's copy", Reactions: []Reaction{{Name: "tada", Count: 1}}}})
	for _, want := range []uint64{start + 1, 0, start + 2} {
		if ev := <-evs; ev.Seq != want {
			t.Errorf("%s event stamped %d, want %d", ev.Type, ev.Seq, want)
		}
	}
	c, ok := a.MessageChange(ch, ts)
	if !ok || c.Seq != start+2 || c.FullSeq != start+1 || c.Deleted || c.Msg == nil || c.Msg.Text != "a" || len(c.Msg.Reactions) != 1 {
		t.Fatalf("after an update and a reaction: %+v (%v)", c, ok)
	}
	// a copy from before both takes the whole message; one from between
	// them only the reactions
	if m, _ := a.ChangedMessage(ch, ts, &c, &Message{TS: ts, Text: "older"}, start); m.Text != "a" {
		t.Errorf("a copy older than the edit drew %q", m.Text)
	}
	if m, _ := a.ChangedMessage(ch, ts, &c, &Message{TS: ts, Text: "mine"}, start+1); m.Text != "mine" || len(m.Reactions) != 1 {
		t.Errorf("a copy with the edit drew %q %+v", m.Text, m.Reactions)
	}
	keys, now, ok := a.MessageChangedSince(start)
	if !ok || now != start+2 || len(keys) != 2 || keys[0] != (MsgKey{ch, ts}) || keys[1] != keys[0] {
		t.Errorf("MessageChangedSince(%d) = %v, %d, %v", start, keys, now, ok)
	}
	if keys, _, ok := a.MessageChangedSince(now); !ok || len(keys) != 0 {
		t.Errorf("nothing changed since %d: %v %v", now, keys, ok)
	}
	a.emit(Event{Type: "delete", Channel: ch, TS: ts})
	if c, _ := a.MessageChange(ch, ts); !c.Deleted || c.Msg != nil || c.RemoveSeq != c.Seq || c.Seq != start+3 {
		t.Errorf("after the delete: %+v", c)
	}
	// the latest maxChangeCopies changes keep their message, older ones
	// don't (the store has it)
	a.emit(Event{Type: "update", Channel: ch, TS: "3.000001", Msg: &Message{TS: "3.000001", Text: "kept"}})
	for i := 0; i < maxChangeCopies; i++ {
		a.emit(Event{Type: "update", Channel: "C2", TS: fmt.Sprintf("%d.000001", 10+i), Msg: &Message{Text: "other"}})
	}
	if c, ok := a.MessageChange(ch, "3.000001"); !ok || c.Msg != nil || c.Seq == 0 {
		t.Errorf("a change %d changes ago still keeps its copy: %+v", maxChangeCopies, c)
	}
	if _, _, ok := a.MessageChangedSince(start); !ok {
		t.Error("a reader fewer than maxRecentChanges behind was told to look everything up")
	}
	for i := 0; i < maxRecentChanges; i++ {
		a.emit(Event{Type: "update", Channel: "C3", TS: "1.000001", Msg: &Message{Text: "busy"}})
	}
	if _, _, ok := a.MessageChangedSince(start); ok {
		t.Error("a reader more than maxRecentChanges behind was named the changes")
	}
	if c, ok := a.MessageChange(ch, ts); !ok || !c.Deleted {
		t.Errorf("the table lost the delete under other changes: %+v %v", c, ok)
	}
}

// Past maxChangeKeys messages the table forgets the ones changed longest
// ago, and its floor says how far.
func TestMessageChangeTableForgets(t *testing.T) {
	a := New(slack.New(slack.Creds{Token: "xoxc-test"}), store.New(""))
	for i := 0; i <= maxChangeKeys; i++ {
		a.emit(Event{Type: "delete", Channel: "C1", TS: fmt.Sprintf("%d.000001", 100+i)})
	}
	f := a.MessageChangeFloor()
	if f == 0 {
		t.Fatal("no floor after more than maxChangeKeys messages")
	}
	if _, ok := a.MessageChange("C1", "100.000001"); ok {
		t.Error("the first message is still known")
	}
	if c, ok := a.MessageChange("C1", fmt.Sprintf("%d.000001", 100+maxChangeKeys)); !ok || c.Seq <= f {
		t.Errorf("the latest message: %+v %v, floor %d", c, ok, f)
	}
}

// A reaction to a message the store holds only as fetched by id (an
// Activity entry's, from a conversation nobody opened) changes that copy
// and goes out as an "update", as Slack's reactions reducer reaches every
// message of its store (store/fetched.go).
func TestReactionReachesFetchedMessage(t *testing.T) {
	a := New(slack.New(slack.Creds{Token: "xoxc-test"}), store.New(""))
	const ch, ts = "C0LIVE0001", "1762790200.000100"
	a.S.PutFetched(ch, []slack.Message{{Type: "message", TS: ts, User: "U2", Text: "a synthetic mention"}}, a.S.LiveSeq())
	start := a.MessageChangeSeq()
	raw, _ := json.Marshal(map[string]any{"type": "reaction_added", "user": "U1", "reaction": "tada",
		"item": map[string]any{"type": "message", "channel": ch, "ts": ts}})
	a.Inject(slack.Event{Type: "reaction_added", Raw: raw})
	c, ok := a.MessageChange(ch, ts)
	if !ok || c.Seq != start+1 || c.Msg == nil || len(c.Msg.Reactions) != 1 {
		t.Fatalf("the reaction left %+v (%v); want one reactions update", c, ok)
	}
	if m, ok := a.S.FindHeld(ch, ts); !ok || len(m.Reactions) != 1 || m.Reactions[0].Name != "tada" {
		t.Errorf("the fetched copy: %v %+v", ok, m.Reactions)
	}
}

// A reaction that lands while messages.list is out reaches the answer:
// the hydrate replays it on the copy it keeps (store/reacts.go), the
// first time a message is fetched (no copy for the reaction to change
// then) and after, and a reaction the answer already counts isn't counted
// twice.
func TestHydrateKeepsReactionsDuringFetch(t *testing.T) {
	a, f := testApp(t)
	const ch, ts = "C1", "1762790300.000100"
	react := func(typ, user string) {
		raw, _ := json.Marshal(map[string]any{"type": typ, "user": user, "reaction": "+1",
			"item": map[string]any{"type": "message", "channel": ch, "ts": ts}})
		a.Inject(slack.Event{Type: typ, Raw: raw})
	}
	answer := func(users ...string) string {
		us, _ := json.Marshal(users)
		return fmt.Sprintf(`{"ok":true,"messages":{"C1":[{"ts":%q,"user":"U1","text":"a synthetic poll",
			"reactions":[{"name":"+1","count":%d,"users":%s}]}]}}`, ts, len(users), us)
	}
	count := func(m slack.Message) (int, []string) {
		for _, r := range m.Reactions {
			if r.Name == "+1" {
				return r.Count, r.Users
			}
		}
		return 0, nil
	}
	want := map[string][]string{ch: {ts}}
	// the first fetch: U2 reacts while it's out, the answer predates it
	f.reply["messages.list"] = answer("U1")
	f.hooks["messages.list"] = func(url.Values) { react("reaction_added", "U2") }
	got := a.hydrate(context.Background(), want)
	if n, us := count(got[ch+"/"+ts]); n != 2 || len(us) != 2 {
		t.Fatalf("first fetch: +1 x%d %v, want U1 and U2's", n, us)
	}
	// again: U1 takes theirs back meanwhile; the answer still has it
	f.reply["messages.list"] = answer("U1", "U2")
	f.hooks["messages.list"] = func(url.Values) { react("reaction_removed", "U1") }
	got = a.hydrate(context.Background(), want)
	if n, us := count(got[ch+"/"+ts]); n != 1 || len(us) != 1 || us[0] != "U2" {
		t.Errorf("second fetch: +1 x%d %v, want U2's alone", n, us)
	}
	if m, _ := a.S.FindHeld(ch, ts); func() bool { n, _ := count(m); return n != 1 }() {
		t.Errorf("the store kept %+v", m.Reactions)
	}
	// an answer that already counts the reaction made meanwhile
	f.reply["messages.list"] = answer("U2", "U3")
	f.hooks["messages.list"] = func(url.Values) { react("reaction_added", "U3") }
	got = a.hydrate(context.Background(), want)
	if n, _ := count(got[ch+"/"+ts]); n != 2 {
		t.Errorf("third fetch: +1 x%d, want 2 (U3's once)", n)
	}
	// a thread's replies answer replays them too (SetThreadSince)
	const root, reply = "1762790290.000100", "1762790295.000100"
	f.reply["conversations.replies"] = fmt.Sprintf(`{"ok":true,"has_more":false,"messages":[
		{"ts":%q,"thread_ts":%q,"user":"U1","text":"a synthetic root","reply_count":1},
		{"ts":%q,"thread_ts":%q,"user":"U2","text":"a synthetic reply","reactions":[{"name":"+1","count":1,"users":["U1"]}]}]}`, root, root, reply, root)
	f.hooks["conversations.replies"] = func(url.Values) {
		raw, _ := json.Marshal(map[string]any{"type": "reaction_added", "user": "U3", "reaction": "+1",
			"item": map[string]any{"type": "message", "channel": ch, "ts": reply}})
		a.Inject(slack.Event{Type: "reaction_added", Raw: raw})
	}
	if _, err := a.RefreshReplies(context.Background(), ch, root); err != nil {
		t.Fatal(err)
	}
	th, _ := a.S.Thread(ch, root)
	if len(th) != 2 {
		t.Fatalf("the thread: %+v", th)
	}
	if n, us := count(th[1]); n != 2 || len(us) != 2 {
		t.Errorf("the thread's reply: +1 x%d %v, want U1 and U3's", n, us)
	}
}

// Slack's messageChanged takes an entry out of Activity for a tombstone,
// a moderated or a thread_hidden copy, and for an edit that took the
// viewer's mention out (c+16 of previous_message, and not of the new
// copy): the "update" says so (Event.Removes). Other edits, and copies
// without previous_message, don't.
func TestMessageChangedRemoves(t *testing.T) {
	a, _ := testApp(t)
	a.S.SetMyUsergroup("SMINE0001", true)
	a.S.Lock()
	a.S.Prefs.Keywords = []string{"deploy"}
	a.S.Unlock()
	const ch = "C1"
	rich := func(els string) json.RawMessage {
		return json.RawMessage(`[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[` + els + `]}]}]`)
	}
	evs, stop := a.Subscribe()
	defer stop()
	n := 0
	changed := func(prev, now *slack.Message) Event {
		t.Helper()
		n++
		ts := fmt.Sprintf("17627904%02d.000100", n)
		now.TS, now.Type = ts, "message"
		e := map[string]any{"subtype": "message_changed", "hidden": true, "channel": ch, "ts": ts, "message": now}
		if prev != nil {
			prev.TS, prev.Type = ts, "message"
			e["previous_message"] = prev
		}
		a.handle(rtm("message", e))
		ev := next(t, evs, "update")
		if ev.TS != ts {
			t.Fatalf("the update is of %s, want %s", ev.TS, ts)
		}
		return ev
	}
	for _, c := range []struct {
		name      string
		prev, now slack.Message
		want      bool
	}{
		{"tombstone", slack.Message{Text: "<@USELF> hi"}, slack.Message{Subtype: "tombstone", Text: "This message was deleted."}, true},
		{"moderated", slack.Message{Text: "hi"}, slack.Message{Subtype: "moderated"}, true},
		{"thread_hidden", slack.Message{Text: "hi"}, slack.Message{Subtype: "thread_hidden"}, true},
		{"mention edited out", slack.Message{Text: "<@USELF> look"}, slack.Message{Text: "look"}, true},
		{"mention kept", slack.Message{Text: "<@USELF> look"}, slack.Message{Text: "<@USELF> look again"}, false},
		{"never mentioned", slack.Message{Text: "look"}, slack.Message{Text: "look again"}, false},
		{"mention added", slack.Message{Text: "look"}, slack.Message{Text: "<@USELF> look"}, false},
		{"@here edited out", slack.Message{Text: "<!here> look"}, slack.Message{Text: "look"}, true},
		{"@channel edited out of a root", slack.Message{Text: "<!channel> look"}, slack.Message{Text: "look"}, true},
		{"@channel edited out of a reply", slack.Message{Text: "<!channel> look", ThreadTS: "1762780000.000100"},
			slack.Message{Text: "look", ThreadTS: "1762780000.000100"}, false},
		{"@everyone edited out of a root", slack.Message{Text: "<!everyone> look"}, slack.Message{Text: "look"}, true},
		{"my group edited out", slack.Message{Text: "<!subteam^SMINE0001|@devs> look"}, slack.Message{Text: "look"}, true},
		{"another group edited out", slack.Message{Text: "<!subteam^SOTHER001|@ops> look"}, slack.Message{Text: "look"}, false},
		{"keyword edited out", slack.Message{Text: "time to deploy"}, slack.Message{Text: "time to ship"}, true},
		{"keyword in rich text runs split mid-word", slack.Message{Blocks: rich(`{"type":"text","text":"time to dep"},{"type":"text","text":"loy","style":{"bold":true}}`)},
			slack.Message{Blocks: rich(`{"type":"text","text":"time to ship"}`)}, true},
		{"rich mention edited out", slack.Message{Blocks: rich(`{"type":"user","user_id":"USELF"}`)}, slack.Message{Blocks: rich(`{"type":"text","text":"hi"}`)}, true},
		{"blocks emptied: the text isn't read", slack.Message{Text: "<@USELF>", Blocks: json.RawMessage(`[]`)},
			slack.Message{Text: "<@USELF>", Blocks: json.RawMessage(`[]`)}, false},
	} {
		prev, now := c.prev, c.now
		if ev := changed(&prev, &now); ev.Removes != c.want {
			t.Errorf("%s: Removes %v, want %v", c.name, ev.Removes, c.want)
		}
	}
	if ev := changed(nil, &slack.Message{Text: "look"}); ev.Removes {
		t.Error("an edit without previous_message removed the entry")
	}
}

// checkForMentionInMessage with its default options (MentionsViewer) over
// rich text's highlight words: processChunk joins a section's runs into
// one text, so a word split across runs counts, and a color or an emoji
// that is one of the words matches.
func TestMentionsViewerRichKeywords(t *testing.T) {
	a, _ := testApp(t)
	a.S.Lock()
	a.S.Prefs.Keywords = []string{"deploy", "#ff0000", ":fire:", ":wave::skin-tone-3:"}
	a.S.Unlock()
	rich := func(els string) json.RawMessage {
		return json.RawMessage(`[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[` + els + `]}]}]`)
	}
	for _, c := range []struct {
		name string
		m    Message
		want bool
	}{
		{"a word split across runs", Message{Blocks: rich(`{"type":"text","text":"de"},{"type":"text","text":"ploy now"}`)}, true},
		{"a word inside another", Message{Blocks: rich(`{"type":"text","text":"redeployment"}`)}, false},
		{"a link's text", Message{Blocks: rich(`{"type":"link","url":"https://example.com","text":"deploy notes"}`)}, true},
		{"a link's url", Message{Blocks: rich(`{"type":"link","url":"https://deploy.example.com"}`)}, true},
		{"a user element breaks the word", Message{Blocks: rich(`{"type":"text","text":"dep"},{"type":"user","user_id":"U1"},{"type":"text","text":"loy"}`)}, false},
		{"a color", Message{Blocks: rich(`{"type":"color","value":"#FF0000"}`)}, true},
		{"an emoji", Message{Blocks: rich(`{"type":"emoji","name":"fire"}`)}, true},
		{"an emoji with its skin tone", Message{Blocks: rich(`{"type":"emoji","name":"wave","skin_tone":3}`)}, true},
		{"another skin tone", Message{Blocks: rich(`{"type":"emoji","name":"wave","skin_tone":2}`)}, false},
		{"two sections are two lines", Message{Blocks: json.RawMessage(`[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"dep"}]},{"type":"rich_text_section","elements":[{"type":"text","text":"loy"}]}]}]`)}, false},
		{"a list item", Message{Blocks: json.RawMessage(`[{"type":"rich_text","elements":[{"type":"rich_text_list","style":"bullet","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"deploy"}]}]}]}]`)}, true},
	} {
		if got := a.MentionsViewer(&c.m); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

// A thread root the store holds only as a copy fetched by id (an Activity
// entry's, from a conversation nobody opened) is one core announces and
// reads like any other: thread_subscribed's update goes out for it, and
// its replies turned off show (isThreadLocked).
func TestFetchedRootAnnounced(t *testing.T) {
	a, _ := testApp(t)
	const ch, root = "C1", "1762790400.000100"
	a.S.PutFetched(ch, []slack.Message{{Type: "message", TS: root, ThreadTS: root, User: "U1", Text: "a synthetic root",
		ReplyCount: 2, IsLocked: true}}, a.S.LiveSeq())
	evs, stop := a.Subscribe()
	defer stop()
	a.handle(rtm("thread_subscribed", map[string]any{"subscription": map[string]any{"type": "thread", "channel": ch, "thread_ts": root, "last_read": root}}))
	if e := next(t, evs, "update"); e.TS != root || !e.Msg.FollowingThread {
		t.Errorf("thread_subscribed: update of %s, following %v", e.TS, e.Msg.FollowingThread)
	}
	if !a.ThreadLocked(ch, root) {
		t.Error("the fetched root's replies turned off don't show")
	}
}

// A read that brings a revision of a message the store held another of
// (after a socket gap: a thread read for the first time, its root in a
// window read before the gap) reaches every copy and goes out as a change,
// as Slack's one store takes a replies answer: the window's copy is the
// new one, and an "update" carries it to the views.
func TestReadRevisionAnnounced(t *testing.T) {
	a, f := testApp(t)
	const ch, root, reply = "C1", "1762790600.000100", "1762790610.000100"
	rootAt := func(text string) map[string]any {
		return map[string]any{"type": "message", "ts": root, "thread_ts": root, "user": "U1", "text": text, "reply_count": 1, "latest_reply": reply}
	}
	a.S.SetLatest(ch, []slack.Message{{Type: "message", TS: root, ThreadTS: root, User: "U1", Text: "votes: 5", ReplyCount: 1, LatestReply: reply}}, false)
	b, _ := json.Marshal(map[string]any{"ok": true, "has_more": false, "messages": []any{rootAt("votes: 9"),
		map[string]any{"type": "message", "ts": reply, "thread_ts": root, "user": "U2", "text": "a synthetic reply"}}})
	f.reply["conversations.replies"] = string(b)
	evs, stop := a.Subscribe()
	defer stop()
	start := a.MessageChangeSeq()
	if _, err := a.Replies(context.Background(), ch, root); err != nil { // never cached: read now
		t.Fatal(err)
	}
	if m, ok := a.S.FindHeld(ch, root); !ok || m.Text != "votes: 9" {
		t.Errorf("the store's copy of the root after the read: %q (%v)", m.Text, ok)
	}
	if w := a.S.Timeline(ch); w == nil || len(w.Msgs) != 1 || w.Msgs[0].Text != "votes: 9" {
		t.Errorf("the window's copy of the root after the read: %+v", w)
	}
	c, ok := a.MessageChange(ch, root)
	if !ok || c.Seq <= start || c.Msg == nil || c.Msg.Text != "votes: 9" {
		t.Fatalf("the read left the change table with %+v (%v)", c, ok)
	}
	for {
		if e := next(t, evs, "update"); e.TS == root {
			if e.Msg.Text != "votes: 9" {
				t.Errorf("the root's update carries %q", e.Msg.Text)
			}
			break
		}
	}
	// read again, nothing new: nothing goes out
	now := a.MessageChangeSeq()
	if _, err := a.RefreshReplies(context.Background(), ch, root); err != nil {
		t.Fatal(err)
	}
	if a.MessageChangeSeq() != now {
		t.Errorf("a read with no new revision made %d changes", a.MessageChangeSeq()-now)
	}
}

// A reaction that lands while a window's newest page is out reaches the
// window (the page predates it), as it reaches the copies fetched by id.
func TestWindowReadKeepsReactions(t *testing.T) {
	a, f := testApp(t)
	const ch, ts = "C1", "1762790700.000100"
	f.reply["conversations.history"] = fmt.Sprintf(`{"ok":true,"has_more":false,"messages":[{"type":"message","ts":%q,"user":"U1","text":"a synthetic poll",
		"reactions":[{"name":"+1","count":1,"users":["U1"]}]}]}`, ts)
	f.hooks["conversations.history"] = func(url.Values) {
		raw, _ := json.Marshal(map[string]any{"type": "reaction_added", "user": "U2", "reaction": "+1",
			"item": map[string]any{"type": "message", "channel": ch, "ts": ts}})
		a.Inject(slack.Event{Type: "reaction_added", Raw: raw})
	}
	if _, err := a.History(context.Background(), ch, ""); err != nil {
		t.Fatal(err)
	}
	w := a.S.Timeline(ch)
	if w == nil || len(w.Msgs) != 1 || len(w.Msgs[0].Reactions) != 1 || w.Msgs[0].Reactions[0].Count != 2 {
		t.Fatalf("the window after a reaction during its read: %+v", w)
	}
}

// The replies the Threads view loads into a card ("Show N more replies")
// are kept where the socket's changes reach them: a reaction to one goes
// out as a change.
func TestOlderRepliesStored(t *testing.T) {
	a, f := testApp(t)
	const ch, root, old = "C1", "1762790800.000100", "1762790810.000100"
	f.reply["conversations.replies"] = fmt.Sprintf(`{"ok":true,"has_more":false,"messages":[
		{"type":"message","ts":%q,"thread_ts":%q,"user":"U1","text":"a synthetic root","reply_count":2},
		{"type":"message","ts":%q,"thread_ts":%q,"user":"U2","text":"an older synthetic reply"}]}`, root, root, old, root)
	if _, _, err := a.OlderReplies(context.Background(), ch, root, "1762790820.000100", 100); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"type": "reaction_added", "user": "U2", "reaction": "tada",
		"item": map[string]any{"type": "message", "channel": ch, "ts": old}})
	a.Inject(slack.Event{Type: "reaction_added", Raw: raw})
	if c, ok := a.MessageChange(ch, old); !ok || c.Msg == nil || len(c.Msg.Reactions) != 1 {
		t.Fatalf("a reaction to an older reply the card loaded: %+v (%v)", c, ok)
	}
}

// The update after removesNext is the one message_changed sends: a
// reactions update emitted in between (the viewer's own reaction, from
// another goroutine) doesn't take its removal.
func TestRemovesNotTakenByReactions(t *testing.T) {
	a := New(slack.New(slack.Creds{Token: "xoxc-test"}), store.New(""))
	const ch, ts = "C1", "1762790900.000100"
	a.removesNext(ch, ts)
	a.emit(Event{Type: "update", Name: UpdateReactions, Channel: ch, TS: ts, Msg: &Message{TS: ts, Reactions: []Reaction{{Name: "tada", Count: 1}}}})
	a.emit(Event{Type: "update", Channel: ch, TS: ts, Msg: &Message{TS: ts, Text: "the mention edited out"}})
	if c, _ := a.MessageChange(ch, ts); c.RemoveSeq == 0 || c.RemoveSeq != c.Seq {
		t.Fatalf("after the edit: %+v; its update removes the entries", c)
	}
}
