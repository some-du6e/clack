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

// SetThreadSince is SetThread for a replies fetch of ch/ts whose request
// began at LiveSeq since: a message the socket changed after that keeps
// the socket's copy, one it deleted stays out, and a reply it brought that
// the answer predates is kept.
func (s *Store) SetThreadSince(ch, ts string, msgs []slack.Message, since uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
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
	s.threads[ch+"/"+ts] = out
}
