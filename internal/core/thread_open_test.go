package core

import (
	"context"
	"net/url"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// formsOf is the params of every call to method, in order.
func formsOf(f *fakeSlack, method string) []url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []url.Values
	for i, c := range f.calls {
		if c == method {
			out = append(out, f.forms[i])
		}
	}
	return out
}

// TestReplyThreadOpenRoot is getThreadTsToOpen (wc0P): a reply opens its
// parent's thread; with openThreadForRootMessage (every mrkdwn and
// rich_text link, BaseMrkdwnLink) a thread's parent opens its own, except
// in a DM with Slackbot or a bot user (DUy0 G: isBotUser, is_bot or
// Slackbot); without the flag a parent opens its conversation at it.
// Synthetic messages.
func TestReplyThreadOpenRoot(t *testing.T) {
	a, _ := testApp(t)
	const root, reply = "1700000000.000100", "1700000060.000200"
	a.S.PutConv(slack.Conversation{ID: "DB", IsIM: true, User: "UB"})        // a bot user (testApp's UB)
	a.S.PutConv(slack.Conversation{ID: "DS", IsIM: true, User: "USLACKBOT"}) // Slackbot
	for _, ch := range []string{"C1", "D1", "DB", "DS"} {
		a.S.SetThread(ch, root, []slack.Message{
			{TS: root, ThreadTS: root, User: "U1", ReplyCount: 1},
			{TS: reply, ThreadTS: root, User: "U2"},
		})
	}
	for _, c := range []struct {
		ch, ts   string
		openRoot bool
		want     string
	}{
		{"C1", root, true, root},
		{"C1", root, false, ""},
		{"C1", reply, false, root},
		{"C1", reply, true, root},
		{"D1", root, true, root}, // a person's DM
		{"DB", root, true, ""},   // a bot's DM: the conversation at the parent
		{"DB", reply, true, root},
		{"DS", root, true, ""},
	} {
		if got, found := a.CachedReplyThread(c.ch, c.ts, c.openRoot); !found || got != c.want {
			t.Errorf("%s %s openRoot=%v: %q (%v), want %q", c.ch, c.ts, c.openRoot, got, found, c.want)
		}
	}
}

// TestReplyThreadSingleFlight: lookups of one message share one
// messages.list request (ensureMessages' debouncer, fetchMessagesByIds):
// clicks on the same link while the first lookup is out ask once, and
// the answer is remembered.
func TestReplyThreadSingleFlight(t *testing.T) {
	a, f := testApp(t)
	const ts, thread = "1700000200.000500", "1700000180.000100"
	f.reply["messages.list"] = `{"ok":true,"messages":{"C1":[{"ts":"` + ts + `","thread_ts":"` + thread + `","user":"U1"}]}}`
	release := make(chan struct{})
	f.hooks["messages.list"] = func(url.Values) { <-release }
	ctx := context.Background()
	got := make([]string, 3)
	var wg sync.WaitGroup
	for i := range got {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got[i], _ = a.ReplyThread(ctx, "C1", ts, false)
		}()
	}
	time.Sleep(300 * time.Millisecond) // every lookup waiting on the first request
	close(release)
	wg.Wait()
	if n := countCalls(f, "messages.list"); n != 1 {
		t.Errorf("%d messages.list requests for one message, want 1", n)
	}
	for i, g := range got {
		if g != thread {
			t.Errorf("lookup %d: %q, want the reply's thread %s", i, g, thread)
		}
	}
	if g, found := a.CachedReplyThread("C1", ts, false); !found || g != thread {
		t.Errorf("after the lookup: %q (%v), want it remembered", g, found)
	}
}

// TestHistoryAround: a conversation opened at a message it doesn't hold
// loads the window around it as Slack's message pane opened at startTs
// does (fetchHistory's requestHistory({latest, oldest})): two
// conversations.history calls, the page ending at the message and the
// page starting at it, inclusive, 28 each; the window holds each message
// once, ascending, and its ends are the pages' has_more.
func TestHistoryAround(t *testing.T) {
	a, f := testApp(t)
	const at = "1700000100.000100"
	// the fake answers both calls alike: the merge keeps each message once
	f.reply["conversations.history"] = `{"ok":true,"has_more":true,"messages":[
		{"ts":"1700000200.000100","user":"U1","text":"synthetic newer"},
		{"ts":"` + at + `","user":"U2","text":"synthetic target"},
		{"ts":"1700000050.000100","user":"U1","text":"synthetic older"}]}`
	w, err := a.HistoryAround(context.Background(), "C1", at)
	if err != nil {
		t.Fatal(err)
	}
	var calls []string
	for _, p := range formsOf(f, "conversations.history") {
		calls = append(calls, "latest="+p.Get("latest")+" oldest="+p.Get("oldest")+" inclusive="+p.Get("inclusive")+" limit="+p.Get("limit"))
	}
	slices.Sort(calls)
	want := []string{"latest= oldest=" + at + " inclusive=true limit=28", "latest=" + at + " oldest= inclusive=true limit=28"}
	if !slices.Equal(calls, want) {
		t.Errorf("conversations.history %q\nwant %q", calls, want)
	}
	var ts []string
	for _, m := range w.Messages {
		ts = append(ts, m.TS)
	}
	if !slices.Equal(ts, []string{"1700000050.000100", at, "1700000200.000100"}) || !w.HasOlder || !w.HasNewer {
		t.Errorf("window %q older %v newer %v, want three messages ascending, more on both sides", ts, w.HasOlder, w.HasNewer)
	}
}

// TestRepliesForReplyKeyedByParent: conversations.replies with a reply's
// ts answers its whole thread (Slack's ts is "a thread's parent message
// or a message in the thread"). Core keeps it under the parent's ts only
// and remembers the reply's thread, so the parent's pane the reply's
// hands on to reads it without a second request. Synthetic messages.
func TestRepliesForReplyKeyedByParent(t *testing.T) {
	a, f := testApp(t)
	const root, reply = "1700000000.000100", "1700000060.000200"
	f.reply["conversations.replies"] = `{"ok":true,"has_more":false,"messages":[
		{"ts":"` + root + `","thread_ts":"` + root + `","user":"U1","reply_count":1,"text":"synthetic root"},
		{"ts":"` + reply + `","thread_ts":"` + root + `","user":"U2","text":"synthetic reply"}]}`
	ctx := context.Background()
	msgs, err := a.Replies(ctx, "C1", reply)
	if err != nil || len(msgs) != 2 || msgs[0].TS != root {
		t.Fatalf("Replies for the reply: %d messages (%v), want its thread from the root", len(msgs), err)
	}
	if th, ok := a.S.Thread("C1", reply); ok {
		t.Errorf("%d messages kept under the reply's ts as a thread of its own", len(th))
	}
	if th, ok := a.S.Thread("C1", root); !ok || len(th) != 2 {
		t.Errorf("the parent's thread: %d messages (%v), want the answer under the parent's ts", len(th), ok)
	}
	if p, ok := a.ReplyParent("C1", reply); !ok || p != root {
		t.Errorf("ReplyParent: %q (%v), want %s", p, ok, root)
	}
	// the parent's pane the reply's hands on to starts at the reply
	// (ReplyAsThreadRoot's openThreadAtIn: threadStartAt), and draws the
	// page the reply's ts read
	if sl, err := a.ThreadAt(ctx, "C1", root, reply); err != nil || len(sl.Msgs) == 0 || sl.Root == nil || sl.Root.TS != root {
		t.Errorf("the parent's thread at the reply: %d messages (%v)", len(sl.Msgs), err)
	}
	if n := countCalls(f, "conversations.replies"); n != 1 {
		t.Errorf("%d conversations.replies requests, want 1 (the parent's pane reads the answer)", n)
	}
}

// TestLatestReply is resolveLatestThreadReplyTs (cdTA): a thread's
// newest reply from conversations.replies limit 1 (its root's
// latest_reply), else from the root core holds, else the thread itself.
func TestLatestReply(t *testing.T) {
	a, f := testApp(t)
	const root, latest = "1700000000.000100", "1700000300.000100"
	f.reply["conversations.replies"] = `{"ok":true,"messages":[{"ts":"` + root + `","thread_ts":"` + root + `","latest_reply":"` + latest + `","user":"U1"}]}`
	ctx := context.Background()
	if got := a.LatestReply(ctx, "C1", root); got != latest {
		t.Errorf("LatestReply %q, want the root's latest_reply %s", got, latest)
	}
	if p := f.last("conversations.replies"); p.Get("ts") != root || p.Get("limit") != "1" {
		t.Errorf("conversations.replies %v, want ts=%s limit=1", p, root)
	}
	f.reply["conversations.replies"] = `{"ok":false,"error":"thread_not_found"}`
	a.S.SetThread("C1", root, []slack.Message{{TS: root, ThreadTS: root, User: "U1", LatestReply: "1700000400.000100"}})
	if got := a.LatestReply(ctx, "C1", root); got != "1700000400.000100" {
		t.Errorf("LatestReply when the request fails %q, want the stored root's latest_reply", got)
	}
	if got := a.LatestReply(ctx, "C1", "1700000900.000100"); got != "1700000900.000100" {
		t.Errorf("LatestReply of an unknown thread %q, want the thread itself", got)
	}
}

// TestReplyThreadForgetsChangedMessages: what a lookup found is forgotten
// once the message changes (message_replied, message_changed), so a
// message looked up before it had replies opens its thread after (Slack's
// store keeps the message, and messageReplied updates it there).
// Synthetic messages.
func TestReplyThreadForgetsChangedMessages(t *testing.T) {
	a, f := testApp(t)
	const ts = "1700000300.000600"
	f.reply["messages.list"] = `{"ok":true,"messages":{"C1":[{"ts":"` + ts + `","user":"U1"}]}}`
	ctx := context.Background()
	if got, found := a.ReplyThread(ctx, "C1", ts, true); !found || got != "" {
		t.Fatalf("a message without replies: %q (%v), want no thread", got, found)
	}
	a.Inject(rtm("message", map[string]any{"subtype": "message_replied", "channel": "C1",
		"message": map[string]any{"ts": ts, "thread_ts": ts, "reply_count": 1, "user": "U1", "latest_reply": "1700000400.000100"}}))
	if got, found := a.CachedReplyThread("C1", ts, true); found && got == "" {
		t.Errorf("the lookup is remembered after the message got replies: no thread")
	}
	f.reply["messages.list"] = `{"ok":true,"messages":{"C1":[{"ts":"` + ts + `","thread_ts":"` + ts + `","reply_count":1,"user":"U1"}]}}`
	if got, found := a.ReplyThread(ctx, "C1", ts, true); !found || got != ts {
		t.Errorf("a parent link after it got replies: %q (%v), want its thread %s", got, found, ts)
	}
}
