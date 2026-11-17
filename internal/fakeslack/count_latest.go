package fakeslack

import "sync"

// The fake's client.counts carries a fixed latest message per channel.
// SetCountLatest makes it report ts for ch instead, as Slack's counts name
// a message the socket never delivered (it was down) once the client reads
// them again after reconnecting.

var countLatests sync.Map // *Server → *countLatestState

type countLatestState struct {
	mu sync.Mutex
	ts map[string]string
}

func (s *Server) countLatestState() *countLatestState {
	v, _ := countLatests.LoadOrStore(s, &countLatestState{ts: map[string]string{}})
	return v.(*countLatestState)
}

// SetCountLatest makes client.counts report ts as ch's latest message.
func (s *Server) SetCountLatest(ch, ts string) {
	st := s.countLatestState()
	st.mu.Lock()
	st.ts[ch] = ts
	st.mu.Unlock()
}

// countLatest is SetCountLatest's ts for ch, if any.
func (s *Server) countLatest(ch string) (string, bool) {
	v, ok := countLatests.Load(s)
	if !ok {
		return "", false
	}
	st := v.(*countLatestState)
	st.mu.Lock()
	defer st.mu.Unlock()
	ts, ok := st.ts[ch]
	return ts, ok
}
