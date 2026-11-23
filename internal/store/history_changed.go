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
// nothing of it from Slack is left, else the oldest message it keeps.
//
// What only this client has stays, as the cache budget never evicts it
// (shouldPersistMessage, evict.go's persistable): the viewer's sends not
// confirmed yet, pending or failed (mine: their ts, which core tracks),
// and ephemeral messages. None comes back from a read of the history, so
// a failed send, its text and its Retry would be gone for good. A
// timeline left with only those is Whole all the same: core reads the
// newest page again, and the read carries them (SetLatestReadAt).

// localMsg reports whether m is only this client's: a send of the
// viewer's not confirmed yet (mine) or an ephemeral message.
func localMsg(m *slack.Message, mine map[string]bool) bool {
	return m.IsEphemeral || mine[m.TS]
}

// HasHistory reports whether ch has history cached (applyCountsToChannels'
// getAllChannelHistory()[id]: the conversation's own key, not its
// threads').
func (s *Store) HasHistory(ch string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.timelines[ch] != nil || len(s.windows[ch]) > 0
}

// TruncateHistoryBefore is truncateChannelHistoryPriorToTimestamp: ch's
// timeline, windows and threads keep only what is at or after latest,
// and what only this client has (mine: the ts of the viewer's sends not
// confirmed yet). It reports each one that lost anything (the timeline
// also when its start was reached: it no longer is).
func (s *Store) TruncateHistoryBefore(ch, latest string, mine map[string]bool) []Evicted {
	if latest == "" {
		return nil
	}
	after := func(ts string) bool { return !TSLess(ts, latest) }
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Evicted
	if t := s.timelines[ch]; t != nil {
		kept, fromSlack := keepMsgs(t.Msgs, func(m *slack.Message) bool { return after(m.TS) }, mine)
		switch {
		case fromSlack == 0 && len(t.Msgs) > 0:
			// no slice left: no end either, the newest page is read again
			s.dropTimelineLocked(ch, kept)
			out = append(out, Evicted{Channel: ch, Whole: true})
		case len(kept) < len(t.Msgs) || !t.HasMore:
			t.Msgs, t.HasMore = kept, true
			t.Ahead, _ = keepMsgs(t.Ahead, func(m *slack.Message) bool { return after(m.TS) }, mine)
			out = append(out, Evicted{Channel: ch, Oldest: oldestFromSlack(kept, mine)})
		}
	}
	if ws := s.windows[ch]; len(ws) > 0 {
		var kept []*Window
		for _, w := range ws {
			if TSLess(w.End, latest) {
				continue // the slice ends before it
			}
			if TSLess(w.Start, latest) {
				w.Msgs, _ = keepMsgs(w.Msgs, func(m *slack.Message) bool { return after(m.TS) }, mine)
				w.Start, w.HasOlder = latest, true
			}
			kept = append(kept, w)
		}
		s.setWindowsLocked(ch, kept)
	}
	for _, k := range s.threadKeysLocked(ch) {
		if e, ok := s.cutThreadLocked(ch, k, latest, mine); ok {
			out = append(out, e)
		}
	}
	return out
}

// DropHistory is removeAllTimestamps({includeThreads: true}): ch's
// timeline, windows and threads go, all but what only this client has
// (mine, as TruncateHistoryBefore's). It reports each one that went.
func (s *Store) DropHistory(ch string, mine map[string]bool) []Evicted {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Evicted
	if t := s.timelines[ch]; t != nil || len(s.windows[ch]) > 0 {
		out = append(out, Evicted{Channel: ch, Whole: true})
		var kept []slack.Message
		if t != nil {
			kept, _ = keepMsgs(t.Msgs, func(*slack.Message) bool { return false }, mine)
		}
		s.dropTimelineLocked(ch, kept)
	}
	delete(s.windows, ch)
	for _, k := range s.threadKeysLocked(ch) {
		if e, ok := s.cutThreadLocked(ch, k, "", mine); ok {
			out = append(out, e)
		}
	}
	return out
}

// dropTimelineLocked lets ch's timeline go, but for kept (what only this
// client has): a timeline of those alone, its start not reached, which
// core reads the newest page into.
func (s *Store) dropTimelineLocked(ch string, kept []slack.Message) {
	if len(kept) == 0 {
		delete(s.timelines, ch)
		return
	}
	t := s.timelines[ch]
	t.Msgs, t.HasMore, t.Ahead = kept, true, nil
	t.EndStale, t.End = false, ""
}

// cutThreadLocked keeps thread key k's replies at or after latest (""
// none), and those only this client has (mine, ephemeral): their slices
// as truncateThreadHistLocked cuts them. It reports what the thread lost.
func (s *Store) cutThreadLocked(ch, k, latest string, mine map[string]bool) (Evicted, bool) {
	keep := func(ts string) bool { return latest != "" && !TSLess(ts, latest) }
	_, thread, _ := strings.Cut(k, "/")
	th := s.threads[k]
	local := map[string]bool{}
	for i := range th {
		if localMsg(&th[i], mine) {
			local[th[i].TS] = true
		}
	}
	a := s.threadHist[k]
	s.truncateThreadHistLocked(k, latest, local)
	left, fromSlack := keepMsgs(th, func(m *slack.Message) bool { return keep(m.TS) }, mine)
	if fromSlack == 0 && len(left) == 0 {
		if len(th) == 0 && a == nil {
			return Evicted{}, false
		}
		delete(s.threads, k)
		s.threadHistDropLocked(k)
		return Evicted{Channel: ch, Thread: thread, Whole: true}, true
	}
	if len(left) == len(th) {
		return Evicted{}, false
	}
	s.threads[k] = left
	if fromSlack == 0 {
		return Evicted{Channel: ch, Thread: thread, Whole: true}, true
	}
	return Evicted{Channel: ch, Thread: thread, Oldest: oldestFromSlack(left, mine)}, true
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
// one thread's slices ("" latest: removeAllTimestamps): those ending
// before latest go, one crossing it starts at it (createSlice of the
// timestamps left, from latest to its end), the start is no longer
// reached, and with no slice left there is no end either. A timestamp in
// local before latest (a reply only this client has) stays, in a slice
// of its own.
func (s *Store) truncateThreadHistLocked(k, latest string, local map[string]bool) {
	a := s.threadHist[k]
	if a == nil || len(a.slices) == 0 {
		return
	}
	keep := func(ts string) bool { return latest != "" && !TSLess(ts, latest) }
	var out []tslice
	var orphans []string
	for _, e := range a.slices {
		var tss []string
		for _, t := range e.tss {
			switch {
			case keep(t):
				tss = append(tss, t)
			case local[t]:
				orphans = append(orphans, t)
			}
		}
		switch {
		case keep(e.start):
			out = append(out, e)
		case keep(e.end):
			out = append(out, createSlice(tss, latest, e.end))
		}
	}
	if len(orphans) > 0 {
		out = append(out, createSlice(orphans, "", ""))
	}
	a.slices, a.reachedStart = sortSlices(out), tFalse
	if len(out) == 0 {
		a.reachedEnd = tUnset
	}
}

// sortSlices is ss by start.
func sortSlices(ss []tslice) []tslice {
	for i := 1; i < len(ss); i++ {
		for j := i; j > 0 && TSLess(ss[j].start, ss[j-1].start); j-- {
			ss[j], ss[j-1] = ss[j-1], ss[j]
		}
	}
	return ss
}

// keepMsgs is msgs' messages keep holds for and those only this client
// has (mine, ephemeral), in a new array, and how many of them are from
// Slack.
func keepMsgs(msgs []slack.Message, keep func(m *slack.Message) bool, mine map[string]bool) (out []slack.Message, fromSlack int) {
	for i := range msgs {
		m := &msgs[i]
		switch {
		case localMsg(m, mine):
			out = append(out, *m)
		case keep(m):
			out = append(out, *m)
			fromSlack++
		}
	}
	return out, fromSlack
}

// oldestFromSlack is the oldest of msgs from Slack (not only this
// client's), "" for none.
func oldestFromSlack(msgs []slack.Message, mine map[string]bool) string {
	for i := range msgs {
		if !localMsg(&msgs[i], mine) {
			return msgs[i].TS
		}
	}
	return ""
}
