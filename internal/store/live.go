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
// socket brought while they were out:
//
//   - a message the socket changed after since keeps the copy the window
//     has now (the socket's, with every change after it: a reaction, a
//     reply count, filled files), or the socket's own when the window
//     lacks it; one it deleted stays out;
//   - a top-level message (or a broadcast) the socket brought after since
//     goes in when the page predates it (core notes its own sends too);
//   - of what the window held newer than the whole page, what the socket
//     brought (a page from a replica a moment behind it can lack a message
//     it just sent; a deletion it missed moves the counts' history_invalid,
//     which purges the history: core/history_catchup.go), keep's ts (a send
//     Slack hasn't confirmed, or one that failed) and ephemeral messages
//     stay. What an earlier page put there the page would show, so Slack
//     deleted it while no event could say so (realtime off: the polls'
//     pages are all there is), and the page wins.
//
// A page read before a socket message and stored after it used to drop
// that message for the session: every list re-reads its conversation from
// here (Owl's message missing from Karim's #meta while Activity showed it,
// α.102).
func (s *Store) SetLatestSince(ch string, msgs []slack.Message, hasMore bool, since uint64, keep map[string]bool) {
	SortAsc(msgs)
	s.mu.Lock()
	defer s.mu.Unlock()
	cur := s.liveCopiesLocked(ch, s.timelineMsgsLocked(ch))
	out := make([]slack.Message, 0, len(msgs)+4)
	have := make(map[string]bool, len(msgs))
	for _, m := range msgs {
		if mm, gone, ok := cur(m.TS, since); ok {
			if gone {
				continue
			}
			s.keepPinLocked(ch, &mm, &m)
			m = mm
		}
		have[m.TS] = true
		out = append(out, m)
	}
	oldest, newest := "", ""
	if len(msgs) > 0 {
		oldest, newest = msgs[0].TS, msgs[len(msgs)-1].TS
	}
	// what the socket brought while the request was out, within the page's
	// range or newer (an edit of an older message stays out of the window)
	for k, r := range s.live {
		if r.seq <= since || r.gone || r.msg == nil || len(k) <= len(ch) || k[:len(ch)+1] != ch+"/" {
			continue
		}
		if m := r.msg; !have[m.TS] && liveTopLevel(m) && (oldest == "" || !TSLess(m.TS, oldest)) {
			mm, _, _ := cur(m.TS, since)
			have[m.TS] = true
			out = append(out, mm)
		}
	}
	// what of the window the page can't show
	if t := s.timelines[ch]; t != nil {
		for _, m := range t.Msgs {
			if have[m.TS] {
				continue
			}
			r, heard := s.live[ch+"/"+m.TS]
			heard = heard && !r.gone && r.msg != nil
			if (keep[m.TS] || heard) && (newest == "" || TSLess(newest, m.TS)) || m.IsEphemeral && (oldest == "" || !TSLess(m.TS, oldest)) {
				have[m.TS] = true
				out = append(out, m)
			}
		}
	}
	SortAsc(out)
	s.timelines[ch] = &Timeline{Msgs: out, HasMore: hasMore, Loaded: true}
}

func liveTopLevel(m *slack.Message) bool {
	return m.ThreadTS == "" || m.ThreadTS == m.TS || m.Subtype == "thread_broadcast" || m.ReplyBroadcast
}

// timelineMsgsLocked is ch's window, nil when none (s.mu held).
func (s *Store) timelineMsgsLocked(ch string) []slack.Message {
	if t := s.timelines[ch]; t != nil {
		return t.Msgs
	}
	return nil
}

// liveCopiesLocked returns a func that reports, for a ts of ch the socket
// changed after since, the copy a fetch keeps: the one held (the window's
// or the thread's: the socket's change and every later one), else the
// socket's own, and whether the socket deleted it. ok is false when
// nothing changed after since (s.mu held).
func (s *Store) liveCopiesLocked(ch string, held []slack.Message) func(ts string, since uint64) (m slack.Message, gone, ok bool) {
	idx := make(map[string]int, len(held))
	for i := range held {
		idx[held[i].TS] = i
	}
	return func(ts string, since uint64) (slack.Message, bool, bool) {
		r, ok := s.live[ch+"/"+ts]
		if !ok || r.seq <= since {
			return slack.Message{}, false, false
		}
		if r.gone || r.msg == nil {
			return slack.Message{}, true, true
		}
		if i, ok := idx[ts]; ok {
			return held[i], false, true
		}
		return *r.msg, false, true
	}
}

// SetThreadSince is SetThread for a replies fetch of ch/ts whose request
// began at LiveSeq since: a message the socket changed after that keeps
// the copy the thread holds now (or the socket's), one it deleted stays
// out, a reply it brought that the answer predates is kept, and so are
// keep's ts (the replies Slack hasn't confirmed, or that failed).
func (s *Store) SetThreadSince(ch, ts string, msgs []slack.Message, since uint64, keep map[string]bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	held := s.threads[ch+"/"+ts]
	cur := s.liveCopiesLocked(ch, held)
	out := make([]slack.Message, 0, len(msgs)+1)
	have := make(map[string]bool, len(msgs))
	for _, m := range msgs {
		have[m.TS] = true
		if mm, gone, ok := cur(m.TS, since); ok {
			if gone {
				continue
			}
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
			mm, _, _ := cur(m.TS, since)
			have[m.TS] = true
			out = append(out, mm)
		}
	}
	for _, m := range held {
		if !have[m.TS] && keep[m.TS] {
			have[m.TS] = true
			out = append(out, m)
		}
	}
	SortAsc(out)
	s.threads[ch+"/"+ts] = out
}

// LiveCopy is the socket's copy of ch/ts as its last realtime change left
// it, for a message no window or thread holds (a conversation nobody
// opened, a reply in a thread that isn't cached): Slack's store keeps
// every message the socket brings. false when it was deleted or nothing
// is noted.
func (s *Store) LiveCopy(ch, ts string) (slack.Message, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.live[ch+"/"+ts]
	if !ok || r.gone || r.msg == nil {
		return slack.Message{}, false
	}
	return *r.msg, true
}
