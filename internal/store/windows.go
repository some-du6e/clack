package store

import (
	"sort"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// A conversation's history beyond its newest window. Slack's
// channelHistory keeps what a channel loaded as slices (client-boot 1qrw
// addTimestamps, invH createSlice/intersects/union/insert, build
// 2c0d02ca130f): a jump to a message far back loads the window around it
// as a slice of its own, older pages and newer ones extend that slice, and
// two slices become one only once they meet. Clack's newest slice is the
// timeline; the others are the conversation's windows, each older than
// the timeline and apart from it and from the others. Find, Mutate and
// Remove reach a window's messages, so a reaction, an edit, a reply count
// or a pin on one updates it there as on the timeline; a new message
// never joins a window (it's newer than the timeline's start), and
// paging inside a window never writes the timeline until the two meet.

// Window is a stretch of a conversation's history apart from its
// timeline: a slice of Slack's channelHistory.
type Window struct {
	Msgs []slack.Message // ascending
	// Start and End are the range the window holds whole (inclusive):
	// every message of the conversation between them is in Msgs, the
	// requests' own bounds (createSlice(timestamps, oldest, latest))
	Start, End string
	// HasOlder: messages exist before Start (the slice isn't the
	// channel's reachedStart one)
	HasOlder bool
	used     time.Time
}

const (
	maxWindows   = 8    // windows kept in all conversations (the least recently used go)
	maxWindowLen = 2000 // a window's messages (the oldest go)
)

// AddWindow stores a stretch of ch's history: msgs, every message of the
// conversation from start to end ("": its first or last message), older
// ones existing when hasOlder and newer ones when hasNewer, read by
// requests begun at LiveSeq since. As the timeline's reads (SetLatestRead,
// PrependOlderSince) a message the socket changed meanwhile keeps the
// socket's copy, one it deleted stays out (Slack's addMessagesFromHistory
// drops what was deleted locally), the reactions it brought are replayed
// and a newer revision reaches every copy (readWindowLocked). As Slack's
// insert it unions with every window whose range meets its own, and with
// the timeline when it meets the timeline's range or reaches the newest
// message (hasNewer false; without a timeline it then is the timeline).
// merged reports that it joined the timeline (w then holds only what the
// read kept of msgs); w is the window it's part of otherwise.
func (s *Store) AddWindow(ch string, msgs []slack.Message, start, end string, hasOlder, hasNewer bool, since uint64) (w Window, merged bool) {
	msgs = dedupe(append([]slack.Message(nil), msgs...))
	if len(msgs) > 0 { // the range the answers cover, a message deleted since included
		if start == "" || TSLess(msgs[0].TS, start) {
			start = msgs[0].TS
		}
		if end == "" || TSLess(end, msgs[len(msgs)-1].TS) {
			end = msgs[len(msgs)-1].TS
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	msgs = s.liveKeepLocked(ch, msgs, since)
	s.readWindowLocked(ch, msgs, since)
	if len(msgs) == 0 && hasNewer {
		return Window{Start: start, End: end, HasOlder: hasOlder}, false // nothing there to keep
	}
	nw := &Window{Msgs: msgs, Start: start, End: end, HasOlder: hasOlder, used: time.Now()}
	var keep []*Window
	for _, x := range s.windows[ch] {
		if rangesMeet(x.Start, x.End, nw.Start, nw.End) {
			nw = unionWindows(nw, x)
			continue
		}
		keep = append(keep, x)
	}
	t := s.timelines[ch]
	if !hasNewer || t != nil && t.Loaded && timelineMeets(t, nw.End) {
		s.joinTimelineLocked(ch, nw)
		s.setWindowsLocked(ch, keep)
		return Window{Msgs: append([]slack.Message(nil), msgs...)}, true // what the read kept
	}
	keep = append(keep, nw)
	s.setWindowsLocked(ch, keep)
	s.capWindowsLocked()
	return copyWindow(nw), false
}

// WindowAt is the window of ch whose range holds ts.
func (s *Store) WindowAt(ch, ts string) (Window, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, x := range s.windows[ch] {
		if !TSLess(ts, x.Start) && !TSLess(x.End, ts) {
			x.used = time.Now()
			return copyWindow(x), true
		}
	}
	return Window{}, false
}

// TimelineHolds reports whether ch's timeline covers ts: it's loaded and
// ts isn't older than its first message (or it holds the whole history).
func (s *Store) TimelineHolds(ch, ts string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t := s.timelines[ch]
	return t != nil && t.Loaded && timelineMeets(t, ts)
}

// timelineMeets reports whether ts is in the timeline's range: from its
// first message on (every message after it is the timeline's, the newest
// window), the whole history when it has nothing older.
func timelineMeets(t *Timeline, ts string) bool {
	if !t.HasMore || len(t.Msgs) == 0 {
		return !t.HasMore
	}
	return !TSLess(ts, t.Msgs[0].TS)
}

// rangesMeet reports two inclusive ranges that share a timestamp.
func rangesMeet(aStart, aEnd, bStart, bEnd string) bool {
	return !TSLess(aEnd, bStart) && !TSLess(bEnd, aStart)
}

// unionWindows is Slack's union: the messages of both, the wider range,
// and what's older than the earlier start.
func unionWindows(a, b *Window) *Window {
	w := &Window{Msgs: dedupe(append(append([]slack.Message(nil), a.Msgs...), b.Msgs...)), Start: a.Start, End: a.End,
		HasOlder: a.HasOlder, used: time.Now()}
	if TSLess(b.Start, a.Start) {
		w.Start, w.HasOlder = b.Start, b.HasOlder
	}
	if TSLess(w.End, b.End) {
		w.End = b.End
	}
	return w
}

// joinTimelineLocked unions window w into ch's timeline (it met it, or
// reached the newest message).
func (s *Store) joinTimelineLocked(ch string, w *Window) {
	t := s.timelines[ch]
	if t == nil {
		// the newest messages' window, as a newest page's read makes one
		// (SetLatestReadAt): a window of its own (Epoch), its end as of
		// the resets so far (EndGen)
		s.timelines[ch] = &Timeline{Msgs: w.Msgs, HasMore: w.HasOlder, Loaded: true, EndGen: s.endGen, Epoch: s.newEpochLocked()}
		return
	}
	earlier := len(t.Msgs) == 0 || len(w.Msgs) > 0 && TSLess(w.Msgs[0].TS, t.Msgs[0].TS)
	t.Msgs = dedupe(append(append([]slack.Message(nil), w.Msgs...), t.Msgs...))
	if earlier {
		t.HasMore = w.HasOlder
	}
	t.Loaded = true
	if len(t.Msgs) > 5000 { // PrependOlder's bound
		t.Msgs, t.HasMore = t.Msgs[len(t.Msgs)-5000:], true
	}
}

// absorbWindowsLocked joins the windows of ch that meet its timeline's
// range now (it grew older, or it was replaced) into it.
func (s *Store) absorbWindowsLocked(ch string) {
	t := s.timelines[ch]
	if t == nil || len(s.windows[ch]) == 0 {
		return
	}
	var keep []*Window
	for _, x := range s.windows[ch] {
		if timelineMeets(t, x.End) {
			s.joinTimelineLocked(ch, x)
			continue
		}
		keep = append(keep, x)
	}
	s.setWindowsLocked(ch, keep)
}

func (s *Store) setWindowsLocked(ch string, ws []*Window) {
	if len(ws) == 0 {
		delete(s.windows, ch)
		return
	}
	sort.Slice(ws, func(i, j int) bool { return TSLess(ws[i].Start, ws[j].Start) })
	for _, w := range ws {
		if n := len(w.Msgs); n > maxWindowLen { // truncateChannelHistory's oldest-first drop
			w.Msgs = w.Msgs[n-maxWindowLen:]
			w.Start, w.HasOlder = w.Msgs[0].TS, true
		}
	}
	if s.windows == nil {
		s.windows = map[string][]*Window{}
	}
	s.windows[ch] = ws
}

// capWindowsLocked keeps the maxWindows windows used last, and every
// window a list holds (HoldWindow) whatever their number.
func (s *Store) capWindowsLocked() {
	for {
		n := 0
		var oldCh string
		var old *Window
		for ch, ws := range s.windows {
			for _, w := range ws {
				n++
				if !s.heldWindowLocked(ch, w) && (old == nil || w.used.Before(old.used)) {
					oldCh, old = ch, w
				}
			}
		}
		if n <= maxWindows || old == nil {
			return
		}
		var keep []*Window
		for _, w := range s.windows[oldCh] {
			if w != old {
				keep = append(keep, w)
			}
		}
		s.setWindowsLocked(oldCh, keep)
	}
}

func copyWindow(w *Window) Window {
	cp := *w
	cp.Msgs = append([]slack.Message(nil), w.Msgs...)
	return cp
}

// windowMsgLocked is message ts of ch in one of its windows.
func (s *Store) windowMsgLocked(ch string, match func(*slack.Message) bool) *slack.Message {
	for _, w := range s.windows[ch] {
		for i := range w.Msgs {
			if match(&w.Msgs[i]) {
				return &w.Msgs[i]
			}
		}
	}
	return nil
}

// removeWindowMsgLocked takes message ts out of ch's windows.
func (s *Store) removeWindowMsgLocked(ch, ts string) {
	for _, w := range s.windows[ch] {
		for i := range w.Msgs {
			if w.Msgs[i].TS == ts {
				w.Msgs = append(w.Msgs[:i], w.Msgs[i+1:]...)
				break
			}
		}
	}
}

// HoldWindow keeps the window of ch that holds ts (or will: a jump's
// window still loading) while a list shows it: the cap on windows never
// drops it, nor does an eviction of the conversation's history (Slack's
// slices stay until their own channel's truncateChannelHistory; jumps in
// other conversations never drop one). release lets it go; it's safe to
// call more than once.
func (s *Store) HoldWindow(ch, ts string) (release func()) {
	s.mu.Lock()
	if s.windowHolds == nil {
		s.windowHolds = map[string]map[string]int{}
	}
	if s.windowHolds[ch] == nil {
		s.windowHolds[ch] = map[string]int{}
	}
	s.windowHolds[ch][ts]++
	s.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			if hs := s.windowHolds[ch]; hs != nil {
				if hs[ts]--; hs[ts] <= 0 {
					delete(hs, ts)
				}
				if len(hs) == 0 {
					delete(s.windowHolds, ch)
				}
			}
		})
	}
}

// heldWindowLocked reports whether a list holds window w of ch.
func (s *Store) heldWindowLocked(ch string, w *Window) bool {
	for ts := range s.windowHolds[ch] {
		if !TSLess(ts, w.Start) && !TSLess(w.End, ts) {
			return true
		}
	}
	return false
}

// keepHeldWindowsLocked drops ch's windows but the ones a list holds (its
// history started over).
func (s *Store) keepHeldWindowsLocked(ch string) {
	var keep []*Window
	for _, w := range s.windows[ch] {
		if s.heldWindowLocked(ch, w) {
			keep = append(keep, w)
		}
	}
	s.setWindowsLocked(ch, keep)
}

// DropWindows forgets every window of ch (its history went: evicted whole,
// left, signed out).
func (s *Store) DropWindows(ch string) {
	s.mu.Lock()
	delete(s.windows, ch)
	s.mu.Unlock()
}
