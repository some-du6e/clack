package fakeslack

import "sync"

// client.counts' threads.unread_count_by_channel: how many of each
// conversation's followed threads have unread replies (Slack's
// threadsUnreadCountByChannel). The fake answers none unless a test sets
// it, so a counts read leaves every conversation at 0.

var threadsByChannelMu sync.Mutex

// SetThreadsByChannel makes client.counts answer m as
// threads.unread_count_by_channel (nil: none).
func (s *Server) SetThreadsByChannel(m map[string]int) {
	threadsByChannelMu.Lock()
	defer threadsByChannelMu.Unlock()
	if m == nil {
		s.threadsByChan = nil
		return
	}
	s.threadsByChan = make(map[string]int, len(m))
	for k, v := range m {
		s.threadsByChan[k] = v
	}
}

func (s *Server) threadsByChannel() map[string]int {
	threadsByChannelMu.Lock()
	defer threadsByChannelMu.Unlock()
	if s.threadsByChan == nil {
		return nil
	}
	out := make(map[string]int, len(s.threadsByChan))
	for k, v := range s.threadsByChan {
		out[k] = v
	}
	return out
}
