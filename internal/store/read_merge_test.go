package store

import (
	"fmt"
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
		if reflect.ValueOf(s).MethodByName("SetLatestSince").IsValid() {
			t.Fatal("msg-latency's SetLatestSince is here and RefreshRange isn't: renamed in the merge? Bring this test up to date")
		}
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

// A newest-page read whose request went out before a message_changed
// leaves every copy on the socket's revision, the copy fetched by id (an
// Activity row's) included, and announces nothing older. A read that noted
// its raw page, not what it kept, spread the older revision over the
// fetched copy while the window kept the socket's: every view drew the
// socket's until core let the window go, then the older one. Runs on
// SetLatestRead, and on msg-latency's SetLatestSince once merged.
func TestWindowReadKeepsFetchedRevision(t *testing.T) {
	const ch, ts = "C1", "1762791600.000100"
	vote := func(n int) slack.Message {
		return slack.Message{Type: "message", TS: ts, User: "U1", Text: fmt.Sprintf("votes: %d", n)}
	}
	for _, tc := range []struct {
		name string
		read func(s *Store, page []slack.Message, since uint64) bool
	}{
		{"SetLatestRead", func(s *Store, page []slack.Message, since uint64) bool {
			s.SetLatestRead(ch, page, false, since)
			return true
		}},
		{"SetLatestSince", func(s *Store, page []slack.Message, since uint64) bool {
			fn := reflect.ValueOf(s).MethodByName("SetLatestSince")
			if !fn.IsValid() {
				return false
			}
			opts := reflect.New(fn.Type().In(3)).Elem()
			opts.FieldByName("Since").SetUint(since)
			fn.Call([]reflect.Value{reflect.ValueOf(ch), reflect.ValueOf(page), reflect.ValueOf(false), opts})
			return true
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := New("")
			s.PutFetched(ch, []slack.Message{vote(1)}, s.LiveSeq()) // Activity's copy
			s.TakeReadRevisions()
			since := s.LiveSeq()
			v2 := vote(2)
			s.NoteMutate(ch, ts, &v2, func(x *slack.Message) { *x = v2 }) // the socket's message_changed
			if !tc.read(s, []slack.Message{vote(1)}, since) {
				t.Skip("lands with msg-latency")
			}
			s.mu.RLock()
			f := s.fetched[ch+"/"+ts]
			s.mu.RUnlock()
			if f == nil || f.Text != "votes: 2" {
				t.Errorf("the fetched copy after the read: %+v, want the socket's votes: 2", f)
			}
			for _, r := range s.TakeReadRevisions() {
				if r.Msg.TS == ts && r.Msg.Text != "votes: 2" {
					t.Errorf("the read announces %q, older than the socket's", r.Msg.Text)
				}
			}
		})
	}
}
