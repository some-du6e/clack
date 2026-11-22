package store

import (
	"testing"

	"github.com/some-du6e/clack/internal/slack"
)

// A copy only this client has (an optimistic send, a local ephemeral)
// carries the local clock's ts: the notifications' maxTs takes the
// server's alone (UpsertLocal, AddReplyLocal), and checks a ts as it is
// raised (NotificationAhead).
func TestLocalCopiesKeepMaxTS(t *testing.T) {
	s := New("")
	s.SetLatest("C1", []slack.Message{{TS: "1700000000.000100"}}, false)
	s.UpsertLocal("C1", slack.Message{TS: "1900000000.000100", Text: "a send's local copy"})
	s.AddReplyLocal("C1", slack.Message{TS: "1900000001.000100", ThreadTS: "1700000000.000100"})
	if got := s.NotificationsMaxTS(); got != "1700000000.000100" {
		t.Errorf("local copies raised maxTs to %q, want the server's 1700000000.000100", got)
	}
	s.Upsert("C1", slack.Message{TS: "1700000001.000100"})
	s.AddReply("C1", slack.Message{TS: "1700000002.000100", ThreadTS: "1700000000.000100"})
	if got := s.NotificationsMaxTS(); got != "1700000002.000100" {
		t.Errorf("the server's copies left maxTs at %q, want 1700000002.000100", got)
	}
	for ts, ahead := range map[string]bool{"1700000002.000100": false, "1700000001.999999": false, "1700000002.000101": true, "1800000000.000000": true} {
		if got := s.NotificationAhead(ts); got != ahead {
			t.Errorf("NotificationAhead(%s) = %v, want %v (maxTs 1700000002.000100)", ts, got, ahead)
		}
	}
	if !New("").NotificationAhead("1700000000.000100") {
		t.Error("a new store's maxTs (\"0\") isn't behind a real ts")
	}
}

// The Threads view's answer installs its state only when no
// update_thread_state came after the request went out.
func TestThreadsViewStateSince(t *testing.T) {
	s := New("")
	since := s.Seq() // getView goes out
	yes, one := true, 1
	s.SetThreadState(&yes, &one, map[string]int{"C1": 1})
	if s.SetThreadsStateSince(false, 0, since) || !s.ThreadsUnread || s.ThreadMentions != 1 {
		t.Errorf("an answer requested before the event put its state back: unread %v, mentions %d", s.ThreadsUnread, s.ThreadMentions)
	}
	if !s.SetThreadsStateSince(false, 0, s.Seq()) || s.ThreadsUnread {
		t.Error("an answer requested after the event didn't install")
	}
}
