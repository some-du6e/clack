package fakeslack

import (
	"sync"

	"github.com/some-du6e/clack/internal/slack"
)

// postState is the newest top-level message PostQuiet put into each
// conversation's history, and whether client.counts follows it
// (CountsFollowPosts).
type postState struct {
	mu     sync.Mutex
	follow bool
	latest map[string]string
}

// CountsFollowPosts makes client.counts follow the messages a test posts
// (PostQuiet and what builds on it): a conversation's latest is its
// newest posted message when that is newer than the fixture's, and the
// conversation is unread while its last_read is before it, as Slack's
// counts are (a mark still reads it: applyMark). Off by default, so the
// tests written against the fixture's fixed counts keep them; on, a test
// can post into a conversation while the client's socket is away and see
// whether the client's counts catch up.
func (s *Server) CountsFollowPosts(on bool) {
	s.posts.mu.Lock()
	s.posts.follow = on
	s.posts.mu.Unlock()
}

// notePosted records ts as ch's newest posted top-level message.
func (s *Server) notePosted(ch, ts string) {
	s.posts.mu.Lock()
	defer s.posts.mu.Unlock()
	if s.posts.latest == nil {
		s.posts.latest = map[string]string{}
	}
	if old := s.posts.latest[ch]; old == "" || slackTSLess(old, ts) {
		s.posts.latest[ch] = ts
	}
}

// postedCount is e with ch's newest posted message as its latest, when
// counts follow the posts and that one is newer.
func (s *Server) postedCount(ch string, e slack.CountEntry) slack.CountEntry {
	s.posts.mu.Lock()
	follow, l := s.posts.follow, s.posts.latest[ch]
	s.posts.mu.Unlock()
	if !follow || l == "" || !slackTSLess(e.Latest, l) {
		return e
	}
	e.Latest = l
	e.HasUnreads = slackTSLess(e.LastRead, l)
	return e
}

// HistoryTS is the ts of every message ch's history serves
// (conversations.history), oldest first: what a client's window of ch is
// checked against for holes.
func (s *Server) HistoryTS(ch string) []string {
	s.histMu.Lock()
	h := s.histLocked(ch)
	s.histMu.Unlock()
	out := make([]string, 0, len(h))
	for i := len(h) - 1; i >= 0; i-- {
		out = append(out, h[i].TS)
	}
	return out
}
