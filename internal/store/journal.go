package store

import (
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
// It reports how many entries it merged: their state is partly older
// than Slack's now, so the caller reads the counts once more.
func (s *Store) SetCountsSince(c *slack.Counts, since uint64) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	old := s.Counts
	s.Counts = make(map[string]*slack.CountEntry, len(old))
	merged := 0
	for _, l := range [][]slack.CountEntry{c.Channels, c.IMs, c.MPIMs} {
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
			s.Counts[e.ID] = &e
		}
	}
	for id, o := range old { // changed since, and not in the answer
		if _, ok := s.Counts[id]; !ok && s.countChangedLocked(id, since) {
			s.Counts[id] = o
			merged++
		}
	}
	s.ThreadsUnread = c.Threads.HasUnreads
	s.ThreadMentions = c.Threads.MentionCount
	s.ThreadsUnreadByChannel = c.Threads.UnreadCountByChannel
	s.countsLive = true
	return merged
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
