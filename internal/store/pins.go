package store

import (
	"slices"
	"strings"

	"github.com/some-du6e/clack/internal/slack"
)

// Pin state from pin_added / pin_removed, kept per message.
//
// Slack's pinAdded / pinRemoved RTM handlers (boot-render) run
// upsertEntityPinnedStatus, whose upsertMessagePinnedStatus (boot-shared
// 1dqP) writes the event's item.message, with pinned_to (DtqA) and
// pinned_info (extendMessageWithPinnedInfo), into the messages store "but
// not the channel-history-store" (fB9+ upsertRawMessages). It does that
// whether or not the message or its conversation is loaded. A copy that
// arrives on the socket later is merged into that entry (appendMessages's
// "Add messages", client-boot z+/K processAndMergeMessages: {...old,
// ...new}; makeMessageObject copies pinned_to and pinned_info only when the
// message has them), so the pin stays. Clack keeps no messages store apart
// from its timelines and threads, so a pin event waits here for its message.
//
// A history or replies fetch is Slack's "Replace messages" (Slack's copy
// wins), except for a pin event that came while the request was out:
// that event is newer than the answer (SyncPins).

type pinRec struct {
	pinned bool
	info   *slack.PinnedInfo
	seq    uint64
}

// apply is DtqA on m's pinned_to (ch added or removed) and
// extendMessageWithPinnedInfo on its pinned_info.
func (r pinRec) apply(ch string, m *slack.Message) {
	i := slices.Index(m.PinnedTo, ch)
	switch {
	case r.pinned && i < 0:
		m.PinnedTo = append(slices.Clip(m.PinnedTo), ch)
	case !r.pinned && i >= 0:
		m.PinnedTo = slices.Delete(slices.Clone(m.PinnedTo), i, i+1)
	}
	if len(m.PinnedTo) == 0 {
		m.PinnedTo = nil
	}
	m.PinnedInfo = nil
	if r.pinned {
		m.PinnedInfo = r.info
	}
}

// maxPinRecs bounds the remembered pin events.
const maxPinRecs = 1024

// PinSeq is the number of pin events seen so far; a fetch reads it before
// its request and passes it to SyncPins.
func (s *Store) PinSeq() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.pinSeq
}

// SetPin records a pin_added (pinned) or pin_removed for ch/ts and applies
// it to the cached copies (timeline and threads). It returns the updated
// message when one was cached.
func (s *Store) SetPin(ch, ts string, pinned bool, info *slack.PinnedInfo) (slack.Message, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pinSeq++
	r := pinRec{pinned: pinned, info: info, seq: s.pinSeq}
	if s.pins == nil {
		s.pins = map[string]pinRec{}
	}
	k := ch + "/" + ts
	if _, ok := s.pins[k]; !ok {
		s.pinQ = append(s.pinQ, k)
		for len(s.pinQ) > maxPinRecs {
			delete(s.pins, s.pinQ[0])
			s.pinQ = s.pinQ[1:]
		}
	}
	s.pins[k] = r
	return s.mutateLocked(ch, ts, func(m *slack.Message) { r.apply(ch, m) })
}

// keepPinLocked is processAndMergeMessages' {...old, ...new} for the pin
// fields of an incoming socket copy m: a field m lacks keeps the stored
// copy's value, or, with nothing stored, a pin event's that came first.
func (s *Store) keepPinLocked(ch string, m *slack.Message, old *slack.Message) {
	if m.PinnedTo != nil && m.PinnedInfo != nil {
		return
	}
	var prev slack.Message
	switch r, ok := s.pins[ch+"/"+m.TS]; {
	case old != nil:
		prev = *old
	case ok:
		r.apply(ch, &prev)
	default:
		return
	}
	if m.PinnedTo == nil {
		m.PinnedTo = prev.PinnedTo
	}
	if m.PinnedInfo == nil {
		m.PinnedInfo = prev.PinnedInfo
	}
}

// KeepPin is keepPinLocked for a socket copy m before it is stored: its
// missing pin fields from the cached copy (timeline, else a thread) or an
// earlier pin event, so the events built from m carry them too.
func (s *Store) KeepPin(ch string, m *slack.Message) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var old *slack.Message
	if t := s.timelines[ch]; t != nil {
		for i := range t.Msgs {
			if t.Msgs[i].TS == m.TS {
				old = &t.Msgs[i]
			}
		}
	}
	if old == nil {
		for k, th := range s.threads {
			if strings.HasPrefix(k, ch+"/") {
				for i := range th {
					if th[i].TS == m.TS {
						old = &th[i]
					}
				}
			}
		}
	}
	s.keepPinLocked(ch, m, old)
}

// SyncPins runs after a history or replies fetch of ch whose request
// began at PinSeq since and answered fetched (now cached). A pin event
// that came meanwhile is newer than the answer and is applied over it, in
// the cache and in fetched; an older one is settled by the answer (Slack's
// copy is the truth) and forgotten for the messages it carried.
func (s *Store) SyncPins(ch string, since uint64, fetched []slack.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.pins) == 0 {
		return
	}
	got := make(map[string]int, len(fetched))
	for i := range fetched {
		got[fetched[i].TS] = i + 1
	}
	prefix := ch + "/"
	dropped := false
	for k, r := range s.pins {
		ts, ok := strings.CutPrefix(k, prefix)
		if !ok {
			continue
		}
		switch {
		case r.seq > since:
			s.mutateLocked(ch, ts, func(m *slack.Message) { r.apply(ch, m) })
			if i := got[ts]; i > 0 {
				r.apply(ch, &fetched[i-1])
			}
		case got[ts] > 0:
			delete(s.pins, k)
			dropped = true
		}
	}
	if dropped {
		q := make([]string, 0, len(s.pins))
		seen := make(map[string]bool, len(s.pins))
		for _, k := range s.pinQ {
			if _, ok := s.pins[k]; ok && !seen[k] {
				seen[k] = true
				q = append(q, k)
			}
		}
		s.pinQ = q
	}
}
