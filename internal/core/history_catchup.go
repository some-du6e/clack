package core

import (
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// Catching up with what the socket never brought.
//
// Slack's views read the histories it holds, and three things make one
// read Slack again:
//
//   - A connect that wasn't fast (the socket came back without its
//     reconnect_url: on a Mac every wake, as RTM.Reconnect drops it) makes
//     the reachedEnd manager stale, and Slack's onConnected handler
//     (boot-shared) resets every channel's reachedEnd ("Resetting
//     reachedEnd"), the first connect included: a view then fetches the
//     newest page of its channel however many messages the socket brought
//     since. Core keeps, per window and cached thread, the
//     slowConnectionCount it was read in, and History and Replies read a
//     window or thread from an older one again, once (merged through
//     SetLatestSince and SetThreadSince: what the socket brought meanwhile
//     stays). The lists on screen load again at the connection event,
//     which reaches the hub after the count moved; the others when they
//     are next shown.
//   - client.counts' history_invalid and updated
//     (maybePurgeChannelHistoryAfterMutations, from applyCountsToChannels):
//     a history_invalid past the one last applied purges the channel's
//     cached history and threads, an updated past it marks the history
//     stale. The socket's own changes move them first (Slack's
//     maybeAdvanceChannel{HistoryInvalid,Updated}Timestamp, once the
//     counts after a connect are applied), so only a change the socket
//     missed reads anything.
//   - Clack's own: counts naming a latest message newer than the window
//     (windowBehind), for a latest that moved after the window was read.
//
// A catch-up that fails is tried again after catchUpRetry, at the next
// read.

// catchUpRetry is how long a failed catch-up of a window or thread waits
// before a read tries it again (a var for the tests).
var catchUpRetry = 5 * time.Second

// zeroTS is the ts Slack's counts send for "never".
const zeroTS = "0000000000.000000"

// histReads is what core knows of when its windows (key: the
// conversation) and cached threads (key: conversation/thread ts) were
// read from Slack, and of the counts' mutation timestamps.
type histReads struct {
	mu sync.Mutex
	// gen: the slowConnectionCount the key was last read in
	gen map[string]int64
	// latest: the conversation's latest message as the counts knew it
	// when its newest page was last read (windowBehind)
	latest map[string]string
	// stale: the seq of the mark the counts' updated made (a read that
	// began after it clears it)
	stale map[string]uint64
	seq   uint64
	// retry: a failed catch-up's earliest next try
	retry map[string]time.Time
	// muts: channelMutationTimestamps, as the counts and socket left them
	muts map[string]mutTimes
	// countsApplied is the socket store's countsAreApplied: counts were
	// applied since the socket's last connect
	countsApplied bool
}

type mutTimes struct{ historyInvalid, updated string }

// readMark is what a read of a window or thread records as it begins.
type readMark struct {
	gen int64
	seq uint64
}

func (h *histReads) init() {
	if h.gen == nil {
		h.gen, h.latest, h.stale = map[string]int64{}, map[string]string{}, map[string]uint64{}
		h.retry, h.muts = map[string]time.Time{}, map[string]mutTimes{}
	}
}

// readStarts is taken as a read of a window or thread starts.
func (a *App) readStarts() readMark {
	h := &a.hist
	h.mu.Lock()
	defer h.mu.Unlock()
	return readMark{gen: a.slowConnectionCount().Load(), seq: h.seq}
}

// noteRead records that key was read from Slack, the read having begun
// at rm.
func (a *App) noteRead(key string, rm readMark) {
	h := &a.hist
	h.mu.Lock()
	defer h.mu.Unlock()
	h.init()
	if g, ok := h.gen[key]; !ok || g < rm.gen {
		h.gen[key] = rm.gen
	}
	if s, ok := h.stale[key]; ok && s <= rm.seq {
		delete(h.stale, key)
	}
	delete(h.retry, key)
}

// noteWindowRead is noteRead for ch's newest page, read when the counts
// knew latest as ch's latest message.
func (a *App) noteWindowRead(ch string, rm readMark, latest string) {
	a.noteRead(ch, rm)
	h := &a.hist
	h.mu.Lock()
	h.latest[ch] = latest
	h.mu.Unlock()
}

// readFailed holds back key's next catch-up for catchUpRetry.
func (a *App) readFailed(key string) {
	h := &a.hist
	h.mu.Lock()
	defer h.mu.Unlock()
	h.init()
	h.retry[key] = time.Now().Add(catchUpRetry)
}

// mustReread reports whether key's cached window or thread is to be read
// from Slack again before it shows: it was read before the last slow
// connect, or the counts marked it stale. One core didn't read itself
// (put in the store directly) counts as read now. h.mu is held.
func (a *App) mustRereadLocked(key string) bool {
	h := &a.hist
	h.init()
	if t, ok := h.retry[key]; ok && time.Now().Before(t) {
		return false
	}
	if _, ok := h.stale[key]; ok {
		return true
	}
	cur := a.slowConnectionCount().Load()
	g, ok := h.gen[key]
	if !ok {
		h.gen[key] = cur
		return false
	}
	return g < cur
}

// threadBehind reports whether the cached thread ch/ts is to be read
// again before it shows.
func (a *App) threadBehind(ch, ts string) bool {
	h := &a.hist
	h.mu.Lock()
	defer h.mu.Unlock()
	return a.mustRereadLocked(ch + "/" + ts)
}

// windowBehind reports whether t, the newest window core holds for ch, is
// to be read again before it shows: mustReread's reasons, or it ends
// before ch's latest message as the counts know it (client.counts, and
// every top-level message the socket brought: store.NoteMessage) when that
// latest moved after the window was read. The latest check runs once per
// latest: a latest no page shows (a message the history leaves out) costs
// one read, not one per load, and a window core didn't read itself counts
// as current for the latest known when it is first checked.
func (a *App) windowBehind(ch string, t *store.Timeline) bool {
	e, _ := a.S.Count(ch)
	h := &a.hist
	h.mu.Lock()
	defer h.mu.Unlock()
	h.init()
	if at, ok := h.retry[ch]; ok && time.Now().Before(at) {
		return false
	}
	if a.mustRereadLocked(ch) {
		return true
	}
	if e.Latest == "" || e.Latest == zeroTS {
		return false
	}
	if n := len(t.Msgs); n > 0 && !store.TSLess(t.Msgs[n-1].TS, e.Latest) {
		return false
	}
	seen, ok := h.latest[ch]
	if !ok {
		h.latest[ch] = e.Latest
		return false
	}
	return seen != e.Latest
}

// applyMutationTimes is applyCountsToChannels' part for the channel
// mutation timestamps: for a conversation core holds a window of, a
// history_invalid past the one last applied purges its history and
// threads, else an updated past it marks the history stale
// (maybePurgeChannelHistoryAfterMutations); then the answer's timestamps
// are kept (bulkAddOrUpdateChannelMutationTimestamps). Only entries with
// both set count, as in Slack.
func (a *App) applyMutationTimes(c *slack.Counts) {
	type change struct {
		ch    string
		purge bool
	}
	var changes []change
	connected := a.RTM != nil && a.RTM.Connected()
	h := &a.hist
	h.mu.Lock()
	h.init()
	for _, list := range [][]slack.CountEntry{c.Channels, c.MPIMs, c.IMs} {
		for _, e := range list {
			if e.Updated == "" || e.HistoryInvalid == "" {
				continue
			}
			old := h.muts[e.ID]
			if a.S.Timeline(e.ID) != nil {
				switch {
				case e.HistoryInvalid != zeroTS && (old.historyInvalid == "" || store.TSLess(old.historyInvalid, e.HistoryInvalid)):
					changes = append(changes, change{e.ID, true})
				case e.Updated != zeroTS && (old.updated == "" || store.TSLess(old.updated, e.Updated)):
					h.seq++
					h.stale[e.ID] = h.seq
					delete(h.retry, e.ID)
					changes = append(changes, change{e.ID, false})
				}
			}
			h.muts[e.ID] = mutTimes{historyInvalid: e.HistoryInvalid, updated: e.Updated}
		}
	}
	if connected {
		h.countsApplied = true
	}
	h.mu.Unlock()
	for _, x := range changes {
		if x.purge {
			a.purgeHistory(x.ch)
		} else {
			a.emit(Event{Type: "history_stale", Channel: x.ch})
		}
	}
}

// purgeHistory is purgeChannelHistory with its threads: ch's window and
// cached threads go, and the next read fetches them again.
func (a *App) purgeHistory(ch string) {
	threads := a.S.PurgeHistory(ch)
	h := &a.hist
	h.mu.Lock()
	delete(h.gen, ch)
	delete(h.latest, ch)
	delete(h.stale, ch)
	delete(h.retry, ch)
	for _, ts := range threads {
		delete(h.gen, ch+"/"+ts)
		delete(h.stale, ch+"/"+ts)
		delete(h.retry, ch+"/"+ts)
	}
	h.mu.Unlock()
	a.emit(Event{Type: "history_purged", Channel: ch})
}

// advanceMutation is maybeAdvanceChannelUpdatedTimestamp (invalid false:
// a reaction, an edit, a reply, a pin) and
// maybeAdvanceChannelHistoryInvalidTimestamp (invalid true: a deletion, a
// history change): the socket's change moves ch's timestamp to its
// event_ts once the counts after the connect are applied, so the next
// counts read nothing for it.
func (a *App) advanceMutation(ch, eventTS string, invalid bool) {
	if ch == "" || eventTS == "" {
		return
	}
	h := &a.hist
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.countsApplied {
		return
	}
	h.init()
	m := h.muts[ch]
	p := &m.updated
	if invalid {
		p = &m.historyInvalid
	}
	if store.TSLess(firstNonEmpty(*p, zeroTS), eventTS) {
		*p = eventTS
		h.muts[ch] = m
	}
}

// socketDown is the socket store's disconnect: the counts are applied
// again after the next connect.
func (a *App) socketDown() {
	h := &a.hist
	h.mu.Lock()
	h.countsApplied = false
	h.mu.Unlock()
}

// pendingIn is the local ts of ch's sends Slack hasn't confirmed (pending
// or failed) in thread ("": the channel's own messages), which a page
// can't show and must leave (store.SetLatestSince, SetThreadSince).
func (a *App) pendingIn(ch, thread string) map[string]bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out map[string]bool
	for _, p := range a.pending {
		if p.confirmed || p.opts.Channel != ch {
			continue
		}
		in := p.opts.ThreadTS == thread
		if thread == "" {
			in = p.opts.ThreadTS == "" || p.opts.Broadcast
		}
		if !in {
			continue
		}
		if out == nil {
			out = map[string]bool{}
		}
		out[p.ts] = true
	}
	return out
}
