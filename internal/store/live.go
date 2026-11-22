package store

import (
	"github.com/some-du6e/clack/internal/slack"
)

// Realtime message changes that a replies fetch must not undo.
//
// Slack's message_changed is replaceMessages: the event's copy replaces
// the stored message, which every view of it (the open thread pane's
// reply included) draws from at once. Clack's thread pane draws the
// thread cache, which a conversations.replies answer replaces wholesale
// (SetThread). When the request went out before a reply changed (an
// app's chat.update turning its "working..." placeholder into the answer
// a moment after posting it, as Karim's agent did, 2026-11-12) the
// answer carries the old copy; put back, the pane showed "working..."
// while Activity, fetched after the edit, showed the answer. So each
// realtime message, change and delete is noted here in order, and a
// fetch keeps what changed after it started (SetThreadSince), as pin
// events do (pins.go).

type liveRec struct {
	seq  uint64
	msg  *slack.Message // the socket's copy; nil when deleted
	gone bool
}

// maxLiveRecs bounds the remembered changes (a fetch takes seconds; a
// thousand messages of socket traffic is far more than one window).
const maxLiveRecs = 1024

// LiveSeq is the number of realtime message changes noted so far; a fetch
// reads it before its request and passes it to SetThreadSince.
func (s *Store) LiveSeq() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.liveSeq
}

// NoteLive records a realtime copy of a message in ch (a new message or a
// message_changed), or with m nil the deletion of ch/ts.
func (s *Store) NoteLive(ch, ts string, m *slack.Message) {
	if ch == "" || ts == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.noteLiveLocked(ch, ts, m)
}

// noteLiveLocked is NoteLive with s.mu held.
func (s *Store) noteLiveLocked(ch, ts string, m *slack.Message) {
	s.liveSeq++
	r := liveRec{seq: s.liveSeq, gone: m == nil}
	if m != nil {
		c := *m
		r.msg = &c
	}
	if s.live == nil {
		s.live = map[string]liveRec{}
	}
	k := ch + "/" + ts
	if _, ok := s.live[k]; !ok {
		s.liveQ = append(s.liveQ, k)
		for len(s.liveQ) > maxLiveRecs {
			delete(s.live, s.liveQ[0])
			s.liveQ = s.liveQ[1:]
		}
	}
	s.live[k] = r
}

// MutateNoted is Mutate for a write a read out now must not undo, the
// socket's or the viewer's own (message_replied's reply count, a lock, a
// link preview taken off): f runs on every copy and the copy it leaves is
// noted as a realtime change (NoteLive), under one lock, so no read lands
// between the two and puts its older copy back.
func (s *Store) MutateNoted(ch, ts string, f func(*slack.Message)) (slack.Message, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.mutateLocked(ch, ts, f)
	if ok && ch != "" && ts != "" {
		s.noteLiveLocked(ch, ts, &m)
	}
	return m, ok
}

// NoteMutate notes m, the socket's copy of ch/ts (message_changed), and
// runs f on every copy the store holds, under one lock: a read landing
// between the two kept the older copy, and announced it after the
// socket's change (core's announceHeld goes by the note). It reports the
// first copy and whether there was one.
func (s *Store) NoteMutate(ch, ts string, m *slack.Message, f func(*slack.Message)) (slack.Message, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ch != "" && ts != "" {
		s.noteLiveLocked(ch, ts, m)
	}
	return s.mutateLocked(ch, ts, f)
}

// NoteRemove notes the deletion of ch/ts and takes every copy out, under
// one lock (NoteLive with nil, then Remove).
func (s *Store) NoteRemove(ch, ts string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ch != "" && ts != "" {
		s.noteLiveLocked(ch, ts, nil)
	}
	s.removeLocked(ch, ts)
}

// HeldStamped is FindHeld with ch/ts's live stamp (LiveStamp), read under
// one lock.
func (s *Store) HeldStamped(ch, ts string) (slack.Message, uint64, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	stamp := s.liveStampLocked(ch, ts)
	if m := s.findRefLocked(ch, ts, ""); m != nil {
		return *m, stamp, true
	}
	if m := s.fetched[ch+"/"+ts]; m != nil {
		return *m, stamp, true
	}
	return slack.Message{}, stamp, false
}

// LiveStamp is the latest realtime change noted of ch/ts (a message, a
// change, a deletion, a reaction: their LiveSeq), 0 for none the store
// remembers. A copy read with one stamp is the message as it is until the
// stamp moves.
func (s *Store) LiveStamp(ch, ts string) uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.liveStampLocked(ch, ts)
}

func (s *Store) liveStampLocked(ch, ts string) uint64 {
	k := ch + "/" + ts
	var n uint64
	if r, ok := s.live[k]; ok {
		n = r.seq
	}
	if ops := s.reacts[k]; len(ops) > 0 {
		n = max(n, ops[len(ops)-1].seq)
	}
	return n
}

// liveReadLocked is the copy a read keeps of m, a message of ch its
// request (begun at LiveSeq since) brought: m itself, unless the socket
// changed m after since (live). Then it is the copy the store holds (a
// window's, a cached thread's, one fetched by id: it has the socket's
// change and every one after it), else the socket's own with the
// reactions noted after it replayed; gone when the socket deleted it
// meanwhile (s.mu held). The reads keep what changed while they were out
// with it (PutFetched, SetLatestRead, PrependOlderSince).
func (s *Store) liveReadLocked(ch string, m *slack.Message, since uint64) (keep slack.Message, live, gone bool) {
	k := ch + "/" + m.TS
	r, ok := s.live[k]
	if !ok || r.seq <= since {
		return *m, false, false
	}
	if r.gone {
		return slack.Message{}, true, true
	}
	var mm slack.Message
	if held := s.heldLocked(ch, m.TS); held != nil {
		mm = *held
	} else if f := s.fetched[k]; f != nil {
		mm = *f
	} else {
		mm = *r.msg
		s.replayReactsLocked(ch, &mm, r.seq)
	}
	if mm.ThreadTS == "" { // a message_changed copy without thread_ts stays in its thread
		mm.ThreadTS = m.ThreadTS
	}
	s.keepPinLocked(ch, &mm, m)
	return mm, true, false
}

// SetThreadSince is SetThread for a replies fetch of ch/ts whose request
// began at LiveSeq since: a message the socket changed after that keeps
// the socket's copy, one it deleted stays out, and a reply it brought that
// the answer predates is kept.
func (s *Store) SetThreadSince(ch, ts string, msgs []slack.Message, since uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.noteAddedLocked(msgs)
	newer := func(t string) (liveRec, bool) {
		r, ok := s.live[ch+"/"+t]
		return r, ok && r.seq > since
	}
	out := make([]slack.Message, 0, len(msgs)+1)
	have := make(map[string]bool, len(msgs))
	for _, m := range msgs {
		have[m.TS] = true
		if r, ok := newer(m.TS); ok {
			if r.gone {
				continue
			}
			mm := *r.msg
			s.keepPinLocked(ch, &mm, &m)
			if mm.ThreadTS == "" { // a message_changed copy without thread_ts stays in its thread
				mm.ThreadTS = m.ThreadTS
			}
			m = mm
		}
		out = append(out, m)
	}
	for k, r := range s.live {
		if r.seq <= since || r.gone || r.msg == nil || len(k) <= len(ch) || k[:len(ch)+1] != ch+"/" {
			continue
		}
		if m := r.msg; !have[m.TS] && m.ThreadTS == ts && m.TS != ts {
			out = append(out, *m)
		}
	}
	SortAsc(out)
	s.replayAllReactsLocked(ch, out, since) // the socket's reactions meanwhile (reacts.go)
	s.readThreadLocked(ch, ts, out)         // a new revision reaches every copy, and core (read_revisions.go)
	s.threads[ch+"/"+ts] = out
	s.setThreadWholeLocked(ch+"/"+ts, out) // the whole thread: one slice, both ends (thread_slices.go)
}
