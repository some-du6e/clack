package store

// Read-only accessors for internal/taut's Redux mirror (Taut plugin
// compatibility). They copy, so callers never hold the store's lock or its
// slices. Message contents stay in memory, as everywhere in the store.

import (
	"sort"
	"strings"

	"github.com/some-du6e/clack/internal/slack"
)

// VisitMessages calls f with each of ch's cached messages in place, under
// the read lock: the timeline in order, then each cached thread by root
// ts, thread(root) before its messages. f must not keep a message or call
// into the store. (The plugin mirror fingerprints every channel it
// watches this way every 750 ms; copying each timeline and its threads
// out, as Timeline and ThreadsIn do, made megabytes of garbage a sweep in
// a long session.)
func (s *Store) VisitMessages(ch string, thread func(root string), f func(m *slack.Message)) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if t := s.timelines[ch]; t != nil {
		for i := range t.Msgs {
			f(&t.Msgs[i])
		}
	}
	prefix := ch + "/"
	var keys []string
	for k := range s.threads {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys) // one prefix: the roots' order
	for _, k := range keys {
		thread(k[len(prefix):])
		th := s.threads[k]
		for i := range th {
			f(&th[i])
		}
	}
}

// ThreadsIn returns copies of the cached threads of conversation ch, keyed
// by root ts (replies ascending, the root included when Slack sent it).
func (s *Store) ThreadsIn(ch string) map[string][]slack.Message {
	s.mu.RLock()
	defer s.mu.RUnlock()
	prefix := ch + "/"
	var out map[string][]slack.Message
	for k, msgs := range s.threads {
		if !strings.HasPrefix(k, prefix) {
			continue
		}
		if out == nil {
			out = map[string][]slack.Message{}
		}
		out[k[len(prefix):]] = append([]slack.Message(nil), msgs...)
	}
	return out
}

// MessageChannels lists the conversations with a cached timeline or thread.
func (s *Store) MessageChannels() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	seen := make(map[string]bool, len(s.timelines))
	for ch := range s.timelines {
		seen[ch] = true
	}
	for k := range s.threads {
		if i := strings.IndexByte(k, '/'); i > 0 {
			seen[k[:i]] = true
		}
	}
	out := make([]string, 0, len(seen))
	for ch := range seen {
		out = append(out, ch)
	}
	sort.Strings(out)
	return out
}

// BotIDs lists the bots whose display metadata is cached.
func (s *Store) BotIDs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.bots))
	for id := range s.bots {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// UserIDs lists every cached user.
func (s *Store) UserIDs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.Users))
	for id := range s.Users {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// ConvIDs lists the sidebar conversations and the ones known from search,
// browse or links (ext).
func (s *Store) ConvIDs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.Convs)+len(s.ext))
	for id := range s.Convs {
		out = append(out, id)
	}
	for id := range s.ext {
		if _, dup := s.Convs[id]; !dup {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// ThreadHas reports whether a message of the cached thread ch/ts (the
// root included when Slack sent it) satisfies match, without copying the
// thread. match runs under the store's read lock: it must not call the
// store. False when the thread isn't cached.
func (s *Store) ThreadHas(ch, ts string, match func(*slack.Message) bool) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	th := s.threads[ch+"/"+ts]
	for i := range th {
		if match(&th[i]) {
			return true
		}
	}
	return false
}
