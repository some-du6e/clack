package fakeslack

import (
	"strings"
	"sync"

	"github.com/some-du6e/clack/internal/slack"
)

// What the fake's client.counts says of a conversation's history beyond
// its fixed latest, for the tests of the client's catch-ups after a gap
// (core/history_catchup.go):
//
//   - SetCountLatest makes it name a message the socket never delivered
//     (it was down) as the latest, as Slack's counts do once the client
//     reads them again after reconnecting. A message PostQuiet adds after
//     it takes over as the latest.
//   - DeleteQuiet deletes a message from the history without an event;
//     SetCountMutations sets history_invalid and updated, Slack's channel
//     mutation timestamps, which the counts carry only for conversations a
//     test gave them.
//
// HistoryReads counts the conversations.history pages the fake read for a
// conversation (a test holding an answer with Fault.ReplyDelay knows the
// page is read once it moved).

var countStates sync.Map // *Server → *countState (dropped by Close)

type countState struct {
	mu     sync.Mutex
	latest map[string]string
	muts   map[string][2]string // history_invalid, updated
	reads  map[string]int
}

func (s *Server) countState() *countState {
	v, _ := countStates.LoadOrStore(s, &countState{latest: map[string]string{}, muts: map[string][2]string{}, reads: map[string]int{}})
	return v.(*countState)
}

// SetCountLatest makes client.counts report ts as ch's latest message.
func (s *Server) SetCountLatest(ch, ts string) {
	st := s.countState()
	st.mu.Lock()
	st.latest[ch] = ts
	st.mu.Unlock()
}

// countLatest is SetCountLatest's ts for ch, if any.
func (s *Server) countLatest(ch string) (string, bool) {
	v, ok := countStates.Load(s)
	if !ok {
		return "", false
	}
	st := v.(*countState)
	st.mu.Lock()
	defer st.mu.Unlock()
	ts, ok := st.latest[ch]
	return ts, ok
}

// postedQuiet drops ch's SetCountLatest once a newer message is posted
// there: the counts name the newest.
func (s *Server) postedQuiet(ch, ts string) {
	v, ok := countStates.Load(s)
	if !ok {
		return
	}
	st := v.(*countState)
	st.mu.Lock()
	if l, ok := st.latest[ch]; ok && tsBefore(l, ts) {
		delete(st.latest, ch)
	}
	st.mu.Unlock()
}

// SetCountMutations makes client.counts report ch's history_invalid and
// updated.
func (s *Server) SetCountMutations(ch, historyInvalid, updated string) {
	st := s.countState()
	st.mu.Lock()
	st.muts[ch] = [2]string{historyInvalid, updated}
	st.mu.Unlock()
}

// countMutations puts SetCountMutations' timestamps on e.
func (s *Server) countMutations(e *slack.CountEntry) {
	v, ok := countStates.Load(s)
	if !ok {
		return
	}
	st := v.(*countState)
	st.mu.Lock()
	defer st.mu.Unlock()
	if m, ok := st.muts[e.ID]; ok {
		e.HistoryInvalid, e.Updated = m[0], m[1]
	}
}

// DeleteQuiet deletes ts from ch's history with no event, as a deletion
// the socket missed (or one no socket could tell: realtime off). The
// counts say nothing of it unless SetCountMutations moves ch's
// history_invalid.
func (s *Server) DeleteQuiet(ch, ts string) {
	s.histMu.Lock()
	defer s.histMu.Unlock()
	h := s.histLocked(ch)
	for i := range h {
		if h[i].TS == ts {
			s.hist[ch] = append(h[:i:i], h[i+1:]...)
			return
		}
	}
}

// tsBefore reports whether Slack ts a is before b.
func tsBefore(a, b string) bool {
	as, af, _ := strings.Cut(a, ".")
	bs, bf, _ := strings.Cut(b, ".")
	if len(as) != len(bs) {
		return len(as) < len(bs)
	}
	if as != bs {
		return as < bs
	}
	return af < bf
}

// noteHistoryRead counts a conversations.history page read for ch.
func (s *Server) noteHistoryRead(ch string) {
	st := s.countState()
	st.mu.Lock()
	st.reads[ch]++
	st.mu.Unlock()
}

// HistoryReads is how many conversations.history pages the fake read for
// ch (answered or held back).
func (s *Server) HistoryReads(ch string) int {
	st := s.countState()
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.reads[ch]
}
