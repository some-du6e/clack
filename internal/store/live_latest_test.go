package store

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

func msgAt(ts, text string) slack.Message { return slack.Message{Type: "message", TS: ts, Text: text} }

func timelineText(s *Store, ch string) string {
	var got []string
	for _, m := range s.Timeline(ch).Msgs {
		got = append(got, m.TS+" "+m.Text)
	}
	return strings.Join(got, "|")
}

// A newest-page fetch merges into the window as Slack's history pages
// merge into its store: what the socket brought or changed while the
// request was out stays (SetLatestSince), and so does the window's
// scrollback the page reaches.
func TestSetLatestSinceKeepsLiveMessages(t *testing.T) {
	s := New("")
	const ch = "C0LIVE0001"
	s.SetLatest(ch, []slack.Message{msgAt("100.000001", "old"), msgAt("200.000001", "a"), msgAt("300.000001", "b")}, true)
	// a pending send in the window, newer than any page will be
	pend := slack.Message{Type: "message", TS: "900.000001", Text: "pending", ClientMsgID: "c1"}
	s.Upsert(ch, pend)
	since := s.LiveSeq()
	// while the request is out: a new message, an edit of one in the
	// page, a deletion, an edit of a message older than the page, a reply
	newMsg := slack.Message{Type: "message", TS: "500.000001", Text: "came meanwhile", BotID: "B0OWL"}
	s.Upsert(ch, newMsg)
	s.NoteLive(ch, newMsg.TS, &newMsg)
	edited := msgAt("300.000001", "b, edited")
	s.Mutate(ch, edited.TS, func(m *slack.Message) { *m = edited }) // core's message_changed
	s.NoteLive(ch, edited.TS, &edited)
	s.NoteLive(ch, "250.000001", nil)
	oldEdit := msgAt("050.000001", "an old message, edited")
	s.NoteLive(ch, oldEdit.TS, &oldEdit)
	reply := slack.Message{Type: "message", TS: "450.000001", ThreadTS: "200.000001", Text: "a reply"}
	s.NoteLive(ch, reply.TS, &reply)
	// the page, read before all of that
	o := LatestOpts{Since: since, Pending: []slack.Message{pend}}
	s.SetLatestSince(ch, []slack.Message{msgAt("200.000001", "a"), msgAt("250.000001", "deleted soon"), msgAt("300.000001", "b")}, true, o)
	want := "100.000001 old|200.000001 a|300.000001 b, edited|500.000001 came meanwhile|900.000001 pending"
	if g := timelineText(s, ch); g != want {
		t.Errorf("the window is %s,\nwant %s", g, want)
	}
	// a page read after them changes nothing of it
	o.Since = s.LiveSeq()
	s.SetLatestSince(ch, []slack.Message{msgAt("200.000001", "a"), edited, newMsg}, true, o)
	if g := timelineText(s, ch); g != want {
		t.Errorf("after a later page the window is %s,\nwant %s", g, want)
	}
}

// What an earlier page put in the window that a page read later doesn't
// show is gone from Slack, unless the page can't know it: a send Slack
// hasn't confirmed (or one that failed), a message the socket brought a
// moment ago (a replica behind it), an ephemeral message. With realtime
// off no event says a message was deleted, and the next page is all there
// is: the window used to keep a deleted newest message for good. A
// confirmed own send has a live record, as core gives it one: past
// heardFor the page wins over it too.
func TestSetLatestSincePageWins(t *testing.T) {
	now := time.Now()
	liveClock = func() time.Time { return now }
	t.Cleanup(func() { liveClock = time.Now })
	s := New("")
	const ch = "C0LIVE0002"
	s.SetLatest(ch, []slack.Message{msgAt("100.000001", "a"), msgAt("200.000001", "b"), msgAt("300.000001", "deleted on Slack")}, true)
	s.Upsert(ch, slack.Message{Type: "message", TS: "250.000001", Text: "only visible to you", IsEphemeral: true, User: "USLACKBOT"})
	failed := slack.Message{Type: "message", TS: "400.000001", Text: "failed send", ClientMsgID: "c1"}
	s.Upsert(ch, failed)
	long := slack.Message{Type: "message", TS: "500.000001", Text: "confirmed long ago, then deleted", ClientMsgID: "c2"}
	s.Upsert(ch, long)
	s.NoteLive(ch, long.TS, &long) // core's confirmPending
	now = now.Add(2 * heardFor)
	heard := msgAt("600.000001", "from the socket")
	s.Upsert(ch, heard)
	s.NoteLive(ch, heard.TS, &heard)
	s.SetLatestSince(ch, []slack.Message{msgAt("100.000001", "a"), msgAt("200.000001", "b")}, true,
		LatestOpts{Since: s.LiveSeq(), Pending: []slack.Message{failed}})
	want := "100.000001 a|200.000001 b|250.000001 only visible to you|400.000001 failed send|600.000001 from the socket"
	if g := timelineText(s, ch); g != want {
		t.Errorf("the window is %s,\nwant %s", g, want)
	}
}

// A page that reaches what the window had from Slack joins it (Slack's
// union of the slices it touches: the scrollback the user reads stays,
// with the window's HasMore); one that doesn't (a gap of more than a page)
// starts the window over at the page, a slice of its own, under a new
// Epoch the lists take whole. What reaches the window is the window's own
// gap boundary (Timeline.Contig): the page read, and what the socket
// brought after it with no slow connect since; a message the socket
// brought after a slow connect is no proof the page reaches the window.
func TestSetLatestSinceJoinsOrStartsOver(t *testing.T) {
	s := New("")
	const ch = "C0LIVE0003"
	s.SetLatest(ch, []slack.Message{msgAt("100.000001", "a"), msgAt("200.000001", "b"), msgAt("300.000001", "c")}, true)
	e0 := s.Timeline(ch).Epoch
	// a page from c on: joined
	if restarted := s.SetLatestSince(ch, []slack.Message{msgAt("300.000001", "c"), msgAt("400.000001", "d")}, false, LatestOpts{Since: s.LiveSeq(), Gen: s.GapGen()}); restarted {
		t.Error("a page that reaches the window started it over")
	}
	tl := s.Timeline(ch)
	if g := timelineText(s, ch); g != "100.000001 a|200.000001 b|300.000001 c|400.000001 d" || !tl.HasMore || tl.Epoch != e0 || tl.Contig != "400.000001" {
		t.Errorf("joined: %s, HasMore %v, epoch %d (was %d), boundary %s", g, tl.HasMore, tl.Epoch, e0, tl.Contig)
	}
	// a slow connect, then the socket brings f; the page starts at e, past
	// the gap: started over, the scrollback and HasMore the page's
	s.NoteSlowConnect()
	f := msgAt("600.000001", "f")
	s.Upsert(ch, f)
	s.NoteLive(ch, f.TS, &f)
	s.NoteHeard(ch, f.TS)
	if got := s.Timeline(ch).Contig; got != "400.000001" {
		t.Errorf("a message after a slow connect moved the boundary to %s", got)
	}
	pend := slack.Message{Type: "message", TS: "350.000001", Text: "failed while away", ClientMsgID: "c9"}
	s.Upsert(ch, pend)
	if restarted := s.SetLatestSince(ch, []slack.Message{msgAt("500.000001", "e"), f}, true,
		LatestOpts{Since: s.LiveSeq(), Gen: s.GapGen(), Pending: []slack.Message{pend}}); !restarted {
		t.Error("a page past a gap joined the window")
	}
	tl = s.Timeline(ch)
	if g := timelineText(s, ch); g != "350.000001 failed while away|500.000001 e|600.000001 f" || !tl.HasMore || tl.Epoch == e0 {
		t.Errorf("started over: %s, HasMore %v, epoch %d (was %d)", g, tl.HasMore, tl.Epoch, e0)
	}
}

// The boundary goes on with the socket's messages while no slow connect
// came (sixty after a wake, then a fast reconnect: the next page joins), and
// stops at a slow connect however many changes the socket brought since;
// past it, PastGap has the socket's messages after the gap.
func TestGapBoundaryIsTheWindows(t *testing.T) {
	s := New("")
	const ch = "C0LIVE0007"
	s.SetLatest(ch, []slack.Message{msgAt("100.000001", "a"), msgAt("200.000001", "b")}, true)
	for i := 0; i < 60; i++ {
		m := msgAt(fmt.Sprintf("3%02d.000001", i), "socket")
		s.Upsert(ch, m)
		s.NoteHeard(ch, m.TS)
	}
	if got := s.Timeline(ch).Contig; got != "359.000001" {
		t.Fatalf("the boundary is %s after the socket's messages, want the newest", got)
	}
	if _, behind := s.PastGap(ch); behind {
		t.Error("a window with no slow connect since its boundary is behind a gap")
	}
	page := []slack.Message{}
	for i := 11; i < 60; i++ {
		page = append(page, msgAt(fmt.Sprintf("3%02d.000001", i), "socket"))
	}
	page = append(page, msgAt("400.000001", "missed in a fast reconnect"))
	if restarted := s.SetLatestSince(ch, page, true, LatestOpts{Since: s.LiveSeq(), Gen: s.GapGen()}); restarted {
		t.Error("a page reaching the socket's messages started the window over")
	}
	// a wake, and the socket's messages after it
	s.NoteSlowConnect()
	for _, ts := range []string{"500.000001", "501.000001"} {
		m := msgAt(ts, "after the wake")
		s.Upsert(ch, m)
		s.NoteHeard(ch, m.TS)
	}
	past, behind := s.PastGap(ch)
	if !behind || len(past) != 2 || past[0].TS != "500.000001" {
		t.Errorf("past the gap: %v, %d messages; want the two after the wake", behind, len(past))
	}
}

// An older page read for a window goes in front of it only while it is
// still that window (its Epoch) starting where the page was asked
// (before): one read before the window started over is dropped.
func TestPrependOlderAtKeepsToItsWindow(t *testing.T) {
	s := New("")
	const ch = "C0LIVE0008"
	s.SetLatest(ch, []slack.Message{msgAt("200.000001", "b"), msgAt("300.000001", "c")}, true)
	tl := s.Timeline(ch)
	older := []slack.Message{msgAt("100.000001", "a")}
	if s.PrependOlderAt(ch, older, false, tl.Epoch+1, "200.000001", nil) {
		t.Error("a page for another epoch went in")
	}
	if s.PrependOlderAt(ch, older, false, tl.Epoch, "300.000001", nil) {
		t.Error("a page asked before another message went in")
	}
	if !s.PrependOlderAt(ch, older, false, tl.Epoch, "200.000001", nil) {
		t.Fatal("the page for the window, where it starts, didn't go in")
	}
	if g := timelineText(s, ch); g != "100.000001 a|200.000001 b|300.000001 c" {
		t.Errorf("the window is %s", g)
	}
}

// The viewer's unconfirmed sends stay through every merge, whatever the
// page's range, and come back after a purge took the window (core passes
// its own copies), but a local copy goes once the page has the confirmed
// one (by client_msg_id): the message showed twice until the echo came.
func TestSetLatestSinceKeepsPendingSends(t *testing.T) {
	s := New("")
	const ch = "C0LIVE0004"
	s.SetLatest(ch, []slack.Message{msgAt("100.000001", "a")}, true)
	failed := slack.Message{Type: "message", TS: "150.000001", Text: "failed", ClientMsgID: "f1"}
	sent := slack.Message{Type: "message", TS: "160.000001", Text: "sending", ClientMsgID: "s1"}
	s.Upsert(ch, failed)
	s.Upsert(ch, sent)
	page := []slack.Message{msgAt("200.000001", "b"), msgAt("300.000001", "c"),
		{Type: "message", TS: "310.000001", Text: "sending", ClientMsgID: "s1"}} // Slack's copy of sent
	s.SetLatestSince(ch, page, true, LatestOpts{Since: s.LiveSeq(), Pending: []slack.Message{failed, sent}})
	if g := timelineText(s, ch); g != "150.000001 failed|200.000001 b|300.000001 c|310.000001 sending" {
		t.Errorf("the window is %s", g)
	}
	s.PurgeHistory(ch)
	s.SetLatestSince(ch, page[:2], true, LatestOpts{Since: s.LiveSeq(), Pending: []slack.Message{failed}})
	if g := timelineText(s, ch); g != "150.000001 failed|200.000001 b|300.000001 c" {
		t.Errorf("after a purge the window is %s", g)
	}
}

// A message the socket changed while the request was out keeps the copy
// the window has now, which carries the changes after the socket's (a
// reaction, a reply count, filled files), not the socket's frozen one.
func TestSetLatestSinceKeepsHeldCopy(t *testing.T) {
	s := New("")
	const ch = "C0LIVE0005"
	s.SetLatest(ch, []slack.Message{msgAt("100.000001", "a")}, true)
	since := s.LiveSeq()
	owl := msgAt("200.000001", "Owl's confession")
	s.Upsert(ch, owl)
	s.NoteLive(ch, owl.TS, &owl)
	s.Mutate(ch, owl.TS, func(m *slack.Message) {
		m.Reactions = []slack.Reaction{{Name: "eyes", Count: 1, Users: []string{"U1"}}}
		m.ReplyCount = 1
	})
	s.SetLatestSince(ch, []slack.Message{msgAt("100.000001", "a"), owl}, true, LatestOpts{Since: since})
	got, ok := s.Find(ch, owl.TS)
	if !ok || len(got.Reactions) != 1 || got.ReplyCount != 1 {
		t.Errorf("the message lost what changed after the socket brought it: %+v", got)
	}
	// a thread: the same for a reply
	s.SetThread(ch, owl.TS, []slack.Message{{Type: "message", TS: owl.TS, ThreadTS: owl.TS, Text: owl.Text}})
	since = s.LiveSeq()
	r := slack.Message{Type: "message", TS: "210.000001", ThreadTS: owl.TS, Text: "a reply"}
	s.AddReply(ch, r)
	s.NoteLive(ch, r.TS, &r)
	s.Mutate(ch, r.TS, func(m *slack.Message) {
		m.Reactions = []slack.Reaction{{Name: "eyes", Count: 1, Users: []string{"U1"}}}
	})
	pend := slack.Message{Type: "message", TS: "220.000001", ThreadTS: owl.TS, Text: "pending reply", ClientMsgID: "c3"}
	s.AddReply(ch, pend)
	s.SetThreadSince(ch, owl.TS, []slack.Message{{Type: "message", TS: owl.TS, ThreadTS: owl.TS, Text: owl.Text}},
		ThreadOpts{Since: since, Pending: []slack.Message{pend}})
	th, _ := s.Thread(ch, owl.TS)
	if len(th) != 3 || len(th[1].Reactions) != 1 || th[2].TS != pend.TS {
		t.Errorf("the thread is %+v; want the root, the reply with its reaction, the pending reply", th)
	}
}

// A re-read of the scrollback (core's refreshOlder after an updated the
// socket never told) replaces what the window holds in its range: an edit
// on Slack shows, a message Slack no longer has goes. The local copies,
// what the socket changed while the read was out, the window outside the
// range and anything older than the window stay as they were.
func TestRefreshRangeReplacesScrollback(t *testing.T) {
	s := New("")
	const ch = "C0LIVE0006"
	s.SetLatest(ch, []slack.Message{msgAt("100.000001", "a"), msgAt("200.000001", "b"), msgAt("300.000001", "c"), msgAt("400.000001", "d")}, true)
	if got := s.Timeline(ch).PageOldest; got != "100.000001" {
		t.Errorf("PageOldest %q, want the page's oldest", got)
	}
	s.Upsert(ch, slack.Message{Type: "message", TS: "250.000001", Text: "only visible to you", IsEphemeral: true, User: "USLACKBOT"})
	failed := slack.Message{Type: "message", TS: "260.000001", Text: "failed", ClientMsgID: "f1"}
	s.Upsert(ch, failed)
	since := s.LiveSeq()
	live := msgAt("300.000001", "c, edited live") // while the read is out
	s.Mutate(ch, live.TS, func(m *slack.Message) { *m = live })
	s.NoteLive(ch, live.TS, &live)
	o := LatestOpts{Since: since, Pending: []slack.Message{failed}}
	s.RefreshRange(ch, []slack.Message{msgAt("050.000001", "older than the window"), msgAt("200.000001", "b, edited"), msgAt("300.000001", "c")},
		"", "400.000001", o)
	want := "200.000001 b, edited|250.000001 only visible to you|260.000001 failed|300.000001 c, edited live|400.000001 d"
	if g := timelineText(s, ch); g != want {
		t.Errorf("the window is %s,\nwant %s (a, which the page lacks, is gone from Slack)", g, want)
	}
	tl := s.Timeline(ch)
	if !tl.HasMore || tl.PageOldest != "100.000001" {
		t.Errorf("HasMore %v, PageOldest %q: the refresh changed the window's ends", tl.HasMore, tl.PageOldest)
	}
}
