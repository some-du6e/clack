package core

import (
	"testing"

	"github.com/some-du6e/clack/internal/slack"
)

// TestCachedSlugKeepsParentThread: a message_mention pill's thread_ts is
// getMessageThreadTs (KwXc hydrateMessageSlugData): a reply's thread and
// a thread's parent's own ts, so the pill's click (p9lB MessageMention:
// jumpToMessage({channelId, ts, threadTs})) opens the thread for both; a
// message outside any thread has none. Clack left a parent's thread_ts
// out and its pill opened the conversation. Synthetic messages.
func TestCachedSlugKeepsParentThread(t *testing.T) {
	a, _ := testApp(t)
	const root, reply, plain = "1700000000.000100", "1700000060.000200", "1700000120.000300"
	a.S.SetLatest("C1", []slack.Message{
		{TS: root, ThreadTS: root, User: "U1", ReplyCount: 1},
		{TS: plain, User: "U2"},
	}, false)
	a.S.SetThread("C1", root, []slack.Message{
		{TS: root, ThreadTS: root, User: "U1", ReplyCount: 1},
		{TS: reply, ThreadTS: root, User: "U2"},
	})
	link := func(ts string) string { return "https://synthetic.slack.com/archives/C1/p" + ts[:10] + ts[11:] }
	for _, c := range []struct{ name, ts, want string }{
		{"a thread's parent", root, root},
		{"a reply", reply, root},
		{"a message outside a thread", plain, ""},
	} {
		s, ok := a.CachedSlug(link(c.ts))
		if !ok || s.ID != c.ts || s.ThreadTS != c.want {
			t.Errorf("%s: slug %+v (%v), want thread_ts %q", c.name, s, ok, c.want)
		}
	}
}
