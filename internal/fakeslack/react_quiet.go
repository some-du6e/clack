package fakeslack

import "github.com/some-du6e/clack/internal/slack"

// ReactQuiet adds a reaction name by users to the message ts in ch's
// history, unpushed (no realtime event), and reports whether it found the
// message.
func (s *Server) ReactQuiet(ch, ts, name string, users ...string) bool {
	s.histMu.Lock()
	defer s.histMu.Unlock()
	h := s.histLocked(ch)
	for i := range h {
		if h[i].TS != ts {
			continue
		}
		h[i].Reactions = append(h[i].Reactions, slack.Reaction{Name: name, Count: len(users), Users: append([]string(nil), users...)})
		return true
	}
	return false
}
