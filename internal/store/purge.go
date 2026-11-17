package store

import "strings"

// PurgeHistory drops ch's cached window and threads, as Slack's
// purgeChannelHistory with includeThreads does when client.counts says
// the history changed in a way the socket never told (a history_invalid
// past the one last applied: core/history_catchup.go). It returns the
// thread ts of the threads it dropped.
func (s *Store) PurgeHistory(ch string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.timelines, ch)
	var out []string
	for k := range s.threads {
		if ts, ok := strings.CutPrefix(k, ch+"/"); ok {
			delete(s.threads, k)
			out = append(out, ts)
		}
	}
	return out
}
