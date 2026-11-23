package core

import (
	"testing"

	"github.com/some-du6e/clack/internal/slack"
)

// canAddReaction: a message that isn't actionable, a file comment, a
// frozen channel, a pending Slack Connect DM and a locked thread take no
// reaction; an ordinary message, a reply in an open thread and a DM do.
func TestCanAddReactionLikeSlack(t *testing.T) {
	a, _ := testApp(t)
	const root, open = "1.000001", "2.000001"
	a.S.SetThread("C1", root, []slack.Message{{TS: root, ThreadTS: root, User: "U1", Text: "a synthetic root", ReplyCount: 1, IsLocked: true},
		{TS: "1.000002", ThreadTS: root, User: "U2", Text: "one"}})
	a.S.SetThread("C1", open, []slack.Message{{TS: open, ThreadTS: open, User: "U1", Text: "another root", ReplyCount: 1}})
	a.S.PutConv(slack.Conversation{ID: "C0FROZEN01", Name: "frozen", IsChannel: true, IsFrozen: true})
	a.S.PutConv(slack.Conversation{ID: "D0PENDING1", IsIM: true, User: "U2", IsPendingExtShared: true})
	a.S.PutConv(slack.Conversation{ID: "D0OPEN0001", IsIM: true, User: "U3"})
	for _, c := range []struct {
		what string
		m    Message
		want bool
	}{
		{"an ordinary message", Message{Channel: "C1", TS: "3.000001"}, true},
		{"a reply in an open thread", Message{Channel: "C1", TS: "2.000002", ThreadTS: open}, true},
		{"a DM", Message{Channel: "D0OPEN0001", TS: "3.000001"}, true},
		{"a reply in a locked thread", Message{Channel: "C1", TS: "1.000002", ThreadTS: root}, false},
		{"a locked thread's root", Message{Channel: "C1", TS: root, ThreadTS: root}, false},
		{"a pending send", Message{Channel: "C1", TS: "3.000001", Pending: true}, false},
		{"a failed send", Message{Channel: "C1", TS: "3.000001", Failed: true}, false},
		{"an ephemeral message", Message{Channel: "C1", TS: "3.000001", Ephemeral: true}, false},
		{"a tombstone", Message{Channel: "C1", TS: "3.000001", Subtype: "tombstone"}, false},
		{"a hidden thread", Message{Channel: "C1", TS: "3.000001", Subtype: "thread_hidden"}, false},
		{"a moderated message", Message{Channel: "C1", TS: "3.000001", Subtype: "moderated"}, false},
		{"a DLP tombstone", Message{Channel: "C1", TS: "3.000001", Subtype: "dlp_tombstone"}, false},
		{"a file comment", Message{Channel: "C1", TS: "3.000001", Subtype: "file_comment"}, false},
		{"a frozen channel's message", Message{Channel: "C0FROZEN01", TS: "3.000001"}, false},
		{"a pending Slack Connect DM's message", Message{Channel: "D0PENDING1", TS: "3.000001"}, false},
	} {
		m := c.m
		if got := a.CanAddReaction(&m); got != c.want {
			t.Errorf("%s: CanAddReaction %v, want %v", c.what, got, c.want)
		}
	}
	if a.CanAddReaction(nil) {
		t.Error("no message takes a reaction")
	}
}
