package fakeslack

import "github.com/some-du6e/clack/internal/slack"

// HistoryAt is ch's i-th newest message (0 the newest) as
// conversations.history serves it, its history made on first use; false
// past its end. Tests pick a message far back in a LongChannels channel
// with it (a permalink to an old message).
func (s *Server) HistoryAt(ch string, i int) (slack.Message, bool) {
	s.histMu.Lock()
	defer s.histMu.Unlock()
	h := s.histLocked(ch)
	if i < 0 || i >= len(h) {
		return slack.Message{}, false
	}
	return h[i], true
}
