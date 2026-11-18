package store

import (
	"time"

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
	at   time.Time // when it was noted (SetLatestSince's heardFor)
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
	r := liveRec{seq: s.liveSeq, gone: m == nil, at: liveClock()}
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

// LatestOpts is what a newest-page read knows besides its page.
type LatestOpts struct {
	// Since is LiveSeq when the request began.
	Since uint64
	// Pending are the viewer's sends Slack hasn't confirmed (pending or
	// failed), with the local ts their copies have: no page holds them,
	// and Slack keeps them apart from the history slices, so they stay
	// whatever the page's range (the window's copy first).
	Pending []slack.Message
	// Gen is GapGen when the request began: the window's gap boundary
	// (Timeline.Contig) holds for the socket's messages from then on only
	// while no slow connect came since.
	Gen uint64
}

// heardFor is how long a message the socket brought stays over a page
// newer than it that lacks it: a replica a moment behind the socket. After
// that the page is right (a deletion nothing told).
const heardFor = time.Minute

// liveClock stamps the live records (a var for the tests).
var liveClock = time.Now

// SetLatestSince is SetLatest for a newest-page read of ch, as Slack's
// history merges a page into its slices (1qrw addTimestamps, invH insert)
// and never takes out what the socket brought while the request was out:
//
//   - a message the socket changed after o.Since keeps the copy the window
//     has now (the socket's, with every change after it: a reaction, a
//     reply count, filled files), or the socket's own when the window
//     lacks it; one it deleted stays out;
//   - a top-level message (or a broadcast) the socket brought after
//     o.Since goes in when the page predates it;
//   - the viewer's unconfirmed sends stay, whatever the page's range, but
//     not one whose confirmed copy the page has (by client_msg_id);
//   - a page that reaches what the window had from Slack (its oldest at or
//     before the newest of it) joins it: the window keeps its older
//     messages and its HasMore, Slack's union of the slices the page
//     touches; the window's messages in the page's range that the page
//     lacks are gone from Slack (realtime off, a socket down: nothing told
//     it), and the page wins;
//   - one that doesn't reach it (a gap of more than a page: an overnight
//     sleep in a busy channel) starts the window over at the page, a slice
//     of its own, as Slack renders the newest slice and loads back from its
//     start: the window takes a new Epoch, which the lists take whole;
//   - of what the window held newer than the whole page, what the socket
//     brought in the last heardFor (a replica a moment behind can lack a
//     message it just sent) and ephemeral messages stay.
//
// It reports whether the window started over.
func (s *Store) SetLatestSince(ch string, msgs []slack.Message, hasMore bool, o LatestOpts) (restarted bool) {
	SortAsc(msgs)
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.timelines[ch]
	var held []slack.Message
	if t != nil {
		held = t.Msgs
	}
	cur := s.liveCopiesLocked(ch, held)
	since := o.Since
	pending := make(map[string]bool, len(o.Pending))
	for i := range o.Pending {
		pending[o.Pending[i].TS] = true
	}
	pageCID := map[string]bool{}
	for i := range msgs {
		if c := msgs[i].ClientMsgID; c != "" {
			pageCID[c] = true
		}
	}
	confirmedInPage := func(m *slack.Message) bool { return m.ClientMsgID != "" && pageCID[m.ClientMsgID] }
	out := make([]slack.Message, 0, len(msgs)+len(held)+4)
	have := make(map[string]bool, len(msgs)+len(held))
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
		if m := r.msg; !have[m.TS] && liveTopLevel(m) && (oldest == "" || !TSLess(m.TS, oldest)) && !(confirmedInPage(m) && pending[m.TS]) {
			mm, _, _ := cur(m.TS, since)
			have[m.TS] = true
			out = append(out, mm)
		}
	}
	// whether the page reaches what the window had from Slack: the newest
	// message the window holds contiguous with Slack's history (its gap
	// boundary, Timeline.Contig: a page read, then what the socket brought
	// while no slow connect came), as Slack's slices keep it
	// (resetReachedEnd, and the socket's messages a slice of their own
	// until a page intersects it). One global mark of the last slow
	// connect, read against a ring of live records, joined a page across
	// a gap after a second wake, or once the records rolled over, and
	// started a window over that had none.
	contig := ""
	if t != nil {
		contig = t.Contig
		if contig == "" {
			for i := len(held) - 1; i >= 0; i-- {
				if m := &held[i]; !pending[m.TS] && !m.IsEphemeral {
					contig = m.TS
					break
				}
			}
		}
	}
	restarted = t == nil || hasMore && oldest != "" && contig != "" && TSLess(contig, oldest)
	keptOlder := false
	now := liveClock()
	for _, m := range held {
		if have[m.TS] {
			continue
		}
		switch {
		case pending[m.TS]:
			if confirmedInPage(&m) {
				continue
			}
		case m.IsEphemeral:
			if restarted && oldest != "" && TSLess(m.TS, oldest) {
				continue
			}
		case newest != "" && TSLess(newest, m.TS):
			r, ok := s.live[ch+"/"+m.TS]
			if !ok || r.gone || r.msg == nil || now.Sub(r.at) > heardFor || confirmedInPage(&m) {
				continue
			}
		case oldest != "" && TSLess(m.TS, oldest):
			if restarted {
				continue
			}
			keptOlder = true
		default:
			continue // in the page's range and not in it: gone from Slack
		}
		have[m.TS] = true
		out = append(out, m)
	}
	// unconfirmed sends the window no longer holds (a purge took it)
	for _, m := range o.Pending {
		if !have[m.TS] && !confirmedInPage(&m) {
			have[m.TS] = true
			out = append(out, m)
		}
	}
	SortAsc(out)
	s.mergedLocked(ch, out, since)
	nt := &Timeline{Msgs: out, HasMore: hasMore, Loaded: true, PageOldest: oldest, Contig: newest, ContigGen: o.Gen}
	if keptOlder {
		nt.HasMore = t.HasMore
	}
	// the socket's messages heard while the request was out, with no slow
	// connect since the window's boundary, go on from the page
	if !restarted && t.ContigGen == o.Gen && o.Gen == s.gapGen && newest != "" && TSLess(newest, t.Contig) {
		nt.Contig = t.Contig
	}
	if restarted {
		s.epochN++
		nt.Epoch = s.epochN
	} else {
		nt.Epoch = t.Epoch
	}
	s.timelines[ch] = nt
	return restarted
}

// mergedLocked is where a newest-page merge (SetLatestSince) and a
// scrollback re-read (RefreshRange) have the window they're about to store
// (s.mu held): out, after the live copies won (since: the read's
// LiveSeq). A hook for what must see the merged window and never the raw
// page, such as replaying reactions noted since the read, or announcing a
// read revision of a message no older than a live-noted socket change.
func (s *Store) mergedLocked(ch string, out []slack.Message, since uint64) {}

// RefreshRange is a re-read of ch's scrollback (core's refreshOlder):
// msgs, the older page Slack answered for the range lo ≤ ts < hi (lo ""
// for none below), replaces what the window holds there, as
// SetLatestSince does in its page's range: the page's copy, unless the
// socket changed the message after o.Since (the window's copy stays) or
// deleted it (it stays out); a message of the window's the page lacks is
// gone from Slack, unless it is a local copy (o.Pending, an ephemeral
// message) or the socket brought it after o.Since. The rest of the window,
// and anything older than its oldest message, stays as it is. With no
// window it does nothing.
func (s *Store) RefreshRange(ch string, msgs []slack.Message, lo, hi string, o LatestOpts) {
	SortAsc(msgs)
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.timelines[ch]
	if t == nil || len(t.Msgs) == 0 {
		return
	}
	first := t.Msgs[0].TS
	in := func(ts string) bool {
		return !TSLess(ts, first) && (lo == "" || !TSLess(ts, lo)) && TSLess(ts, hi)
	}
	held := t.Msgs
	cur := s.liveCopiesLocked(ch, held)
	pending := make(map[string]bool, len(o.Pending))
	for i := range o.Pending {
		pending[o.Pending[i].TS] = true
	}
	out := make([]slack.Message, 0, len(held)+len(msgs))
	have := make(map[string]bool, len(held)+len(msgs))
	for _, m := range msgs {
		if !in(m.TS) {
			continue
		}
		if mm, gone, ok := cur(m.TS, o.Since); ok {
			if gone {
				continue
			}
			s.keepPinLocked(ch, &mm, &m)
			m = mm
		}
		have[m.TS] = true
		out = append(out, m)
	}
	for _, m := range held {
		if have[m.TS] {
			continue
		}
		if in(m.TS) && !pending[m.TS] && !m.IsEphemeral {
			if r, ok := s.live[ch+"/"+m.TS]; !ok || r.seq <= o.Since || r.gone {
				continue // gone from Slack
			}
		}
		have[m.TS] = true
		out = append(out, m)
	}
	SortAsc(out)
	s.mergedLocked(ch, out, o.Since)
	nt := *t
	nt.Msgs = out
	s.timelines[ch] = &nt
}

func liveTopLevel(m *slack.Message) bool {
	return m.ThreadTS == "" || m.ThreadTS == m.TS || m.Subtype == "thread_broadcast" || m.ReplyBroadcast
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

// ThreadOpts is what a replies read knows besides its answer.
type ThreadOpts struct {
	// Since is LiveSeq when the request began.
	Since uint64
	// Pending are the viewer's unconfirmed replies in the thread (local
	// ts): they stay (LatestOpts.Pending).
	Pending []slack.Message
}

// SetThreadSince is SetThread for a replies fetch of ch/ts whose request
// began at o.Since: a message the socket changed after that keeps the copy
// the thread holds now (or the socket's), one it deleted stays out, a
// reply it brought that the answer predates is kept, and so are the
// viewer's unconfirmed replies (o.Pending), unless the answer has the
// confirmed copy.
func (s *Store) SetThreadSince(ch, ts string, msgs []slack.Message, o ThreadOpts) {
	s.mu.Lock()
	defer s.mu.Unlock()
	held := s.threads[ch+"/"+ts]
	cur := s.liveCopiesLocked(ch, held)
	since := o.Since
	pending := make(map[string]bool, len(o.Pending))
	for i := range o.Pending {
		pending[o.Pending[i].TS] = true
	}
	pageCID := map[string]bool{}
	for i := range msgs {
		if c := msgs[i].ClientMsgID; c != "" {
			pageCID[c] = true
		}
	}
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
		m := r.msg
		if have[m.TS] || m.ThreadTS != ts || m.TS == ts || pending[m.TS] && m.ClientMsgID != "" && pageCID[m.ClientMsgID] {
			continue
		}
		mm, _, _ := cur(m.TS, since)
		have[m.TS] = true
		out = append(out, mm)
	}
	keep := func(m slack.Message) {
		if !have[m.TS] && !(m.ClientMsgID != "" && pageCID[m.ClientMsgID]) {
			have[m.TS] = true
			out = append(out, m)
		}
	}
	for _, m := range held {
		if pending[m.TS] {
			keep(m)
		}
	}
	for _, m := range o.Pending {
		keep(m)
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
