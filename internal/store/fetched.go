package store

import (
	"github.com/some-du6e/clack/internal/slack"
)

// Messages fetched by id (messages.list: Activity's, Saved's and Sent's
// hydration) that no cached window or thread holds.
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
// reads them. The oldest put goes first past maxFetched.

// maxFetched bounds the messages fetched by id (Activity's pages, Saved,
// Sent: a few hundred at most in a session's views).
const maxFetched = 2048

// PutFetched keeps messages.list's answer for ch, for a fetch whose
// request began at LiveSeq since: a message the socket changed after
// that keeps the socket's copy, and one it deleted stays out (as
// SetThreadSince does). A message a cached window or thread holds now
// isn't kept twice. It returns the messages as the store has them, the
// deleted ones left out.
func (s *Store) PutFetched(ch string, msgs []slack.Message, since uint64) []slack.Message {
	if ch == "" || len(msgs) == 0 {
		return msgs
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]slack.Message, 0, len(msgs))
	for _, m := range msgs {
		if m.TS == "" {
			continue
		}
		k := ch + "/" + m.TS
		if r, ok := s.live[k]; ok && r.seq > since {
			if r.gone {
				s.dropFetchedLocked(k)
				continue
			}
			mm := *r.msg
			if mm.ThreadTS == "" { // a message_changed copy without thread_ts stays in its thread
				mm.ThreadTS = m.ThreadTS
			}
			s.keepPinLocked(ch, &mm, &m)
			m = mm
		}
		if held := s.heldLocked(ch, m.TS); held != nil {
			// a window or thread loaded it meanwhile: that copy is the one
			// the socket keeps current
			out = append(out, *held)
			s.dropFetchedLocked(k)
			continue
		}
		s.keepPinLocked(ch, &m, nil)
		if s.fetched == nil {
			s.fetched = map[string]*slack.Message{}
		}
		if _, ok := s.fetched[k]; !ok {
			s.fetchedQ = append(s.fetchedQ, k)
			for len(s.fetchedQ) > maxFetched {
				delete(s.fetched, s.fetchedQ[0])
				s.fetchedQ = s.fetchedQ[1:]
			}
		}
		c := m
		s.fetched[k] = &c
		out = append(out, m)
	}
	return out
}

// dropFetchedLocked forgets the fetched copy under k.
func (s *Store) dropFetchedLocked(k string) {
	if _, ok := s.fetched[k]; !ok {
		return
	}
	delete(s.fetched, k)
	for i, q := range s.fetchedQ {
		if q == k {
			s.fetchedQ = append(s.fetchedQ[:i:i], s.fetchedQ[i+1:]...)
			break
		}
	}
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
