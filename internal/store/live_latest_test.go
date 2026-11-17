package store

import (
	"strings"
	"testing"

	"github.com/some-du6e/clack/internal/slack"
)

// A newest-page fetch merges into the window as Slack's history pages
// merge into its store: what the socket brought or changed while the
// request was out stays (SetLatestSince).
func TestSetLatestSinceKeepsLiveMessages(t *testing.T) {
	s := New("")
	const ch = "C0LIVE0001"
	msg := func(ts, text string) slack.Message { return slack.Message{Type: "message", TS: ts, Text: text} }
	s.SetLatest(ch, []slack.Message{msg("100.000001", "old"), msg("200.000001", "a"), msg("300.000001", "b")}, true)
	// a pending send in the window, newer than any page will be
	s.Upsert(ch, slack.Message{Type: "message", TS: "900.000001", Text: "pending", ClientMsgID: "c1"})
	since := s.LiveSeq()
	// while the request is out: a new message, an edit of one in the
	// page, a deletion, an edit of a message older than the page, a reply
	newMsg := slack.Message{Type: "message", TS: "500.000001", Text: "came meanwhile", BotID: "B0OWL"}
	s.Upsert(ch, newMsg)
	s.NoteLive(ch, newMsg.TS, &newMsg)
	edited := msg("300.000001", "b, edited")
	s.Mutate(ch, edited.TS, func(m *slack.Message) { *m = edited }) // core's message_changed
	s.NoteLive(ch, edited.TS, &edited)
	s.NoteLive(ch, "250.000001", nil)
	oldEdit := msg("050.000001", "an old message, edited")
	s.NoteLive(ch, oldEdit.TS, &oldEdit)
	reply := slack.Message{Type: "message", TS: "450.000001", ThreadTS: "200.000001", Text: "a reply"}
	s.NoteLive(ch, reply.TS, &reply)
	// the page, read before all of that
	pending := map[string]bool{"900.000001": true}
	s.SetLatestSince(ch, []slack.Message{msg("200.000001", "a"), msg("250.000001", "deleted soon"), msg("300.000001", "b")}, true, since, pending)
	var got []string
	for _, m := range s.Timeline(ch).Msgs {
		got = append(got, m.TS+" "+m.Text)
	}
	want := "200.000001 a|300.000001 b, edited|500.000001 came meanwhile|900.000001 pending"
	if g := strings.Join(got, "|"); g != want {
		t.Errorf("the window is %s,\nwant %s", g, want)
	}
	// a page read after them changes nothing of it
	s.SetLatestSince(ch, []slack.Message{msg("200.000001", "a"), edited, newMsg}, true, s.LiveSeq(), pending)
	got = got[:0]
	for _, m := range s.Timeline(ch).Msgs {
		got = append(got, m.TS+" "+m.Text)
	}
	if g := strings.Join(got, "|"); g != want {
		t.Errorf("after a later page the window is %s,\nwant %s", g, want)
	}
}

func timelineText(s *Store, ch string) string {
	var got []string
	for _, m := range s.Timeline(ch).Msgs {
		got = append(got, m.TS+" "+m.Text)
	}
	return strings.Join(got, "|")
}

// What an earlier page put in the window that a page read later doesn't
// show is gone from Slack, unless the page can't know it: a send Slack
// hasn't confirmed (or one that failed), a message the socket brought, an
// ephemeral message. With realtime off no event says a message was
// deleted, and the next page is all there is: the window used to keep a
// deleted newest message for good.
func TestSetLatestSincePageWins(t *testing.T) {
	s := New("")
	const ch = "C0LIVE0002"
	msg := func(ts, text string) slack.Message { return slack.Message{Type: "message", TS: ts, Text: text} }
	s.SetLatest(ch, []slack.Message{msg("100.000001", "a"), msg("200.000001", "b"), msg("300.000001", "deleted on Slack")}, true)
	s.Upsert(ch, slack.Message{Type: "message", TS: "250.000001", Text: "only visible to you", IsEphemeral: true, User: "USLACKBOT"})
	s.Upsert(ch, slack.Message{Type: "message", TS: "400.000001", Text: "failed send", ClientMsgID: "c1"})
	s.Upsert(ch, slack.Message{Type: "message", TS: "500.000001", Text: "confirmed long ago, then deleted", ClientMsgID: "c2"})
	// the socket brought one: a replica a moment behind can lack it
	heard := msg("600.000001", "from the socket")
	s.Upsert(ch, heard)
	s.NoteLive(ch, heard.TS, &heard)
	s.SetLatestSince(ch, []slack.Message{msg("100.000001", "a"), msg("200.000001", "b")}, true, s.LiveSeq(), map[string]bool{"400.000001": true})
	want := "100.000001 a|200.000001 b|250.000001 only visible to you|400.000001 failed send|600.000001 from the socket"
	if g := timelineText(s, ch); g != want {
		t.Errorf("the window is %s,\nwant %s", g, want)
	}
}

// A message the socket changed while the request was out keeps the copy
// the window has now, which carries the changes after the socket's (a
// reaction, a reply count, filled files), not the socket's frozen one.
func TestSetLatestSinceKeepsHeldCopy(t *testing.T) {
	s := New("")
	const ch = "C0LIVE0003"
	msg := func(ts, text string) slack.Message { return slack.Message{Type: "message", TS: ts, Text: text} }
	s.SetLatest(ch, []slack.Message{msg("100.000001", "a")}, true)
	since := s.LiveSeq()
	owl := msg("200.000001", "Owl's confession")
	s.Upsert(ch, owl)
	s.NoteLive(ch, owl.TS, &owl)
	s.Mutate(ch, owl.TS, func(m *slack.Message) {
		m.Reactions = []slack.Reaction{{Name: "eyes", Count: 1, Users: []string{"U1"}}}
		m.ReplyCount = 1
	})
	s.SetLatestSince(ch, []slack.Message{msg("100.000001", "a"), owl}, true, since, nil)
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
	s.SetThreadSince(ch, owl.TS, []slack.Message{{Type: "message", TS: owl.TS, ThreadTS: owl.TS, Text: owl.Text}}, since, map[string]bool{pend.TS: true})
	th, _ := s.Thread(ch, owl.TS)
	if len(th) != 3 || len(th[1].Reactions) != 1 || th[2].TS != pend.TS {
		t.Errorf("the thread is %+v; want the root, the reply with its reaction, the pending reply", th)
	}
}
