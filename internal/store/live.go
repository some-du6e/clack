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

// SetLatestSince is SetLatest for a newest-page fetch of ch whose request
// began at LiveSeq since, as Slack's history pages merge into its message
// store by timestamp (addMessagesFromHistory) and never take out what the
// socket brought: a message the socket changed after that keeps the
// socket's copy, one it deleted stays out, and a top-level message (or a
// broadcast) it brought that the page predates goes in, as does a message
// the window held that is newer than the whole page (a pending send, one
// Upsert put in while the request was out). A page read before a socket
// message and stored after it used to drop that message for the session:
// every list re-reads its conversation from here (Owl's message missing
// from Karim's #meta while Activity showed it, α.102).
func (s *Store) SetLatestSince(ch string, msgs []slack.Message, hasMore bool, since uint64) {
	SortAsc(msgs)
	s.mu.Lock()
	defer s.mu.Unlock()
	newer := func(t string) (liveRec, bool) {
		r, ok := s.live[ch+"/"+t]
		return r, ok && r.seq > since
	}
	topLevel := func(m *slack.Message) bool {
		return m.ThreadTS == "" || m.ThreadTS == m.TS || m.Subtype == "thread_broadcast" || m.ReplyBroadcast
	}
	out := make([]slack.Message, 0, len(msgs)+4)
	have := make(map[string]bool, len(msgs))
	for _, m := range msgs {
		if r, ok := newer(m.TS); ok {
			if r.gone {
				continue
			}
			mm := *r.msg
			s.keepPinLocked(ch, &mm, &m)
			m = mm
		}
		have[m.TS] = true
		out = append(out, m)
	}
	oldest := ""
	if len(msgs) > 0 {
		oldest = msgs[0].TS
	}
	// what the socket brought while the request was out, within the page's
	// range or newer (an edit of an older message stays out of the window)
	for k, r := range s.live {
		if r.seq <= since || r.gone || r.msg == nil || len(k) <= len(ch) || k[:len(ch)+1] != ch+"/" {
			continue
		}
		if m := r.msg; !have[m.TS] && topLevel(m) && (oldest == "" || !TSLess(m.TS, oldest)) {
			have[m.TS] = true
			out = append(out, *m)
		}
	}
	// and what the window held newer than the whole page
	if t := s.timelines[ch]; t != nil && len(msgs) > 0 {
		newest := msgs[len(msgs)-1].TS
		for _, m := range t.Msgs {
			if !have[m.TS] && TSLess(newest, m.TS) {
				if r, ok := newer(m.TS); ok && r.gone {
					continue
				}
				have[m.TS] = true
				out = append(out, m)
			}
		}
	}
	SortAsc(out)
	s.timelines[ch] = &Timeline{Msgs: out, HasMore: hasMore, Loaded: true}
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
