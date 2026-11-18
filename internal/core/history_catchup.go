package core

import (
	"context"
	"strings"
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
// A catch-up runs in the background: History and Replies return what core
// holds at once, as Slack renders the cached slice while it refetches
// (B0mF getPotentiallyOutdatedTimestamps), and a "history_caught_up" event
// tells the lists when the page lands. One that fails is tried again
// after catchUpRetry, a few times.

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
	// bg: the catch-ups running in the background (key as gen's)
	bg map[string]bool
	// older: the windows whose catch-up reads their scrollback again too
	// (refreshOlder): a mutation timestamp the socket never told marked
	// them
	older map[string]bool
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
		h.retry, h.muts, h.bg, h.older = map[string]time.Time{}, map[string]mutTimes{}, map[string]bool{}, map[string]bool{}
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
// mutation timestamps (maybePurgeChannelHistoryAfterMutations): for a
// conversation core holds a window of, a history_invalid past the one last
// applied purges its history and threads, else an updated past it marks
// them stale; then the answer's timestamps are kept
// (bulkAddOrUpdateChannelMutationTimestamps). Only entries with both set
// count, as in Slack.
//
// Two departures. A conversation's first answer of a launch is the
// baseline: Slack persists the last session's timestamps
// (channelMutationTimestamps), Clack doesn't, and every window read before
// boot's counts answered was purged or re-read. And the conversation on
// screen (watched) is read again instead of reset, so its list keeps the
// messages the user reads: on an updated always, on a history_invalid
// with no socket too (realtime off or down: nothing advances the
// timestamps, so every deletion would reset it; the poll's page drops what
// is gone from its range).
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
			old, had := h.muts[e.ID]
			h.muts[e.ID] = mutTimes{historyInvalid: e.HistoryInvalid, updated: e.Updated}
			if !had {
				continue // the baseline
			}
			purge := e.HistoryInvalid != zeroTS && (old.historyInvalid == "" || store.TSLess(old.historyInvalid, e.HistoryInvalid))
			if purge || e.Updated != zeroTS && (old.updated == "" || store.TSLess(old.updated, e.Updated)) {
				changes = append(changes, change{e.ID, purge})
			}
		}
	}
	if connected {
		h.countsApplied = true
	}
	h.mu.Unlock()
	if len(changes) == 0 {
		return
	}
	for _, x := range changes {
		if !a.S.HasTimeline(x.ch) { // Slack's channels with history
			continue
		}
		if !x.purge || !connected {
			// an updated, on screen or not (a split view's other pane, a
			// conversation not shown), or a history_invalid with no socket:
			// the window and its threads read Slack again behind their
			// cache when next shown, the scrollback too (refreshOlder), as
			// Slack's prJX only marks them stale; a purge made each open on
			// a network read
			a.markStale(x.ch)
			a.hist.mu.Lock()
			a.hist.older[x.ch] = true
			a.hist.mu.Unlock()
			a.emit(Event{Type: "history_stale", Channel: x.ch})
			continue
		}
		a.purgeHistory(x.ch) // a deletion the socket never told
	}
}

// markStale makes ch's window and cached threads read Slack again at their
// next read (markObjectsAsStale over the channel's timestamps).
func (a *App) markStale(ch string) {
	threads := a.S.ThreadTSs(ch)
	h := &a.hist
	h.mu.Lock()
	defer h.mu.Unlock()
	h.init()
	h.seq++
	h.stale[ch] = h.seq
	delete(h.retry, ch)
	for _, ts := range threads {
		h.stale[ch+"/"+ts] = h.seq
		delete(h.retry, ch+"/"+ts)
	}
}

// watchedConv is the conversation the UI shows (watch).
func (a *App) watchedConv() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.watchCh
}

// catchUpLater reads key's window (key: the conversation) or cached thread
// (conversation/thread ts) from Slack again in the background, at most
// one at a time per key, and tells the lists when it lands
// ("history_caught_up"): History and Replies returned what core holds. A
// read joined that began before a slow connect counts for nothing: once
// more. A failure is tried again after catchUpRetry, a few times.
func (a *App) catchUpLater(key string) {
	h := &a.hist
	h.mu.Lock()
	h.init()
	if h.bg[key] {
		h.mu.Unlock()
		return
	}
	h.bg[key] = true
	h.mu.Unlock()
	ch, ts, thread := strings.Cut(key, "/")
	go func() {
		defer func() {
			h.mu.Lock()
			delete(h.bg, key)
			h.mu.Unlock()
		}()
		for attempt := 0; attempt < 3; attempt++ {
			if attempt > 0 {
				select {
				case <-a.bg.Done():
					return
				case <-time.After(catchUpRetry):
				}
			}
			ctx, cancel := context.WithTimeout(a.bg, 45*time.Second)
			err := a.catchUpOnce(ctx, ch, ts, thread)
			cancel()
			if err == nil {
				ev := Event{Type: "history_caught_up", Channel: ch}
				if thread {
					ev.ThreadTS = ts
				}
				a.emit(ev)
				return
			}
			a.readFailed(key)
			if a.bg.Err() != nil {
				return
			}
		}
	}()
}

// catchUpOnce is catchUpLater's read: twice at most, when the first joined
// a read from before the mark that asked for it.
func (a *App) catchUpOnce(ctx context.Context, ch, ts string, thread bool) error {
	if thread {
		for try := 0; try < 2; try++ {
			if err := a.fetchThread(ctx, ch, ts); err != nil {
				return err
			}
			if !a.threadBehind(ch, ts) {
				return nil
			}
		}
		return nil
	}
	for try := 0; try < 2; try++ {
		if err := a.loadLatest(ctx, ch); err != nil {
			return err
		}
		if t := a.S.Timeline(ch); t == nil || !a.windowBehind(ch, t) {
			break
		}
	}
	return a.refreshOlder(ctx, ch)
}

// refreshPages bounds the older pages (100 messages each) a catch-up
// reads again (refreshOlder): the scrollback a user reads, not a long
// history loaded hours ago.
const refreshPages = 4

// refreshOlder reads the scrollback of ch's window from Slack again, when
// a mutation timestamp the socket never told marked it (h.older): an
// edit, a deletion or a reaction there. A stale mark re-reads the newest
// page and the cached threads; the older messages the list shows kept what
// the socket missed. Page by page from below the newest page back to the
// window's oldest message, at most refreshPages, each merged as the
// newest page is (store.RefreshRange). A failure leaves the mark for the
// next try.
func (a *App) refreshOlder(ctx context.Context, ch string) error {
	h := &a.hist
	h.mu.Lock()
	want := h.older[ch]
	delete(h.older, ch)
	h.mu.Unlock()
	t := a.S.Timeline(ch)
	if !want || t == nil {
		return nil
	}
	pending := map[string]bool{}
	for _, m := range a.pendingCopies(ch, "") {
		pending[m.TS] = true
	}
	oldest := ""
	for i := range t.Msgs {
		if m := &t.Msgs[i]; !m.IsEphemeral && !pending[m.TS] {
			oldest = m.TS
			break
		}
	}
	cursor := t.PageOldest
	for i := 0; i < refreshPages && cursor != "" && oldest != "" && store.TSLess(oldest, cursor); i++ {
		live, pins, subs := a.S.LiveSeq(), a.S.PinSeq(), a.S.SubSeq()
		p, err := a.C.History(ctx, ch, slack.HistoryOpts{Latest: cursor, Limit: 100})
		if err != nil {
			h.mu.Lock()
			h.older[ch] = true
			h.mu.Unlock()
			return err
		}
		lo := ""
		if p.HasMore {
			for _, m := range p.Messages {
				if lo == "" || store.TSLess(m.TS, lo) {
					lo = m.TS
				}
			}
		}
		// fetchLatest's fixups: the processed files core holds of the
		// page's stubs, and the pins and subscriptions events changed while
		// it was out; a stub left gets its files read
		a.keepPageFiles(ch, p.Messages)
		a.S.RefreshRange(ch, p.Messages, lo, cursor, store.LatestOpts{Since: live, Pending: a.pendingCopies(ch, "")})
		a.S.SyncPins(ch, pins, p.Messages)
		a.S.SyncSubs(ch, subs, p.Messages)
		for j := range p.Messages {
			if needsFileInfo(&p.Messages[j]) {
				go a.fillFiles(ch, p.Messages[j].TS)
			}
		}
		if lo == "" {
			break
		}
		cursor = lo
	}
	return nil
}

// catchingUp reports whether a background catch-up of key is out (tests).
func (a *App) catchingUp(key string) bool {
	h := &a.hist
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.bg[key]
}

// noteSlowConnect is the socket's slow connect: what it brings from now on
// may come after a gap, past every window's boundary (store
// NoteSlowConnect, Timeline.Contig).
func (a *App) noteSlowConnect() { a.S.NoteSlowConnect() }

// gapEpoch is, while ch's window is behind a slow connect and holds
// messages past its gap (store PastGap), the Epoch of its view past the
// gap (GapView), for a message event to carry (Event.Epoch); 0 otherwise.
// Cheap unless the window is behind.
func (a *App) gapEpoch(ch string) uint64 {
	if _, ok := a.S.PastGap(ch); !ok {
		return 0
	}
	if t := a.S.Timeline(ch); t != nil {
		return t.Epoch | GapView
	}
	return 0
}

// confirmFromPage confirms the viewer's sends whose copy a page from
// Slack holds (by client_msg_id): one whose reply and echo were lost (a
// Wi-Fi drop) went out after all. The page only hid the failed copy, which
// came back with its Retry once the message left the newest page.
func (a *App) confirmFromPage(ch string, msgs []slack.Message) {
	for i := range msgs {
		m := &msgs[i]
		if m.ClientMsgID == "" {
			continue
		}
		a.mu.Lock()
		p, ok := a.pending[m.ClientMsgID]
		open := ok && !p.confirmed
		a.mu.Unlock()
		if open {
			c := *m
			a.confirmPending(ch, &c)
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

// pendingCopies are the local copies of ch's sends Slack hasn't
// confirmed (pending or failed) in thread ("": the channel's own
// messages), which no page holds and every merge keeps
// (store.LatestOpts.Pending, ThreadOpts.Pending).
func (a *App) pendingCopies(ch, thread string) []slack.Message {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []slack.Message
	for _, p := range a.pending {
		if p.confirmed || p.opts.Channel != ch || p.local.TS == "" {
			continue
		}
		in := p.opts.ThreadTS == thread
		if thread == "" {
			in = p.opts.ThreadTS == "" || p.opts.Broadcast
		}
		if in {
			out = append(out, p.local)
		}
	}
	return out
}

// localTSSet is the ts of ch's local copies (pendingCopies): where an
// older page starts goes by the others.
func (a *App) localTSSet(ch string) map[string]bool {
	out := map[string]bool{}
	for _, m := range a.pendingCopies(ch, "") {
		out[m.TS] = true
	}
	return out
}

// keepPageFiles gives a page's messages the processed files core holds of
// their stubs (a pasted screenshot read while Slack was still processing
// it, fillFiles done since): Slack keeps files in a store of their own,
// which a history page's stub of a file doesn't reset, and the message
// went back to a generic FILE card at every read of its page.
func (a *App) keepPageFiles(ch string, msgs []slack.Message) {
	for i := range msgs {
		if !needsFileInfo(&msgs[i]) {
			continue
		}
		if old, ok := a.S.Find(ch, msgs[i].TS); ok {
			keepFiles(&msgs[i], &old)
		}
	}
}
