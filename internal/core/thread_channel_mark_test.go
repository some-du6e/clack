package core

import (
	"context"
	"testing"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// A thread read past a broadcast it sent to the channel marks the
// channel too when nothing else there is unread (setThreadLastRead's
// maybeMarkChannelFromThread): the channel reads up to the broadcast. Any
// other unread message in that span, or a window that doesn't reach back
// to the channel's last read, leaves the channel as it is; the viewer's
// own messages and ignored types don't count.
func TestMarkThreadMarksChannelFromBroadcastLikeSlack(t *testing.T) {
	const root, chanRead, bcast, newest = "1700000000.000100", "1700000010.000100", "1700000020.000100", "1700000030.000100"
	msg := func(ts, user, subtype string) slack.Message {
		m := slack.Message{Type: "message", TS: ts, User: user, Subtype: subtype, Text: "synthetic"}
		if subtype == "thread_broadcast" {
			m.ThreadTS = root
		}
		return m
	}
	for _, tc := range []struct {
		name    string
		extra   []slack.Message
		hasMore bool
		first   string // the window's oldest message
		marked  bool
	}{
		{"only the broadcast unread", nil, false, root, true},
		{"another unread message", []slack.Message{msg("1700000015.000100", "U1", "")}, false, root, false},
		{"the viewer's own message", []slack.Message{msg("1700000015.000100", "USELF", "")}, false, root, true},
		{"a join", []slack.Message{msg("1700000015.000100", "U2", "channel_join")}, false, root, true},
		{"the window short of the channel's last read", nil, true, "1700000012.000100", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, f := testApp(t)
			f.reply["subscriptions.thread.mark"] = `{"ok":true}`
			f.reply["conversations.mark"] = `{"ok":true}`
			msgs := []slack.Message{msg(tc.first, "U1", "")}
			if tc.first == root {
				msgs[0].ThreadTS, msgs[0].ReplyCount = root, 2
				msgs = append(msgs, msg("1700000005.000100", "U1", ""))
			}
			msgs = append(msgs, tc.extra...)
			msgs = append(msgs, msg(bcast, "U2", "thread_broadcast"))
			a.S.SetLatest("C1", msgs, tc.hasMore)
			a.S.MarkRead("C1", chanRead)
			a.S.SetThreadSub("C1", root, func(s *store.ThreadSub) { s.Subscribed, s.LastRead = true, root })
			if err := a.MarkThread(context.Background(), "C1", root, newest); err != nil {
				t.Fatal(err)
			}
			got := f.last("conversations.mark")
			if tc.marked && (got == nil || got.Get("channel") != "C1" || got.Get("ts") != bcast) {
				t.Errorf("the channel wasn't marked up to the broadcast (conversations.mark %v)", got)
			}
			if !tc.marked && got != nil {
				t.Errorf("the channel was marked (%v) though it has to stay unread", got)
			}
		})
	}
}
