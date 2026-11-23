package store

import (
	"strings"

	"github.com/some-du6e/clack/internal/slack"
)

// History rewritten on Slack's side (core/history_changed.go has the
// events): a bulk delete, a retention pass, an import. Slack's client
// drops what it cached of the conversation and reads it again as the
// views ask, through two of channelHistory's actions (client-boot 6Yed):
//
//   - truncateChannelHistoryPriorToTimestamp (fp), at a
//     channel_history_changed: of the conversation's slices and every
//     thread's (the `${channelId}-` keys) only what is at or after the
//     event's latest stays. A slice that ends before it goes, one that
//     crosses it starts at it (createSlice of the timestamps left, from
//     latest to its end), the history no longer reaches the start
//     (reachedStart false), and one left with no slice has no end either
//     (reachedEnd null).
//   - removeAllTimestamps({includeThreads: true}) (bz), when client.counts'
//     history_invalid is newer than the one the client applied: the
//     conversation's history and every thread's go.
//
// Clack's conversation history is its timeline (the newest slice) and its
// windows (the others, windows.go); a thread's is its threadHist slices
// and the replies in threads. Both report what went as Evicted, the
// cache budget's record (evict.go): a conversation or thread Whole when
// nothing of it is left, else the oldest message it keeps.

// HasHistory reports whether ch has history cached (applyCountsToChannels'
// getAllChannelHistory()[id]: the conversation's own key, not its
// threads').
func (s *Store) HasHistory(ch string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.timelines[ch] != nil || len(s.windows[ch]) > 0
}

// TruncateHistoryBefore is truncateChannelHistoryPriorToTimestamp: ch's
// timeline, windows and threads keep only what is at or after latest.
// It reports each one that lost anything (the timeline also when its
// start was reached: it no longer is).
func (s *Store) TruncateHistoryBefore(ch, latest string) []Evicted {
	if latest == "" {
		return nil
	}
	keep := func(ts string) bool { return !TSLess(ts, latest) }
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Evicted
	if t := s.timelines[ch]; t != nil {
		msgs := keepMsgs(t.Msgs, keep)
		switch {
		case len(msgs) == 0 && len(t.Msgs) > 0:
			// no slice left: no end either, the newest page is read again
			delete(s.timelines, ch)
			out = append(out, Evicted{Channel: ch, Whole: true})
		case len(msgs) < len(t.Msgs) || !t.HasMore:
			t.Msgs, t.HasMore = msgs, true
			t.Ahead = keepMsgs(t.Ahead, keep)
			oldest := ""
			if len(msgs) > 0 {
				oldest = msgs[0].TS
			}
			out = append(out, Evicted{Channel: ch, Oldest: oldest})
		}
	}
	if ws := s.windows[ch]; len(ws) > 0 {
		var kept []*Window
		for _, w := range ws {
			if TSLess(w.End, latest) {
				continue // the slice ends before it
			}
			if TSLess(w.Start, latest) {
				w.Msgs = keepMsgs(w.Msgs, keep)
				w.Start, w.HasOlder = latest, true
			}
			kept = append(kept, w)
		}
		s.setWindowsLocked(ch, kept)
	}
	for _, k := range s.threadKeysLocked(ch) {
		_, thread, _ := strings.Cut(k, "/")
		s.truncateThreadHistLocked(k, latest)
		th := s.threads[k]
		left := keepMsgs(th, keep)
		if a := s.threadHist[k]; len(left) == 0 && (a == nil || len(a.slices) == 0) {
			if len(th) > 0 || a != nil {
				delete(s.threads, k)
				s.threadHistDropLocked(k)
				out = append(out, Evicted{Channel: ch, Thread: thread, Whole: true})
			}
			continue
		}
		if len(left) < len(th) {
			s.threads[k] = left
			oldest := ""
			if len(left) > 0 {
				oldest = left[0].TS
			}
			out = append(out, Evicted{Channel: ch, Thread: thread, Oldest: oldest})
		}
	}
	return out
}

// DropHistory is removeAllTimestamps({includeThreads: true}): ch's
// timeline, windows and threads go. It reports each one that went.
func (s *Store) DropHistory(ch string) []Evicted {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Evicted
	if s.timelines[ch] != nil || len(s.windows[ch]) > 0 {
		out = append(out, Evicted{Channel: ch, Whole: true})
	}
	delete(s.timelines, ch)
	delete(s.windows, ch)
	for _, k := range s.threadKeysLocked(ch) {
		_, thread, _ := strings.Cut(k, "/")
		delete(s.threads, k)
		s.threadHistDropLocked(k)
		out = append(out, Evicted{Channel: ch, Thread: thread, Whole: true})
	}
	return out
}

// threadKeysLocked is the keys ("ch/ts") of ch's cached threads: their
// replies or their history slices.
func (s *Store) threadKeysLocked(ch string) []string {
	p := ch + "/"
	seen := map[string]bool{}
	var out []string
	for k := range s.threads {
		if strings.HasPrefix(k, p) && !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	for k := range s.threadHist {
		if strings.HasPrefix(k, p) && !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	return out
}

// truncateThreadHistLocked is truncateChannelHistoryPriorToTimestamp on
// one thread's slices: those ending before latest go, one crossing it
// starts at it, the start is no longer reached, and with no slice left
// there is no end either.
func (s *Store) truncateThreadHistLocked(k, latest string) {
	a := s.threadHist[k]
	if a == nil || len(a.slices) == 0 {
		return
	}
	var out []tslice
	for _, e := range a.slices {
		if TSLess(e.end, latest) {
			continue
		}
		if !TSLess(e.start, latest) {
			out = append(out, e)
			continue
		}
		var tss []string
		for _, t := range e.tss {
			if !TSLess(t, latest) {
				tss = append(tss, t)
			}
		}
		out = append(out, createSlice(tss, latest, e.end))
	}
	a.slices, a.reachedStart = out, tFalse
	if len(out) == 0 {
		a.reachedEnd = tUnset
	}
}

// keepMsgs is msgs' messages keep holds for, in a new array.
func keepMsgs(msgs []slack.Message, keep func(ts string) bool) []slack.Message {
	var out []slack.Message
	for i := range msgs {
		if keep(msgs[i].TS) {
			out = append(out, msgs[i])
		}
	}
	return out
}
