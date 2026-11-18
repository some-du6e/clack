package store

import (
	"cmp"
	"slices"

	"github.com/some-du6e/clack/internal/slack"
)

// Messages fetched by id (messages.list: Activity's and Saved's
// hydration, the Threads view's getView), kept where the socket's changes
// reach them.
//
// Slack keeps one messages store. getMessageByTimestamp (client-boot
// iR00 Ld) reads it, and a message it doesn't hold is fetched by id
// (fetchObjectWithId: messages.list) into the same store, where the
// socket's changes reach it like any other message's: message_changed
// replaces it (replaceMessages), the reactions reducer counts a
// reaction_added on it and message_deleted removes it. Every Activity
// row reads its message from there, so a vote's chat.update shows in
// Activity in the same commit as in the channel. Clack's store held only
// the conversations' windows and threads: the message behind an Activity
// entry from a conversation nobody had opened changed nowhere, so a
// reaction to it emitted nothing and its row kept the copy the feed was
// hydrated with.
//
// Find leaves these out, so a hydrate asks Slack again as every feed load
// did (without the socket, nothing else would bring their changes); the
// writes a socket event makes (Mutate, Remove) reach them, and FindHeld
// reads them. A message a window or a cached thread holds keeps a copy
// here too (KeepFetched): core evicts windows while Activity still draws
// their messages. Past maxFetched messages or maxFetchedSize bytes the
// one used longest ago goes.

const (
	// maxFetched bounds the messages fetched by id (Activity's pages,
	// Saved, the Threads view: a few hundred at most in a session's
	// views).
	maxFetched = 2048
	// maxFetchedSize bounds their size (messageSize, as Slack's message
	// budget counts it: store/evict.go).
	maxFetchedSize = 4 << 20
)

// fetchedUse is a fetched message's latest put or keep and its size.
type fetchedUse struct {
	at   uint64
	size int
}

// fetchedTick is one use of a fetched message, in Store.fetchedQ.
type fetchedTick struct {
	k  string
	at uint64
}

// PutFetched keeps messages.list's answer for ch, for a fetch whose
// request began at LiveSeq since, merged the way SetLatestSince merges a
// window's newest page (store/live.go):
//
//   - a message the socket changed after since keeps the copy the store
//     holds now (a window's, a cached thread's, or the fetched one: the
//     socket's change and every later one), else the socket's own, with
//     the reactions that came after it; one the socket deleted stays out;
//   - any other takes the answer, with the reactions the socket brought
//     after since replayed on it (reacts.go). Each counts a person once,
//     so a reaction the answer already has changes nothing.
//
// It returns the messages as the store has them, the deleted ones left
// out.
func (s *Store) PutFetched(ch string, msgs []slack.Message, since uint64) []slack.Message {
	if ch == "" || len(msgs) == 0 {
		return msgs
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.putFetchedPageLocked(ch, msgs, since)
}

// putFetchedPageLocked is PutFetched with s.mu held.
func (s *Store) putFetchedPageLocked(ch string, msgs []slack.Message, since uint64) []slack.Message {
	out := make([]slack.Message, 0, len(msgs))
	for _, m := range msgs {
		if m.TS == "" {
			continue
		}
		k := ch + "/" + m.TS
		mm, live, gone := s.liveReadLocked(ch, &m, since) // live.go
		switch {
		case gone:
			s.dropFetchedLocked(k)
			continue
		case live:
			m = mm
		default:
			s.replayReactsLocked(ch, &m, since)
			s.keepPinLocked(ch, &m, nil)
		}
		s.noteReadLocked(ch, s.fetched[k], &m) // read_revisions.go
		s.putFetchedLocked(k, m)
		out = append(out, m)
	}
	return out
}

// PutFetchedAsked is PutFetched for a messages.list answer of ch, which
// was asked for asked: a message asked for that the answer doesn't have is
// gone (Slack's isNonExistent), and its fetched copy goes in the same
// write, so the store never draws a deleted message from a copy an earlier
// read left (the views take the item's MsgGone, or core's "gone").
func (s *Store) PutFetchedAsked(ch string, asked []string, msgs []slack.Message, since uint64) []slack.Message {
	if ch == "" {
		return msgs
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.putFetchedPageLocked(ch, msgs, since)
	have := make(map[string]bool, len(msgs))
	for i := range msgs {
		have[msgs[i].TS] = true
	}
	for _, ts := range asked {
		if ts != "" && !have[ts] {
			s.dropFetchedLocked(ch + "/" + ts)
		}
	}
	return out
}

// KeepFetched keeps a fetched copy of the messages of ch with tss that a
// window or a cached thread holds (a hydrate served from memory), so the
// socket's changes still reach them once the window is evicted, and
// counts this as their use. One the store holds nowhere is left out.
func (s *Store) KeepFetched(ch string, tss []string) {
	if ch == "" || len(tss) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ts := range tss {
		k := ch + "/" + ts
		if held := s.heldLocked(ch, ts); held != nil {
			s.putFetchedLocked(k, *held) // the copy the hydrate served
		} else if f := s.fetched[k]; f != nil {
			s.putFetchedLocked(k, *f)
		}
	}
}

// putFetchedLocked keeps m under k as its latest use, then lets go of the
// messages used longest ago past maxFetched or maxFetchedSize.
func (s *Store) putFetchedLocked(k string, m slack.Message) {
	if s.fetched == nil {
		s.fetched = map[string]*slack.Message{}
		s.fetchedUse = map[string]fetchedUse{}
	}
	s.fetchedN++
	u := fetchedUse{at: s.fetchedN, size: messageSize(&m)}
	s.fetchedSize += u.size - s.fetchedUse[k].size
	s.fetched[k] = &m
	s.fetchedUse[k] = u
	s.fetchedQ = append(s.fetchedQ, fetchedTick{k, u.at})
	for len(s.fetchedQ) > 0 && (len(s.fetched) > maxFetched || s.fetchedSize > maxFetchedSize && len(s.fetched) > 1) {
		t := s.fetchedQ[0]
		if t.k == k && t.at == u.at {
			break // the message just kept is all there is
		}
		s.fetchedQ = s.fetchedQ[1:]
		if s.fetchedUse[t.k].at == t.at {
			s.dropFetchedLocked(t.k)
		}
	}
	if len(s.fetchedQ) > 2*len(s.fetched)+64 {
		// the uses a later one superseded: rebuild the queue from the
		// latest ones, oldest first
		q := make([]fetchedTick, 0, len(s.fetched))
		for fk, fu := range s.fetchedUse {
			q = append(q, fetchedTick{fk, fu.at})
		}
		slices.SortFunc(q, func(a, b fetchedTick) int { return cmp.Compare(a.at, b.at) })
		s.fetchedQ = q
	}
}

// measureFetchedLocked counts the fetched copy under k at its size now
// (the socket grew it in place: Mutate), then lets go of the copies used
// longest ago past the bounds.
func (s *Store) measureFetchedLocked(k string) {
	m := s.fetched[k]
	if m == nil {
		return
	}
	u := s.fetchedUse[k]
	n := messageSize(m)
	s.fetchedSize += n - u.size
	u.size = n
	s.fetchedUse[k] = u
	for len(s.fetchedQ) > 0 && s.fetchedSize > maxFetchedSize && len(s.fetched) > 1 {
		t := s.fetchedQ[0]
		if t.k == k && t.at == u.at {
			break // the copy just grown is the newest use there is
		}
		s.fetchedQ = s.fetchedQ[1:]
		if s.fetchedUse[t.k].at == t.at {
			s.dropFetchedLocked(t.k)
		}
	}
}

// dropFetchedLocked forgets the fetched copy under k.
func (s *Store) dropFetchedLocked(k string) {
	if _, ok := s.fetched[k]; !ok {
		return
	}
	s.fetchedSize -= s.fetchedUse[k].size
	delete(s.fetched, k)
	delete(s.fetchedUse, k)
}

// heldLocked is ch/ts as a cached window or thread holds it (Find's copy).
func (s *Store) heldLocked(ch, ts string) *slack.Message {
	return s.findRefLocked(ch, ts, "")
}

// FindHeld is Find, then the messages fetched by id: any copy of ch/ts
// the store keeps current from the socket.
func (s *Store) FindHeld(ch, ts string) (slack.Message, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if m := s.findRefLocked(ch, ts, ""); m != nil {
		return *m, true
	}
	if m := s.fetched[ch+"/"+ts]; m != nil {
		return *m, true
	}
	return slack.Message{}, false
}
