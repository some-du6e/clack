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
// the read lock: the timeline in order, then its windows' (windows.go),
// then each cached thread by root ts, thread(root) before its messages. f must not keep a message or call
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
	for _, w := range s.windows[ch] {
		for i := range w.Msgs {
			f(&w.Msgs[i])
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
	// then the messages fetched by id, after a mark of their own: they are
	// in the plugins' messages slice too (FetchedIn)
	if fs := s.fetchedInLocked(ch); len(fs) > 0 {
		thread("fetched")
		for _, m := range fs {
			f(m)
		}
	}
}

// FetchedIn returns copies of ch's messages fetched by id (Activity's and
// Saved's hydration, the Threads view's getView: fetched.go), by ts. Slack
// keeps one messages store: getMessageByTimestamp reads it, and a message
// it doesn't hold is fetched by id (fetchObjectWithId) into the same store,
// so the selectors and the plugins patching its slice see it beside the
// windows' and threads' messages. The plugin mirror's messages slice holds
// them too.
func (s *Store) FetchedIn(ch string) []slack.Message {
	s.mu.RLock()
	defer s.mu.RUnlock()
	fs := s.fetchedInLocked(ch)
	out := make([]slack.Message, len(fs))
	for i, m := range fs {
		out[i] = *m
	}
	return out
}

// FetchedHas reports, for each ts, whether ch has a copy of it fetched by
// id (fetched.go): a message the plugins' messages slice holds whatever
// else does (FetchedIn), so a load bringing it again isn't news to them.
func (s *Store) FetchedHas(ch string, tss []string) []bool {
	out := make([]bool, len(tss))
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i, ts := range tss {
		out[i] = s.fetched[ch+"/"+ts] != nil
	}
	return out
}

// FetchedChannels lists the conversations with messages fetched by id.
func (s *Store) FetchedChannels() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.fetchedBy))
	for ch := range s.fetchedBy {
		out = append(out, ch)
	}
	sort.Strings(out)
	return out
}

// fetchedInLocked is ch's messages fetched by id, by ts (s.mu held): read
// through fetchedBy, the channel's own, not a pass over every channel's.
func (s *Store) fetchedInLocked(ch string) []*slack.Message {
	by := s.fetchedBy[ch]
	if len(by) == 0 {
		return nil
	}
	out := make([]*slack.Message, 0, len(by))
	for ts := range by {
		if m := s.fetched[ch+"/"+ts]; m != nil {
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TS < out[j].TS })
	return out
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

// MessageChannels lists the conversations with a cached timeline, window
// or thread.
func (s *Store) MessageChannels() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	seen := make(map[string]bool, len(s.timelines))
	for ch := range s.timelines {
		seen[ch] = true
	}
	for ch, ws := range s.windows {
		if len(ws) > 0 {
			seen[ch] = true
		}
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

// WindowsIn is copies of ch's windows (windows.go), oldest first: a jump's
// stretches apart from the timeline, which Slack's messages store and
// channelHistory slices hold as they hold the newest messages.
func (s *Store) WindowsIn(ch string) []Window {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ws := s.windows[ch]
	if len(ws) == 0 {
		return nil
	}
	out := make([]Window, 0, len(ws))
	for _, w := range ws {
		out = append(out, copyWindow(w))
	}
	sort.Slice(out, func(i, j int) bool { return TSLess(out[i].Start, out[j].Start) })
	return out
}
