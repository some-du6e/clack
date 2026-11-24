package fakeslack

import "sync"

// client.counts' threads.unread_count_by_channel: how many of each
// conversation's followed threads have unread replies (Slack's
// threadsUnreadCountByChannel). The fake answers none unless a test sets
// it, so a counts read leaves every conversation at 0. Its has_unreads
// and mention_count are unread threads with 2 mentions unless a test sets
// them (SetThreadsState).

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

// countsThreads is what SetThreadsState set.
type countsThreads struct {
	unread   bool
	mentions int
}

// SetThreadsState makes client.counts answer threads.has_unreads and
// mention_count as given: the server's state, as an update_thread_state a
// test pushes says it is. Slack's server answers a counts read after the
// event with the state the event carried, and core takes an answer to a
// read sent after an event over the event (store.SetCountsSince); with
// the fake's own answer (unread threads, 2 mentions) a counts read sent
// just after a pushed "nothing unread" put the threads back to unread.
func (s *Server) SetThreadsState(unread bool, mentions int) {
	threadsByChannelMu.Lock()
	defer threadsByChannelMu.Unlock()
	s.threadsState = &countsThreads{unread: unread, mentions: mentions}
}

func (s *Server) threadsUnread() (unread bool, mentions int) {
	threadsByChannelMu.Lock()
	defer threadsByChannelMu.Unlock()
	if s.threadsState == nil {
		return true, 2
	}
	return s.threadsState.unread, s.threadsState.mentions
}
