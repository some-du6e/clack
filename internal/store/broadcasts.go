package store

import "github.com/some-du6e/clack/internal/slack"

// MutateBroadcastRoots applies f to the root object (Root, from the API)
// of every thread_broadcast of thread root in ch's timeline that has one,
// and returns copies of those broadcasts as they are after f. A
// broadcast's root is Slack's copy of the parent (udbq
// makeBroadcastRootObject adds it to the message store), so it keeps the
// thread's reply metadata when the parent itself isn't cached.
func (s *Store) MutateBroadcastRoots(ch, root string, f func(*slack.Message)) []slack.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.timelines[ch]
	if t == nil || root == "" {
		return nil
	}
	var out []slack.Message
	for i := range t.Msgs {
		m := &t.Msgs[i]
		if m.Subtype != "thread_broadcast" || m.Root == nil || firstNonEmptyTS(m.Root.TS, m.ThreadTS) != root {
			continue
		}
		r := *m.Root // the copy the broadcast was decoded with stays as it was
		f(&r)
		m.Root = &r
		out = append(out, *m)
	}
	return out
}

func firstNonEmptyTS(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
