package store

import (
	"testing"

	"github.com/some-du6e/clack/internal/slack"
)

// Messages fetched by id stay where the socket's changes reach them
// (fetched.go): messages.list's answer keeps what the socket changed or
// deleted while the request was out, Mutate and Remove reach the kept
// copies, FindHeld reads them and Find doesn't (a hydrate asks Slack again).
func TestFetchedMessagesTakeSocketChanges(t *testing.T) {
	s := New("")
	const ch = "C0LIVE0001"
	msg := func(ts, text string) slack.Message { return slack.Message{Type: "message", TS: ts, Text: text} }
	since := s.LiveSeq()
	// while messages.list is out: an edit of one, a deletion of another
	edited := msg("200.000001", "b, edited")
	s.NoteLive(ch, edited.TS, &edited)
	s.NoteLive(ch, "300.000001", nil)
	got := s.PutFetched(ch, []slack.Message{msg("100.000001", "a"), msg("200.000001", "b"), msg("300.000001", "c")}, since)
	if len(got) != 2 || got[0].Text != "a" || got[1].Text != "b, edited" {
		t.Fatalf("PutFetched answered %+v; want a, then the socket's edit, and no deleted c", got)
	}
	if m, ok := s.FindHeld(ch, "200.000001"); !ok || m.Text != "b, edited" {
		t.Errorf("FindHeld 200: %v %q", ok, m.Text)
	}
	if _, ok := s.FindHeld(ch, "300.000001"); ok {
		t.Error("the deleted message was kept")
	}
	if _, ok := s.Find(ch, "100.000001"); ok {
		t.Error("Find reads messages fetched by id: a hydrate would never ask Slack again")
	}
	// a reaction reaches the kept copy, as Slack's reducer reaches its store
	m, ok := s.Mutate(ch, "100.000001", func(m *slack.Message) {
		m.Reactions = append(m.Reactions, slack.Reaction{Name: "tada", Count: 1, Users: []string{"U1"}})
	})
	if !ok || len(m.Reactions) != 1 {
		t.Fatalf("Mutate of a fetched message: %v %+v", ok, m.Reactions)
	}
	if m, _ := s.FindHeld(ch, "100.000001"); len(m.Reactions) != 1 {
		t.Errorf("the kept copy has %d reactions after Mutate", len(m.Reactions))
	}
	s.Remove(ch, "100.000001")
	if _, ok := s.FindHeld(ch, "100.000001"); ok {
		t.Error("Remove left the fetched copy")
	}
	// a message a window holds isn't kept twice: the window's copy is the
	// one the socket keeps current
	s.SetLatest(ch, []slack.Message{msg("400.000001", "in the window")}, false)
	got = s.PutFetched(ch, []slack.Message{msg("400.000001", "messages.list's copy")}, s.LiveSeq())
	if len(got) != 1 || got[0].Text != "in the window" {
		t.Errorf("PutFetched of a held message answered %+v, want the window's copy", got)
	}
	s.mu.RLock()
	_, twice := s.fetched[ch+"/400.000001"]
	s.mu.RUnlock()
	if twice {
		t.Error("a held message was kept again as fetched")
	}
}

// The fetched copies are bounded: the oldest put goes first.
func TestFetchedMessagesBounded(t *testing.T) {
	s := New("")
	const ch = "C0LIVE0002"
	var msgs []slack.Message
	for i := 0; i < maxFetched+10; i++ {
		msgs = append(msgs, slack.Message{Type: "message", TS: "1700000000." + pad6(i)})
	}
	s.PutFetched(ch, msgs, s.LiveSeq())
	if _, ok := s.FindHeld(ch, msgs[0].TS); ok {
		t.Error("the oldest fetched message outlived the bound")
	}
	if _, ok := s.FindHeld(ch, msgs[len(msgs)-1].TS); !ok {
		t.Error("the newest fetched message is gone")
	}
	s.mu.RLock()
	n, q := len(s.fetched), len(s.fetchedQ)
	s.mu.RUnlock()
	if n != maxFetched || q != maxFetched {
		t.Errorf("%d kept, %d queued; want %d", n, q, maxFetched)
	}
}

func pad6(i int) string {
	b := []byte("000000")
	for j := 5; j >= 0 && i > 0; j-- {
		b[j] = byte('0' + i%10)
		i /= 10
	}
	return string(b)
}
