package store

import (
	"maps"
	"math"
	"sync/atomic"

	"github.com/some-du6e/clack/internal/slack"
)

// The write journal: a sequence every change to the sidebar's state
// advances, and for each conversation the position of its last mark, of
// its last new message, and of its last local membership change or edit
// (left, archived, deleted, joined, created, opened, renamed), and the
// position of the last change to the section list. A list fetch
// notes Seq as it goes out, and its answer installs only what nothing
// changed after that (the ...Since methods), the way Slack's reducers
// merge a boot API's answer into the store.
//
// Core's warm boot needs it: the window is usable from the boot data
// while Boot's calls are out, so a mark, a leave or a section move made
// then is newer than the answer that lands after it. Stars and saved
// items have their own (core's starEdits, savedEdits).

type journal struct {
	seq      atomic.Uint64
	marks    map[string]uint64 // conversation -> its last read cursor change (s.mu)
	msgs     map[string]uint64 // conversation -> its last new message (s.mu)
	convs    map[string]uint64 // conversation -> its last local membership change or edit (s.mu)
	sections uint64            // the last change to the section list (s.mu)
	threads  uint64            // the last update_thread_state the threads' state took (s.mu)
}

// Seq is the journal's position now: a fetch notes it as it goes out.
func (s *Store) Seq() uint64 { return s.jr.seq.Load() }

// NextSeq advances the journal and returns the new position, for changes
// core keeps itself (a DM's open flag, the Activity badges).
func (s *Store) NextSeq() uint64 { return s.jr.seq.Add(1) }

func (s *Store) noteMarkLocked(ch string) {
	if s.jr.marks == nil {
		s.jr.marks = map[string]uint64{}
	}
	s.jr.marks[ch] = s.jr.seq.Add(1)
}

func (s *Store) noteMessageLocked(ch string) {
	if s.jr.msgs == nil {
		s.jr.msgs = map[string]uint64{}
	}
	s.jr.msgs[ch] = s.jr.seq.Add(1)
}

// countChangedLocked reports whether ch's unread state changed after
// since (a mark or a message).
func (s *Store) countChangedLocked(ch string, since uint64) bool {
	return s.jr.marks[ch] > since || s.jr.msgs[ch] > since
}

func (s *Store) noteConvLocked(ch string) {
	if s.jr.convs == nil {
		s.jr.convs = map[string]uint64{}
	}
	s.jr.convs[ch] = s.jr.seq.Add(1)
}

// CountChangedSince reports whether ch's unread state changed after since.
func (s *Store) CountChangedSince(ch string, since uint64) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.countChangedLocked(ch, since)
}

// SetCountsSince installs a client.counts answer fetched at journal
// position since, merged per conversation with what changed after since:
//
//   - a mark (the read cursor moved here while the call was out): the
//     local cursor, unread flag and mention count stay, Slack's are older
//     than the mark;
//   - only new messages: Slack's cursor, unread flag and mention count
//     (it knows mentions the client can't count, below a partial mark),
//     and a message newer than the answer still unread, its latest kept,
//     the mention count never below the local one;
//   - nothing: Slack's.
//
// An IM's or group DM's entry then takes Slack's unread count rule
// (dmUnreadCount). It reports how many entries it merged: their state is
// partly older than Slack's now, so the caller reads the counts once
// more.
func (s *Store) SetCountsSince(c *slack.Counts, since uint64) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	old := s.Counts
	s.Counts = make(map[string]*slack.CountEntry, len(old))
	merged := 0
	for li, l := range [][]slack.CountEntry{c.Channels, c.IMs, c.MPIMs} {
		dm := li > 0 // the answer's ims and mpims (Slack's fetcher flags them is_im, is_mpim)
		for i := range l {
			e := l[i]
			o, had := old[e.ID]
			switch {
			case had && s.jr.marks[e.ID] > since:
				m := *o
				if TSLess(m.Latest, e.Latest) { // a message newer than the client knew
					m.Latest = e.Latest
					if m.LastRead == "" || TSLess(m.LastRead, e.Latest) {
						m.HasUnreads = true
					}
				}
				if dm {
					m = dmUnreadCount(m, s.Muted[e.ID])
				}
				s.Counts[e.ID] = &m
				merged++
				continue
			case had && s.jr.msgs[e.ID] > since:
				if TSLess(e.Latest, o.Latest) { // a message the answer can't have counted
					e.Latest = o.Latest
					if e.LastRead == "" || TSLess(e.LastRead, o.Latest) {
						e.HasUnreads = true
					}
					e.MentionCount = max(e.MentionCount, o.MentionCount)
				}
				merged++
			}
			if dm {
				e = dmUnreadCount(e, s.Muted[e.ID])
			}
			s.Counts[e.ID] = &e
		}
	}
	for id, o := range old { // changed since, and not in the answer
		if _, ok := s.Counts[id]; !ok && s.countChangedLocked(id, since) {
			s.Counts[id] = o
			merged++
		}
	}
	if s.jr.threads <= since { // an update_thread_state after the request went out is newer
		s.ThreadsUnread = c.Threads.HasUnreads
		s.ThreadMentions = c.Threads.MentionCount
		s.ThreadsUnreadByChannel = c.Threads.UnreadCountByChannel
	}
	s.countsLive = true
	return merged
}

// SetThreadState is update_thread_state's state, taken at once as Slack's
// updateThreadState takes it: whether any followed thread has unread
// replies, their mention count, and how many threads of each conversation
// have unread replies (the reply bar's Unread mark). nil leaves a field as
// it was; an event with none changes nothing. Journaled: a client.counts
// answer requested before it keeps the event's (SetCountsSince), even when
// the event changed no value. It reports whether a value changed.
func (s *Store) SetThreadState(hasUnreads *bool, mentions *int, byChannel map[string]int) (changed bool) {
	if hasUnreads == nil && mentions == nil && byChannel == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if hasUnreads != nil && s.ThreadsUnread != *hasUnreads {
		s.ThreadsUnread, changed = *hasUnreads, true
	}
	if mentions != nil && s.ThreadMentions != *mentions {
		s.ThreadMentions, changed = *mentions, true
	}
	if byChannel != nil && !maps.Equal(s.ThreadsUnreadByChannel, byChannel) {
		s.ThreadsUnreadByChannel, changed = byChannel, true
	}
	s.jr.threads = s.jr.seq.Add(1)
	return changed
}

// SetThreadsStateSince is the Threads view's answer's state (getView's
// has_unreads and mention_count) for a request that went out at Seq since:
// an update_thread_state taken after that is newer and stays. It reports
// whether the answer's state went in.
func (s *Store) SetThreadsStateSince(hasUnreads bool, mentions int, since uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.jr.threads > since {
		return false
	}
	s.ThreadsUnread, s.ThreadMentions = hasUnreads, mentions
	return true
}

// dmUnreadCount is client-boot mtwS applyCountsToChannels' rule for an IM
// or a group DM in a client.counts answer: its unread count is
// mention_count || has_unreads, and that count is an unmuted one's badge
// (fgSa shouldUseUnreadCountAsMentionCount, 9dui
// refreshFilteredUnreadChannelCounts). So an answer with has_unreads and
// mention_count 0 is one unread message with a badge of 1, not a bold
// row with none. A muted one keeps the answer's mention count: Slack
// counts no unreads for it (fgSa shouldCountUnreadsInModel), and a muted
// group DM badges on its mentions alone here (NoteMessage).
func dmUnreadCount(e slack.CountEntry, muted bool) slack.CountEntry {
	if e.HasUnreads && e.MentionCount == 0 && !muted {
		e.MentionCount = 1
	}
	return e
}

// PutConvLocal is PutConv for a conversation this session changed
// (joined, created, opened, renamed, closed): a list fetched before the
// change doesn't put its older copy back.
func (s *Store) PutConvLocal(c slack.Conversation) {
	s.PutConv(c)
	s.mu.Lock()
	s.noteConvLocked(c.ID)
	s.mu.Unlock()
}

// DropConv removes a conversation the viewer is no longer in (left,
// archived, deleted, converted) and returns what the store held; a list
// fetched before the change doesn't bring it back.
func (s *Store) DropConv(id string) (*slack.Conversation, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.Convs[id]
	delete(s.Convs, id)
	s.noteConvLocked(id)
	return c, ok
}

// ConvChangedSince reports whether this session changed conversation id
// (PutConvLocal, DropConv) after since.
func (s *Store) ConvChangedSince(id string, since uint64) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.jr.convs[id] > since
}

// PutConvsSince installs a users.conversations answer fetched at journal
// position since: each conversation is put (PutConv), except the ones
// this session changed after since, which stay as they are (a channel
// left meanwhile stays left). One critical section: a leave can't land
// between a conversation's check and its put. It reports how many it
// skipped.
func (s *Store) PutConvsSince(cs []slack.Conversation, since uint64) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	skip := 0
	for i := range cs {
		if s.jr.convs[cs[i].ID] > since {
			skip++
			continue
		}
		s.putConvLocked(cs[i])
	}
	return skip
}

// PutInfoSince stores a conversations.info answer for a lookup asked for at
// journal position since (core's lookups queued off the event pump), in the
// member set when it is there already or the answer says the viewer is in it
// (an IM, a group DM), else outside it (PutExtConv), unless this session
// changed the conversation after since (DropConv, PutConvLocal): read before
// a channel_left that the pump applied meanwhile, the answer put the left
// channel back in the member set. One critical section, as PutConvsSince. It
// reports whether it stored the answer.
func (s *Store) PutInfoSince(c slack.Conversation, since uint64) bool {
	if c.ID == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.jr.convs[c.ID] > since {
		return false
	}
	if _, member := s.Convs[c.ID]; member || c.IsMember || c.IsIM || c.IsMPIM {
		s.putConvLocked(c)
		return true
	}
	s.putExtConvLocked(c)
	return true
}

// SetSections installs a section list (a fresh users.channelSections.list,
// realtime's) and moves the journal.
func (s *Store) SetSections(secs []slack.Section) {
	s.SetSectionsSince(secs, math.MaxUint64)
}

// SetSectionsSince installs a section list fetched at journal position
// since, unless the list changed after since (a move's own refresh, which
// is newer, or a local edit): then it keeps the store's and reports false.
func (s *Store) SetSectionsSince(secs []slack.Section, since uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if since != math.MaxUint64 && s.jr.sections > since {
		return false
	}
	s.Sections = secs
	s.jr.sections = s.jr.seq.Add(1)
	return true
}

// NoteSectionsChanged moves the journal for a section change made in place
// or sent to Slack before its refresh lands (a bulkUpdate, a new channel
// filed into a section): an older list doesn't replace it.
func (s *Store) NoteSectionsChanged() {
	s.mu.Lock()
	s.jr.sections = s.jr.seq.Add(1)
	s.mu.Unlock()
}
