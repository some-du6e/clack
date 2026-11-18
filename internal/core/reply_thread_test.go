package core

import (
	"context"
	"strings"
	"testing"

	"github.com/some-du6e/clack/internal/slack"
)

// ReplyThread is jumpToMessage's lookup before it routes a message that
// came without a thread_ts: the store first (a cached thread, a
// broadcast in the timeline), else messages.list; only a reply has a
// thread to open. Synthetic messages on the fake API.
func TestReplyThread(t *testing.T) {
	a, f := testApp(t)
	ctx := context.Background()
	const root, reply, broadcast, plain = "1700000000.000100", "1700000060.000200", "1700000090.000300", "1700000120.000400"
	a.S.SetThread("C1", root, []slack.Message{
		{TS: root, ThreadTS: root, User: "U1", ReplyCount: 1},
		{TS: reply, ThreadTS: root, User: "U2"},
	})
	a.S.SetLatest("C1", []slack.Message{
		{TS: root, ThreadTS: root, User: "U1", ReplyCount: 2},
		{TS: broadcast, ThreadTS: root, User: "U2", Subtype: "thread_broadcast"},
		{TS: plain, User: "U1"},
	}, false)

	// cached: no request
	for _, c := range []struct {
		ts, want string
	}{{reply, root}, {broadcast, root}, {root, ""}, {plain, ""}} {
		got, found := a.CachedReplyThread("C1", c.ts, false)
		if !found || got != c.want {
			t.Errorf("CachedReplyThread(%s) = %q %v, want %q (a reply's thread, else none)", c.ts, got, found, c.want)
		}
		if got, found := a.ReplyThread(ctx, "C1", c.ts, false); !found || got != c.want {
			t.Errorf("ReplyThread(%s) = %q %v, want %q", c.ts, got, found, c.want)
		}
	}
	if f.last("messages.list") != nil {
		t.Errorf("a cached message was looked up with messages.list")
	}

	// not cached: messages.list (jumpToMessage:resolveThreadTs)
	f.reply["messages.list"] = `{"ok":true,"messages":{"C1":[
		{"ts":"1700000200.000500","thread_ts":"1700000180.000100","user":"U1"},
		{"ts":"1700000300.000600","user":"U1"}]}}`
	if got, found := a.ReplyThread(ctx, "C1", "1700000200.000500", false); !found || got != "1700000180.000100" {
		t.Errorf("an uncached reply: %q %v, want its thread from messages.list", got, found)
	}
	if ids := formVal(f.last("messages.list"), "message_ids"); !strings.Contains(ids, "1700000200.000500") || !strings.Contains(ids, `"C1"`) {
		t.Errorf("messages.list asked for %s, want C1's message", ids)
	}
	if got, found := a.ReplyThread(ctx, "C1", "1700000300.000600", false); !found || got != "" {
		t.Errorf("an uncached top-level message: %q %v, want no thread", got, found)
	}
	// gone, or the request failed: not found (Slack navigates without a thread)
	f.reply["messages.list"] = `{"ok":true,"messages":{}}`
	if got, found := a.ReplyThread(ctx, "C1", "1700000400.000700", false); found || got != "" {
		t.Errorf("a message messages.list doesn't have: %q %v, want not found", got, found)
	}
	f.reply["messages.list"] = `{"ok":false,"error":"channel_not_found"}`
	if _, found := a.ReplyThread(ctx, "C1", "1700000500.000800", false); found {
		t.Errorf("a failed messages.list found the message")
	}
	if _, found := a.ReplyThread(ctx, "", reply, false); found {
		t.Errorf("no channel: found")
	}
}
