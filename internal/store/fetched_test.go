package store

import (
	"strings"
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
	m, ok := s.Mutate(ch, "100.000001", func(m *slack.Message) { ApplyReaction(m, "tada", "U1", true) })
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
	// a message a window holds keeps a fetched copy too: once core evicts
	// the window, the socket's changes still reach it
	s.SetLatest(ch, []slack.Message{msg("400.000001", "in the window")}, false)
	s.KeepFetched(ch, []string{"400.000001"})
	s.SetLatest(ch, []slack.Message{msg("500.000001", "the window moved on")}, false)
	if m, ok := s.FindHeld(ch, "400.000001"); !ok || m.Text != "in the window" {
		t.Fatalf("after the window let it go: %v %q", ok, m.Text)
	}
	if _, ok := s.Mutate(ch, "400.000001", func(m *slack.Message) { ApplyReaction(m, "eyes", "U2", true) }); !ok {
		t.Error("a reaction no longer reaches the message the window held")
	}
	// a socket edit while a fetch of a held message was out keeps the
	// copy the window has now (the edit and what came after it)
	s.SetLatest(ch, []slack.Message{msg("600.000001", "held")}, false)
	since = s.LiveSeq()
	held := msg("600.000001", "held, edited")
	s.NoteLive(ch, held.TS, &held)
	s.Mutate(ch, held.TS, func(m *slack.Message) { *m = held; ApplyReaction(m, "tada", "U3", true) })
	got = s.PutFetched(ch, []slack.Message{msg("600.000001", "messages.list's older copy")}, since)
	if len(got) != 1 || got[0].Text != "held, edited" || len(got[0].Reactions) != 1 {
		t.Errorf("PutFetched of a held message the socket changed answered %+v", got)
	}
}

// A reaction the socket brought while messages.list was out reaches the
// answer (reacts.go), whether or not the store held the message, and one
// the answer already counts isn't counted twice.
func TestFetchedMessagesKeepReactions(t *testing.T) {
	s := New("")
	const ch, ts = "C0LIVE0003", "700.000001"
	poll := func(users ...string) slack.Message {
		m := slack.Message{Type: "message", TS: ts, Text: "a synthetic poll"}
		if len(users) > 0 {
			m.Reactions = []slack.Reaction{{Name: "+1", Count: len(users), Users: users}}
		}
		return m
	}
	react := func(user string, add bool) {
		s.Mutate(ch, ts, func(m *slack.Message) { ApplyReaction(m, "+1", user, add) })
		s.NoteReaction(ch, ts, "+1", user, add)
	}
	plusOnes := func(m slack.Message) string {
		for _, r := range m.Reactions {
			if r.Name == "+1" {
				return strings.Join(r.Users, ",")
			}
		}
		return ""
	}
	// the first fetch: nothing held for the reaction to change
	since := s.LiveSeq()
	react("U2", true)
	if got := s.PutFetched(ch, []slack.Message{poll("U1")}, since); plusOnes(got[0]) != "U1,U2" {
		t.Errorf("first fetch: +1 by %q, want U1,U2", plusOnes(got[0]))
	}
	// held now: U1 takes theirs back while the next fetch is out
	since = s.LiveSeq()
	react("U1", false)
	if got := s.PutFetched(ch, []slack.Message{poll("U1", "U2")}, since); plusOnes(got[0]) != "U2" {
		t.Errorf("second fetch: +1 by %q, want U2", plusOnes(got[0]))
	}
	// the answer already has the reaction made meanwhile
	since = s.LiveSeq()
	react("U3", true)
	if got := s.PutFetched(ch, []slack.Message{poll("U2", "U3")}, since); plusOnes(got[0]) != "U2,U3" || got[0].Reactions[0].Count != 2 {
		t.Errorf("third fetch: +1 by %q x%d, want U2,U3 x2", plusOnes(got[0]), got[0].Reactions[0].Count)
	}
	// a reaction from before the request is the answer's to tell
	react("U4", true)
	since = s.LiveSeq()
	if got := s.PutFetched(ch, []slack.Message{poll("U2", "U3")}, since); plusOnes(got[0]) != "U2,U3" {
		t.Errorf("a fetch after U4's reaction: +1 by %q, want the answer's U2,U3", plusOnes(got[0]))
	}
}

// The fetched copies are bounded by count and by size, and the one used
// longest ago goes first: a message put again (another load drawing it)
// or kept (KeepFetched) moves to the back.
func TestFetchedMessagesBounded(t *testing.T) {
	s := New("")
	const ch = "C0LIVE0002"
	var msgs []slack.Message
	for i := 0; i < maxFetched+10; i++ {
		msgs = append(msgs, slack.Message{Type: "message", TS: "1700000000." + pad6(i)})
	}
	s.PutFetched(ch, msgs[:1], s.LiveSeq())
	s.PutFetched(ch, msgs[1:maxFetched], s.LiveSeq())
	s.PutFetched(ch, msgs[:1], s.LiveSeq()) // drawn again
	s.PutFetched(ch, msgs[maxFetched:], s.LiveSeq())
	if _, ok := s.FindHeld(ch, msgs[0].TS); !ok {
		t.Error("the message put again was evicted")
	}
	for _, i := range []int{1, 10} {
		if _, ok := s.FindHeld(ch, msgs[i].TS); ok {
			t.Errorf("message %d outlived the bound", i)
		}
	}
	if _, ok := s.FindHeld(ch, msgs[len(msgs)-1].TS); !ok {
		t.Error("the newest fetched message is gone")
	}
	s.mu.RLock()
	n, q := len(s.fetched), len(s.fetchedQ)
	s.mu.RUnlock()
	if n != maxFetched || q > 2*n+64 {
		t.Errorf("%d kept, %d queued; want %d, a queue at most twice that", n, q, maxFetched)
	}
	// by size: big messages push the oldest out
	s2 := New("")
	big := strings.Repeat("x", 512<<10)
	for i := 0; i < 12; i++ {
		s2.PutFetched(ch, []slack.Message{{Type: "message", TS: "1700000100." + pad6(i), Text: big}}, s2.LiveSeq())
	}
	s2.mu.RLock()
	size, n2 := s2.fetchedSize, len(s2.fetched)
	s2.mu.RUnlock()
	if size > maxFetchedSize || n2 >= 12 {
		t.Errorf("%d bytes in %d messages; want at most %d bytes", size, n2, maxFetchedSize)
	}
	if _, ok := s2.FindHeld(ch, "1700000100."+pad6(11)); !ok {
		t.Error("the newest big message is gone")
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

// An answer whose reaction lists fewer people than it counts (a big
// reaction: Slack lists some of them) may already count a reaction made
// while it was out: the replay leaves the count alone for people the
// answer doesn't list, as Slack's reducer only changes the people a
// reaction lists.
func TestFetchedReplayTruncatedUsers(t *testing.T) {
	s := New("")
	const ch, ts = "C0LIVE0004", "710.000001"
	users := make([]string, 50)
	for i := range users {
		users[i] = "U" + pad6(i)
	}
	answer := slack.Message{Type: "message", TS: ts, Text: "a synthetic poll", Reactions: []slack.Reaction{{Name: "+1", Count: 60, Users: users}}}
	since := s.LiveSeq()
	s.NoteReaction(ch, ts, "+1", "U9NOTLISTED", true) // counted by the answer, or not: it doesn't say
	s.NoteReaction(ch, ts, "+1", "U9GONE", false)     // the same for a removal
	got := s.PutFetched(ch, []slack.Message{answer}, since)
	if n := got[0].Reactions[0].Count; n != 60 {
		t.Errorf("+1 x%d after the replay, want the answer's 60", n)
	}
	// a person it lists takes theirs back
	since = s.LiveSeq()
	s.NoteReaction(ch, ts, "+1", users[0], false)
	got = s.PutFetched(ch, []slack.Message{answer}, since)
	if n := got[0].Reactions[0].Count; n != 59 {
		t.Errorf("+1 x%d after a listed person's removal, want 59", n)
	}
}

// Slack's reducer drops a reaction once nobody it lists is left, whatever
// its count says (6Dm2: !users.length || count < 1).
func TestApplyReactionLastListed(t *testing.T) {
	m := slack.Message{TS: "1.000001", Reactions: []slack.Reaction{{Name: "eyes", Count: 3, Users: []string{"U1"}}}}
	if !ApplyReaction(&m, "eyes", "U1", false) || len(m.Reactions) != 0 {
		t.Fatalf("after the only listed person's removal: %+v", m.Reactions)
	}
}

// A copy the socket grows in place (an agent's answer streaming in,
// reactions) counts at its new size against the 4 MB bound.
func TestFetchedSizeAfterMutate(t *testing.T) {
	s := New("")
	const ch, ts = "C0LIVE0005", "720.000001"
	s.PutFetched(ch, []slack.Message{{Type: "message", TS: ts, Text: "working..."}}, s.LiveSeq())
	long := strings.Repeat("a synthetic answer ", 10000)
	s.Mutate(ch, ts, func(m *slack.Message) { m.Text = long })
	s.mu.RLock()
	size, want := s.fetchedSize, messageSize(s.fetched[ch+"/"+ts])
	s.mu.RUnlock()
	if size != want {
		t.Errorf("fetched size %d after the copy grew, want %d", size, want)
	}
}
