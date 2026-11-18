package store

import (
	"reflect"
	"testing"

	"github.com/some-du6e/clack/internal/slack"
)

// RefreshRange (msg-latency's re-read of a window's scrollback) is a read
// like the others: a reaction the socket brought while it was out stays
// on the window's copy, and a revision the store held another of reaches
// every copy and waits for core (TakeReadRevisions). Its hooks run over
// what it keeps, never the raw page: a message the socket changed while
// it was out keeps the socket's copy, and nothing older is announced.
// RefreshRange lands with msg-latency; until then there's nothing to run.
func TestRefreshRangeRead(t *testing.T) {
	s := New("")
	fn := reflect.ValueOf(s).MethodByName("RefreshRange")
	if !fn.IsValid() {
		t.Skip("store.RefreshRange lands with msg-latency")
	}
	if fn.Type().NumIn() != 5 {
		t.Fatalf("RefreshRange's signature changed (%v): bring this test up to date", fn.Type())
	}
	const ch = "C1"
	const vote, edited, newest = "1762791500.000100", "1762791510.000100", "1762791520.000100"
	plus := func(users ...string) []slack.Reaction {
		return []slack.Reaction{{Name: "+1", Count: len(users), Users: users}}
	}
	s.SetLatest(ch, []slack.Message{
		{Type: "message", TS: vote, User: "U1", Text: "votes: 1", Reactions: plus("U1")},
		{Type: "message", TS: edited, User: "U2", Text: "a synthetic draft"},
		{Type: "message", TS: newest, User: "U1", Text: "the newest synthetic message"},
	}, true)
	s.TakeReadRevisions()
	since := s.LiveSeq()
	// while the re-read is out: a reaction to the vote, an edit of the draft
	s.ApplyReactionNoted(ch, vote, "+1", "U2", true)
	final := slack.Message{Type: "message", TS: edited, User: "U2", Text: "a synthetic plan, final"}
	s.NoteLive(ch, edited, &final)
	s.Mutate(ch, edited, func(x *slack.Message) { *x = final })
	page := []slack.Message{
		{Type: "message", TS: vote, User: "U1", Text: "votes: 2", Reactions: plus("U1")},
		{Type: "message", TS: edited, User: "U2", Text: "a synthetic draft"},
	}
	opts := reflect.New(fn.Type().In(4)).Elem()
	opts.FieldByName("Since").SetUint(since)
	fn.Call([]reflect.Value{reflect.ValueOf(ch), reflect.ValueOf(page), reflect.ValueOf(""), reflect.ValueOf(newest), opts})
	w := s.Timeline(ch)
	got := map[string]slack.Message{}
	for _, m := range w.Msgs {
		got[m.TS] = m
	}
	if m := got[vote]; m.Text != "votes: 2" || len(m.Reactions) != 1 || m.Reactions[0].Count != 2 {
		t.Errorf("the re-read vote: %q %+v, want votes: 2 with both +1s", m.Text, m.Reactions)
	}
	if m := got[edited]; m.Text != final.Text {
		t.Errorf("the message edited while the re-read was out: %q, want the socket's", m.Text)
	}
	var revs []string
	for _, r := range s.TakeReadRevisions() {
		revs = append(revs, r.Msg.TS+" "+r.Msg.Text)
		if r.Msg.TS == edited && r.Msg.Text != final.Text {
			t.Errorf("the re-read announces the draft's older revision %q", r.Msg.Text)
		}
	}
	found := false
	for _, r := range revs {
		found = found || r == vote+" votes: 2"
	}
	if !found {
		t.Errorf("the re-read's revisions %q lack the vote's votes: 2", revs)
	}
}
