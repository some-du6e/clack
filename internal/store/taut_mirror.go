package store

// Read-only accessors for internal/taut's Redux mirror (Taut plugin
// compatibility). They copy, so callers never hold the store's lock or its
// slices. Message contents stay in memory, as everywhere in the store.

import (
	"sort"
	"strings"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
)

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
