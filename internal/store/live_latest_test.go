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
	s.NoteLive(ch, edited.TS, &edited)
	s.NoteLive(ch, "250.000001", nil)
	oldEdit := msg("050.000001", "an old message, edited")
	s.NoteLive(ch, oldEdit.TS, &oldEdit)
	reply := slack.Message{Type: "message", TS: "450.000001", ThreadTS: "200.000001", Text: "a reply"}
	s.NoteLive(ch, reply.TS, &reply)
	// the page, read before all of that
	s.SetLatestSince(ch, []slack.Message{msg("200.000001", "a"), msg("250.000001", "deleted soon"), msg("300.000001", "b")}, true, since)
	var got []string
	for _, m := range s.Timeline(ch).Msgs {
		got = append(got, m.TS+" "+m.Text)
	}
	want := "200.000001 a|300.000001 b, edited|500.000001 came meanwhile|900.000001 pending"
	if g := strings.Join(got, "|"); g != want {
		t.Errorf("the window is %s,\nwant %s", g, want)
	}
	// a page read after them changes nothing of it
	s.SetLatestSince(ch, []slack.Message{msg("200.000001", "a"), edited, newMsg}, true, s.LiveSeq())
	got = got[:0]
	for _, m := range s.Timeline(ch).Msgs {
		got = append(got, m.TS+" "+m.Text)
	}
	if g := strings.Join(got, "|"); g != want {
		t.Errorf("after a later page the window is %s,\nwant %s", g, want)
	}
}
