package store

import (
	"testing"

	"github.com/some-du6e/clack/internal/slack"
)

// Every store path that adds the server's messages raises the
// notifications' maxTs, as each of Slack's "Add messages" batches does:
// the newest page's read (SetLatestRead → SetLatestReadAt), a stale end's
// catch-up (CatchUp), a message_changed copy put in the window's range
// (UpsertInRange) and a thread's replies read (SetThreadSince). Without
// them a followed reply that came through one of them alone was dropped
// as 19.
func TestNotificationsMaxTSEveryAddPath(t *testing.T) {
	s := New("")
	step := func(what, want string) {
		t.Helper()
		if got := s.NotificationsMaxTS(); got != want {
			t.Errorf("after %s maxTs is %q, want %q", what, got, want)
		}
	}
	s.SetLatestRead("C1", []slack.Message{{TS: "1762520000.000100"}, {TS: "1762520001.000100"}}, true, s.LiveSeq())
	step("the newest page's read", "1762520001.000100")
	s.CatchUp("C1", "1762520001.000100", s.EndGen(), []slack.Message{{TS: "1762520002.000100"}}, false, s.LiveSeq())
	step("a catch-up from a stale end", "1762520002.000100")
	s.UpsertInRange("C1", slack.Message{TS: "1762520003.000100", Text: "an edit's copy"})
	step("a message_changed copy in the window's range", "1762520003.000100")
	s.SetThreadSince("C1", "1762520000.000100", []slack.Message{{TS: "1762520000.000100"}, {TS: "1762520004.000100", ThreadTS: "1762520000.000100"}}, s.LiveSeq())
	step("a thread's replies read", "1762520004.000100")
}
