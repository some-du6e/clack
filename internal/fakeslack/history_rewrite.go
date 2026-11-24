package fakeslack

import (
	"fmt"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// History rewritten on the server's side, as a bulk delete, a retention
// pass or an import does it: the messages change with no message event
// (DeleteQuiet, EditQuiet), and Slack tells the clients with a
// channel_history_changed (group_, im_, mpim_: HistoryChangedEvent) and
// with client.counts' mutation timestamps, history_invalid and updated
// (SetMutationTimestamps). Synthetic data only.

// mutState is the mutation timestamps client.counts answers with.
type mutState struct {
	mu sync.Mutex
	by map[string][2]string // conversation → history_invalid, updated
}

var mutStates = newPerServer() // *Server -> *mutState

func (s *Server) mutState() *mutState {
	v, _ := mutStates.LoadOrStore(s, &mutState{by: map[string][2]string{}})
	return v.(*mutState)
}

// SetMutationTimestamps makes client.counts' entry for ch carry
// history_invalid and updated ("" for neither).
func (s *Server) SetMutationTimestamps(ch, historyInvalid, updated string) {
	m := s.mutState()
	m.mu.Lock()
	defer m.mu.Unlock()
	if historyInvalid == "" && updated == "" {
		delete(m.by, ch)
		return
	}
	m.by[ch] = [2]string{historyInvalid, updated}
}

// countWithMutations is client.counts' entry e with its conversation's
// mutation timestamps (SetMutationTimestamps): history_invalid next to
// the entry's fields, updated in its own.
func (s *Server) countWithMutations(e slack.CountEntry) any {
	m := s.mutState()
	m.mu.Lock()
	t, ok := m.by[e.ID]
	m.mu.Unlock()
	if !ok {
		return e
	}
	e.Updated = t[1]
	return struct {
		slack.CountEntry
		HistoryInvalid string `json:"history_invalid"`
	}{e, t[0]}
}

// DeleteQuiet deletes ch's messages at tss from its history (and their
// replies with them), unpushed. It reports how many it found.
func (s *Server) DeleteQuiet(ch string, tss ...string) int {
	gone := map[string]bool{}
	for _, ts := range tss {
		gone[ts] = true
	}
	s.histMu.Lock()
	defer s.histMu.Unlock()
	h := s.histLocked(ch)
	keep := make([]slack.Message, 0, len(h))
	n := 0
	for _, m := range h {
		if gone[m.TS] {
			n++
			delete(s.quietReplies, ch+"/"+m.TS)
			continue
		}
		keep = append(keep, m)
	}
	s.hist[ch] = keep
	return n
}

// EditQuiet changes the text of ch's message at ts, unpushed. It reports
// whether it found the message.
func (s *Server) EditQuiet(ch, ts, text string) bool {
	s.histMu.Lock()
	defer s.histMu.Unlock()
	h := s.histLocked(ch)
	for i := range h {
		if h[i].TS == ts {
			h[i].Text = text
			h[i].Blocks = nil
			h[i].Edited = &slack.Edited{User: h[i].User, TS: nowTS()}
			return true
		}
	}
	return false
}

// HistoryChangedEvent is the event Slack sends when ch's history before
// latest was rewritten: typ is channel_history_changed, group_, im_ or
// mpim_ (or, with message true, the message subtype of that name).
func HistoryChangedEvent(typ, ch, latest string, message bool) map[string]any {
	ts := nowTS()
	ev := map[string]any{"type": typ, "channel": ch, "latest": latest, "ts": ts, "event_ts": ts}
	if message {
		ev["type"], ev["subtype"] = "message", typ
	}
	return ev
}

// nowTS is a Slack ts for now.
func nowTS() string {
	n := time.Now()
	return fmt.Sprintf("%d.%06d", n.Unix(), n.Nanosecond()/1000)
}
