package store

import (
	"testing"

	"github.com/some-du6e/clack/internal/slack"
)

// The notifications store's maxTs as Slack's client keeps it: "0" at
// first, then the newest ts of every message batch any path adds (a
// history page, a thread, a reply, messages fetched by id, a socket
// message), never lowered by an older batch, and kept across launches
// (persistenceEnabled). shouldShowNotificationForDesktopNotification
// drops a followed thread's reply newer than it (19).
func TestNotificationsMaxTSLikeSlack(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	if got := s.NotificationsMaxTS(); got != "0" {
		t.Fatalf("a new store's maxTs is %q, want Slack's \"0\"", got)
	}
	step := func(what, want string) {
		t.Helper()
		if got := s.NotificationsMaxTS(); got != want {
			t.Errorf("after %s maxTs is %q, want %q", what, got, want)
		}
	}
	s.SetLatest("C1", []slack.Message{{TS: "1762520000.000300"}, {TS: "1762520000.000100"}}, true)
	step("a history page", "1762520000.000300")
	s.PrependOlderSince("C1", []slack.Message{{TS: "1762510000.000100"}}, false, s.Seq())
	step("an older page", "1762520000.000300")
	s.SetThread("C1", "1762520000.000100", []slack.Message{{TS: "1762520000.000100"}, {TS: "1762520001.000100", ThreadTS: "1762520000.000100"}})
	step("a thread", "1762520001.000100")
	s.AddReply("C1", slack.Message{TS: "1762520002.000100", ThreadTS: "1762520000.000100"})
	step("a reply", "1762520002.000100")
	s.PutFetched("C2", []slack.Message{{TS: "1762520003.000100"}}, s.Seq())
	step("a message fetched by id", "1762520003.000100")
	s.NoteAdded("1762520004.000100")
	step("a socket message", "1762520004.000100")
	s.NoteAdded("1762500000.000100")
	step("an older socket message", "1762520004.000100")

	if err := s.SaveCache(); err != nil {
		t.Fatal(err)
	}
	s2 := New(dir)
	if got := s2.NotificationsMaxTS(); got != "0" {
		t.Fatalf("maxTs before LoadCache is %q, want \"0\"", got)
	}
	s2.LoadCache()
	if got := s2.NotificationsMaxTS(); got != "1762520004.000100" {
		t.Errorf("the next launch's maxTs is %q, want the saved 1762520004.000100", got)
	}
	// a message the session took before the cache loaded stays
	s3 := New(dir)
	s3.NoteAdded("1762530000.000100")
	s3.LoadCache()
	if got := s3.NotificationsMaxTS(); got != "1762530000.000100" {
		t.Errorf("maxTs after a load older than the session's is %q, want the session's 1762530000.000100", got)
	}
}

// An update_thread_state takes the threads' state at once, and neither
// the boot data read from disk after it nor a client.counts answer
// requested before it puts back the older state they carry; an answer
// requested after it does. An event with no state changes nothing.
func TestThreadStateNewerThanCountsAnswer(t *testing.T) {
	s := New("")
	yes, none := true, 0
	since := s.Seq() // a client.counts request goes out
	if !s.SetThreadState(&yes, &none, map[string]int{"C1": 1}) {
		t.Fatal("SetThreadState reported no change for new state")
	}
	s.InstallBootData(&BootData{}) // last session's: no unread threads
	if !s.ThreadsUnread || s.ThreadsUnreadByChannel["C1"] != 1 {
		t.Errorf("boot data installed after the event put back its state: unread %v, by channel %v", s.ThreadsUnread, s.ThreadsUnreadByChannel)
	}
	if s.SetThreadState(&yes, &none, map[string]int{"C1": 1}) {
		t.Error("SetThreadState reported a change for the same state")
	}
	var old slack.Counts // the answer: as the server was before the reply
	s.SetCountsSince(&old, since)
	if !s.ThreadsUnread || s.ThreadsUnreadByChannel["C1"] != 1 {
		t.Errorf("an answer requested before the event put back its state: unread %v, by channel %v", s.ThreadsUnread, s.ThreadsUnreadByChannel)
	}
	later := s.Seq() // requested after the event: newer than it
	var read slack.Counts
	s.SetCountsSince(&read, later)
	if s.ThreadsUnread || s.ThreadsUnreadByChannel["C1"] != 0 {
		t.Errorf("an answer requested after the event didn't install: unread %v, by channel %v", s.ThreadsUnread, s.ThreadsUnreadByChannel)
	}
	before := s.Seq()
	if s.SetThreadState(nil, nil, nil) || s.Seq() != before {
		t.Error("an update_thread_state with no state changed the store or its journal")
	}
}
