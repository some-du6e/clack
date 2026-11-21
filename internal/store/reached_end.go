package store

import "github.com/some-du6e/clack/internal/slack"

// Windows after a socket gap: Slack's reachedEnd.
//
// Slack keeps each channel's history as slices of contiguous
// timestamps (client-boot 6Yed's channelHistory store, om2Z's insert and
// union), and a slice "reaches the end" once a read showed nothing newer
// exists (reachedEnd). A message the socket brings joins the last slice
// only while that holds (addTimestamps' append: reachedEnd). At every
// connect that isn't a fast reconnect the socket store's
// slowConnectionCount moves (5rVH connected(isFast)), the reachedEnd
// manager goes stale ("Connection count a -> n: marking all as stale",
// p7oS _maybeResetState), and the onConnected handler (boot-shared erkq,
// "Resetting reachedEnd") dispatches resetReachedEnd (6Yed "Was
// disconnected; reset reachedEnd to false") over every channel: the
// socket missed whatever was posted while it was away. A view then draws
// the slice it had (dTxY getPotentiallyOutdatedTimestamps) and its list,
// at the bottom, asks for newer messages from the slice's end (akeQ
// loadMore → AEjl requestNewer → XEIU fetchHistory {oldest: end} →
// conversations.history oldest=end inclusive, limit 28, rGEL): the page
// joins the slice, and reaches the end again when has_more is false.
// When more remain, the newest view starts from the newest page instead
// (requestHistory({}) on an empty latest slice): two slices that don't
// meet are never drawn as one list.
//
// Clack's store keeps one window per conversation, so a window whose end
// went stale (EndStale) remembers its newest message from Slack then
// (End), and the socket's messages past it wait in Ahead, kept out of
// the window: appended to it, they drew right after the old window with
// the hours between missing and nothing to show it (Karim's #clack,
// 2026-11-20: 6:54 PM, then 9:53 PM). Core reads the window forward from
// End (CatchUp); a read that reaches the end joins Ahead to it. One that
// doesn't is followed by the newest page, which starts the window over
// (SetLatestRead), Ahead joined to it.

// maxAhead bounds the messages waiting past one stale end: past it the
// oldest go (a catch-up that far behind starts the window over with the
// newest page anyway, and those are on Slack's side).
const maxAhead = 100

// ResetReachedEnd is Slack's resetReachedEnd for every cached window, at
// a connect that wasn't a fast reconnect: each window's end goes stale,
// End its newest message from Slack (local is "ch/ts" of the viewer's
// unconfirmed and failed sends, which are no part of Slack's history;
// nor is an ephemeral message). It reports how many windows it reset.
func (s *Store) ResetReachedEnd(local map[string]bool) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.endGen++
	n := 0
	for ch, t := range s.timelines {
		t.EndGen = s.endGen // a read begun before this reset doesn't reach the end (CatchUp)
		if t.EndStale {
			continue // still waiting on the last reset's read: its End stands
		}
		t.EndStale, t.End, t.Ahead = true, "", nil
		for i := len(t.Msgs) - 1; i >= 0; i-- {
			if m := &t.Msgs[i]; !m.IsEphemeral && !local[ch+"/"+m.TS] {
				t.End = m.TS
				break
			}
		}
		n++
	}
	return n
}

// EndOf reports whether ch's window has a stale end and, if so, the
// message it is read forward from (End: "" when it held none from
// Slack, and only the newest page will do) and the reset it went stale
// at (EndGen, for CatchUp).
func (s *Store) EndOf(ch string) (end string, gen uint64, stale bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if t := s.timelines[ch]; t != nil && t.EndStale {
		return t.End, t.EndGen, true
	}
	return "", 0, false
}

// IsAhead reports whether ch/ts is a message waiting past a stale end
// (Ahead): no list draws it yet.
func (s *Store) IsAhead(ch, ts string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if t := s.timelines[ch]; t != nil {
		for i := range t.Ahead {
			if t.Ahead[i].TS == ts {
				return true
			}
		}
	}
	return false
}

// UpsertHeard is Upsert for a top-level message the socket brought: one
// past the end of a window whose end went stale waits in Ahead (it
// reports so) instead of going into the window.
func (s *Store) UpsertHeard(ch string, m slack.Message) (ahead bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.timelines[ch]
	if t == nil {
		return false
	}
	for i := range t.Msgs {
		if t.Msgs[i].TS == m.TS {
			s.keepPinLocked(ch, &m, &t.Msgs[i])
			t.Msgs[i] = m
			return false
		}
	}
	if s.aheadLocked(ch, t, m) {
		return true
	}
	s.keepPinLocked(ch, &m, nil)
	t.Msgs = append(t.Msgs, m)
	SortAsc(t.Msgs)
	return false
}

// aheadLocked puts m, a message of ch that t's messages don't hold, into
// t.Ahead when t's end is stale and m is past it (a copy there is
// replaced), and reports whether it did (s.mu held).
func (s *Store) aheadLocked(ch string, t *Timeline, m slack.Message) bool {
	if !t.EndStale || t.End != "" && !TSLess(t.End, m.TS) {
		return false
	}
	for i := range t.Ahead {
		if t.Ahead[i].TS == m.TS {
			s.keepPinLocked(ch, &m, &t.Ahead[i])
			t.Ahead[i] = m
			return true
		}
	}
	s.keepPinLocked(ch, &m, nil)
	t.Ahead = append(t.Ahead, m)
	SortAsc(t.Ahead)
	if len(t.Ahead) > maxAhead {
		t.Ahead = append([]slack.Message(nil), t.Ahead[len(t.Ahead)-maxAhead:]...)
	}
	return true
}

// CatchUp takes a page of ch read forward from from (conversations.history
// oldest=from inclusive) whose request began at LiveSeq since, into a
// window whose end went stale at from at reset gen (EndOf): the page
// joins the window (what the socket changed or deleted while it was out
// keeps the socket's state, and the read's hooks run over what it keeps,
// as every window read: readWindowLocked), and End moves to the page's
// newest message. With nothing newer left (hasMore false) the window
// reaches the end again: the messages waiting in Ahead join it, and it
// reports joined; unless the socket went and came back while the page
// was out (a later reset than gen), when what it missed then is after
// the page: the end stays stale and the window is read forward again. A
// confirmed copy of one of the viewer's sends in the page takes the
// place of the local one (same client_msg_id). ok is false when the
// window isn't the one the page was read for any more (a newest page
// caught it up or started it over, or it went): the page is dropped and
// the catch-up is over.
func (s *Store) CatchUp(ch, from string, gen uint64, msgs []slack.Message, hasMore bool, since uint64) (joined, ok bool) {
	SortAsc(msgs)
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.timelines[ch]
	if t == nil || !t.EndStale || t.End != from {
		return false, false
	}
	out := s.liveKeepLocked(ch, msgs, since)
	s.readWindowLocked(ch, out, since)
	// the page's copies win over the window's (dedupe keeps the later);
	// a local copy of a send the page holds confirmed goes
	sent := map[string]string{}
	for i := range out {
		if out[i].ClientMsgID != "" {
			sent[out[i].ClientMsgID] = out[i].TS
		}
	}
	kept := make([]slack.Message, 0, len(t.Msgs)+len(out))
	for i := range t.Msgs {
		if ts, ok := sent[t.Msgs[i].ClientMsgID]; ok && t.Msgs[i].ClientMsgID != "" && ts != t.Msgs[i].TS {
			continue
		}
		kept = append(kept, t.Msgs[i])
	}
	t.Msgs = dedupe(append(kept, out...))
	newest := newestTS(msgs)
	if newest == "" {
		newest = from // from itself went, and nothing came after it
	}
	if hasMore || t.EndGen != gen {
		if TSLess(t.End, newest) {
			t.End = newest
		}
		return false, true
	}
	t.Msgs = joinAhead(t.Msgs, newest, t.Ahead)
	t.EndStale, t.End, t.Ahead = false, "", nil
	return true, true
}

// joinAhead is msgs, read up to newest, with the messages that waited
// past a stale end that msgs doesn't hold: those newer than the read
// (they came after it) and ephemeral ones (no read holds them). One the
// read covers and doesn't hold is gone (deleted while the socket was
// away). Ascending.
func joinAhead(msgs []slack.Message, newest string, ahead []slack.Message) []slack.Message {
	if len(ahead) == 0 {
		return msgs
	}
	held := make(map[string]bool, len(msgs))
	for i := range msgs {
		held[msgs[i].TS] = true
	}
	out := msgs
	for _, m := range ahead {
		if held[m.TS] || !m.IsEphemeral && newest != "" && !TSLess(newest, m.TS) {
			continue
		}
		out = append(out, m)
	}
	SortAsc(out)
	return out
}

// newestTS is the newest ts among msgs ("" for none).
func newestTS(msgs []slack.Message) string {
	n := ""
	for i := range msgs {
		if n == "" || TSLess(n, msgs[i].TS) {
			n = msgs[i].TS
		}
	}
	return n
}

// PrependOlderAt is PrependOlderSince for a page read before before (the
// oldest message the list that asked held): it goes in front of ch's
// window only where the window still starts there or earlier. A window
// read again meanwhile (a newest page that didn't reach the old one, a
// catch-up that started it over, an eviction) starts later: in front of
// it the page left the messages between out, and the next list to read
// the window drew the hole as one list. Nor does an older page make a
// window where none is any more. It returns what the read kept either
// way (the list that asked holds what the page continues).
func (s *Store) PrependOlderAt(ch, before string, msgs []slack.Message, hasMore bool, since uint64) []slack.Message {
	SortAsc(msgs)
	s.mu.Lock()
	defer s.mu.Unlock()
	msgs = s.liveKeepLocked(ch, msgs, since)
	s.readWindowLocked(ch, msgs, since)
	t := s.timelines[ch]
	if t == nil {
		return msgs
	}
	for i := range t.Msgs {
		if !t.Msgs[i].IsEphemeral {
			if TSLess(before, t.Msgs[i].TS) {
				return msgs // the window starts after the page ends
			}
			break
		}
	}
	t.Msgs = dedupe(append(append([]slack.Message(nil), msgs...), t.Msgs...))
	t.HasMore = hasMore
	if len(t.Msgs) > 5000 { // bound memory per channel
		t.Msgs = t.Msgs[len(t.Msgs)-5000:]
	}
	return msgs
}
