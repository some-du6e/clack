package core

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/logtag"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// History rewritten on Slack's side: a bulk delete, a retention pass, an
// import. The socket sends a conversation's channel_history_changed
// (group_, im_, mpim_), and client.counts carries each conversation's
// mutation timestamps, history_invalid and updated, for what the socket
// missed. Without either the client keeps drawing what it cached: the
// deleted messages as ghosts, the old copies of what changed.
//
// Slack's web client (boot-render 3c277d38, client-boot 83aff18a):
//
//   - channelHistoryChanged (all four events; an unknown conversation is
//     ignored): truncateChannelHistoryPriorToTimestamp at the event's
//     latest (store/history_changed.go), then
//     maybeAdvanceChannelHistoryInvalidTimestamp at its event_ts, so the
//     counts after the next connect don't purge it again. The message
//     subtypes of the same names are ignored (messageReceived).
//   - channelMutationTimestamps (Oy+F): each conversation's
//     historyInvalidTs and updatedTs. While counts are applied
//     (countsAreApplied: since the last connect's client.counts went in)
//     the socket's own changes advance them: historyInvalidTs at a
//     message_deleted and a history change, updatedTs at a
//     message_changed, message_replied, pin_added, pin_removed and
//     reaction_removed (maybeAdvanceChannelUpdatedTimestamp), each to its
//     event_ts when newer.
//   - applyCountsToChannels: for an entry with both timestamps, a
//     conversation with cached history goes through
//     maybePurgeChannelHistoryAfterMutations (FhIQ): a history_invalid
//     newer than the applied one purges its history and its threads'
//     (removeAllTimestamps({includeThreads: true})); else an updated newer
//     than the applied one marks its cached messages stale, read again as
//     a view shows them (the cache freshness manager's markObjectsAsStale
//     and maybeRefetch). The entry's timestamps are then the applied ones.
//
// Clack's stale history is re-read as Slack's catch-up reads a window
// (reached_end.go): the next time a list reads the conversation (the open
// one at once) it draws the cached page and core reads the newest page in
// the background, which replaces the cached one (store SetLatestReadAt:
// what was deleted goes, what changed is the new copy), then
// "history_caught_up" has the lists load it again. Slack re-reads each
// stale range as it renders; Clack's lists read older pages again as they
// scroll to them, the newest page having replaced the older ones. Any
// newest-page read begun after a stale mark answers it (fetchLatest).
//
// Where Clack differs because it keeps less than Slack:
//
//   - What only this client has survives a purge or a truncation, as the
//     cache budget never evicts it (shouldPersistMessage): the viewer's
//     sends not confirmed yet (a failed one is the only copy of its text)
//     and ephemeral messages. A timeline left with only those is read
//     again at once.
//   - Slack persists channelMutationTimestamps across launches and the
//     boot's conversations.view seeds the open channel's; Clack keeps
//     them in memory, so a conversation's first counts answer since
//     launch is its baseline, and purges nothing. An answer older than
//     the applied value (the socket advanced it while the call was out)
//     doesn't move it back.
//   - With no socket up (realtime off, or down) nothing echoes the
//     viewer's own deletes, edits and pins, so those writes advance the
//     applied value themselves (ownMutation).
//
// A fast reconnect fetches no counts and counts are applied at once, as
// Slack's fetchAndApplyReconnectCounts: the socket replays what it missed.
//
// What Slack doesn't port here Clack doesn't either: Activity's and
// search's copies stay (Slack's handler touches neither), and a group
// DM's conversation object isn't marked stale (Clack keeps none on disk
// to refresh); an im or mpim being migrated (is_migrating) isn't read
// again (Clack has no migration banner).

// zeroTS is the ts Slack's counts send for "never".
const zeroTS = "0000000000.000000"

// mutTimes is one conversation's channelMutationTimestamps record.
type mutTimes struct{ historyInvalid, updated string }

// histMuts is an App's channelMutationTimestamps, countsAreApplied and
// its conversations whose cached history is stale (each mark's seq: a
// newest-page read begun after it clears it).
type histMuts struct {
	mu      sync.Mutex
	muts    map[string]mutTimes
	applied bool
	stale   map[string]uint64
	seq     uint64
}

var histMutsOf = newPerApp() // *App → *histMuts

func (a *App) histMuts() *histMuts {
	if v, ok := histMutsOf.Load(a); ok {
		return v.(*histMuts)
	}
	v, _ := histMutsOf.LoadOrStore(a, &histMuts{muts: map[string]mutTimes{}, stale: map[string]uint64{}})
	return v.(*histMuts)
}

// countsNotYetApplied is the socket store's setCountsAreApplied(false):
// a connect, whose client.counts is still to come.
func (a *App) countsNotYetApplied() {
	h := a.histMuts()
	h.mu.Lock()
	h.applied = false
	h.mu.Unlock()
}

// countsApplied is setCountsAreApplied(true): a client.counts answer is
// in, or the connect was fast (fetchAndApplyReconnectCounts fetches
// nothing then: the socket replays what it missed).
func (a *App) countsApplied() {
	h := a.histMuts()
	h.mu.Lock()
	h.applied = true
	h.mu.Unlock()
}

// ownMutationAhead is how far ahead of the local clock ownMutation puts
// a write of the viewer's: Slack's clock, which stamps the change, isn't
// the local one.
const ownMutationAhead = 30 * time.Second

// ownMutation is a write of the viewer's that changed ch's history (a
// delete: invalid; an edit, a pin) with no socket up: Slack's client
// hears its own change on the socket and advances the applied timestamp
// to the event's event_ts, and with nothing to echo it the next counts,
// carrying it, purged the conversation (a client with realtime off, at
// every poll). The local clock, ownMutationAhead ahead, stands for
// Slack's.
func (a *App) ownMutation(ch string, invalid bool) {
	if a.RTM != nil && a.RTM.Connected() {
		return // the socket's event advances it
	}
	ts := fmt.Sprintf("%d.000000", time.Now().Add(ownMutationAhead).Unix())
	a.advanceMutation(ch, ts, invalid)
}

// unconfirmedIn is the ts of the viewer's sends in ch not confirmed yet
// (pending or failed copies): only this client has them, so a history
// change keeps them (store/history_changed.go).
func (a *App) unconfirmedIn(ch string) map[string]bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := map[string]bool{}
	for _, p := range a.pending {
		if p.opts.Channel == ch && !p.confirmed {
			out[p.ts] = true
		}
	}
	return out
}

// advanceMutation is maybeAdvanceChannelHistoryInvalidTimestamp
// (invalid) or maybeAdvanceChannelUpdatedTimestamp: while counts are
// applied, ch's timestamp moves to eventTS when that's newer.
func (a *App) advanceMutation(ch, eventTS string, invalid bool) {
	if ch == "" || eventTS == "" {
		return
	}
	h := a.histMuts()
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.applied {
		return
	}
	m := h.muts[ch]
	cur := &m.updated
	if invalid {
		cur = &m.historyInvalid
	}
	if store.TSLess(orZero(*cur), eventTS) {
		*cur = eventTS
		h.muts[ch] = m
	}
}

func orZero(ts string) string {
	if ts == "" {
		return zeroTS
	}
	return ts
}

// advanceFromEvent advances the updated timestamp a pin or a reaction's
// removal moves (handle, before the event's own handler; onMessage
// advances a message change's).
func (a *App) advanceFromEvent(ev slack.Event) {
	switch ev.Type {
	case "pin_added", "pin_removed", "reaction_removed":
	default:
		return
	}
	var e struct {
		EventTS   string `json:"event_ts"`
		ChannelID string `json:"channel_id"`
		Item      struct {
			Channel string `json:"channel"`
		} `json:"item"`
	}
	if json.Unmarshal(ev.Raw, &e) != nil {
		return
	}
	a.advanceMutation(firstNonEmpty(e.ChannelID, e.Item.Channel, chanID(ev.Channel)), e.EventTS, false)
}

// onHistoryChanged is channelHistoryChanged.
func (a *App) onHistoryChanged(ch string, ev slack.Event) {
	if ch == "" {
		log.Printf("core: %s without a channel", ev.Type)
		return
	}
	if _, ok := a.S.Conv(ch); !ok {
		log.Printf("core: %s for an unknown conversation %s", ev.Type, logtag.ID(ch))
		return
	}
	var e struct {
		Latest  string `json:"latest"`
		EventTS string `json:"event_ts"`
	}
	_ = json.Unmarshal(ev.Raw, &e)
	gone := a.S.TruncateHistoryBefore(ch, e.Latest, a.unconfirmedIn(ch))
	log.Printf("core: %s %s: history before %s let go (%d histories)", ev.Type, logtag.ID(ch), e.Latest, len(gone))
	a.emitHistoryGone(ch, e.Latest, gone)
	a.advanceMutation(ch, e.EventTS, true)
}

// emitHistoryGone tells the lists and the plugins' mirror what of ch's
// history went: each timeline and thread as the cache budget's "evicted"
// does, then "history_changed" for the lists' windows (TS: latest, ""
// when the whole history went).
func (a *App) emitHistoryGone(ch, latest string, gone []store.Evicted) {
	for _, g := range gone {
		ts := g.Oldest
		if g.Whole {
			ts = ""
		}
		a.emit(Event{Type: "evicted", Channel: g.Channel, ThreadTS: g.Thread, TS: ts})
		if g.Whole && g.Thread == "" && a.S.Timeline(ch) != nil {
			// a timeline left with only what this client has (a failed
			// send): the newest page is read into it, and carries it
			a.markStale(ch)
		}
	}
	a.emit(Event{Type: "history_changed", Channel: ch, TS: latest})
}

// applyMutationTimestamps is applyCountsToChannels' share of a
// client.counts answer: maybePurgeChannelHistoryAfterMutations for each
// conversation with cached history, then the answer's timestamps as the
// applied ones, and counts are applied (setCountsAreApplied(true)). A
// conversation the client applied nothing for yet (the first answer
// since launch) takes the answer as its baseline: Slack persists
// channelMutationTimestamps across launches and the boot's
// conversations.view brings the open one's, so it purges only for a
// change since; with nothing applied, Clack purged the history it had
// just read, and the open channel blanked and loaded again. And the
// applied value stays when it's newer than the answer's: the socket
// advanced it while the call was out.
func (a *App) applyMutationTimestamps(c *slack.Counts) {
	type purge struct {
		ch    string
		whole bool
	}
	var todo []purge
	h := a.histMuts()
	h.mu.Lock()
	for _, l := range [][]slack.CountEntry{c.Channels, c.IMs, c.MPIMs} {
		for i := range l {
			e := &l[i]
			if e.Updated == "" || e.HistoryInvalid == "" {
				continue
			}
			old, had := h.muts[e.ID]
			if had && a.S.HasHistory(e.ID) {
				switch {
				case e.HistoryInvalid != zeroTS && (old.historyInvalid == "" || store.TSLess(old.historyInvalid, e.HistoryInvalid)):
					todo = append(todo, purge{e.ID, true})
				case e.Updated != zeroTS && (old.updated == "" || store.TSLess(old.updated, e.Updated)):
					todo = append(todo, purge{e.ID, false})
				}
			}
			h.muts[e.ID] = mutTimes{historyInvalid: newerTS(old.historyInvalid, e.HistoryInvalid), updated: newerTS(old.updated, e.Updated)}
		}
	}
	h.applied = true
	h.mu.Unlock()
	for _, p := range todo {
		if p.whole {
			log.Printf("core: counts: purging cached history for %s (history_invalid newer than applied)", logtag.ID(p.ch))
			a.emitHistoryGone(p.ch, "", a.S.DropHistory(p.ch, a.unconfirmedIn(p.ch)))
			continue
		}
		log.Printf("core: counts: history of %s is stale (updated newer than applied)", logtag.ID(p.ch))
		a.markStale(p.ch)
	}
}

// newerTS is the newer of two timestamps ("" is older than any).
func newerTS(a, b string) string {
	if a == "" || b != "" && store.TSLess(a, b) {
		return b
	}
	return a
}

// markStale marks ch's cached history to be read again, the open
// conversation's at once.
func (a *App) markStale(ch string) {
	h := a.histMuts()
	h.mu.Lock()
	h.seq++
	h.stale[ch] = h.seq
	h.mu.Unlock()
	a.mu.Lock()
	open := a.watchCh == ch
	a.mu.Unlock()
	if open {
		a.startStaleRead(ch)
	}
}

// staleMark is where the stale marks are now: a newest-page read begun
// here clears the marks made before it (readFresh).
func (a *App) staleMark() uint64 {
	h := a.histMuts()
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.seq
}

// readFresh is a newest page of ch read from Slack, begun at mark: a
// stale mark made before it is cleared (one made while it was out
// stays).
func (a *App) readFresh(ch string, mark uint64) {
	h := a.histMuts()
	h.mu.Lock()
	if s, ok := h.stale[ch]; ok && s <= mark {
		delete(h.stale, ch)
	}
	h.mu.Unlock()
}

// historyStale reports whether ch's cached history is to be read again
// (the counts' updated).
func (a *App) historyStale(ch string) bool {
	h := a.histMuts()
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.stale[ch] != 0
}

// staleReading holds the conversations whose stale read runs, per App.
var staleReading sync.Map // catchUpKey → struct{}

// startStaleRead reads ch's newest page again in the background, once at
// a time: it replaces the cached history (fetchLatest clears the mark),
// and "history_caught_up" has the lists load it. A read that fails
// leaves the history stale for the next list to read it.
func (a *App) startStaleRead(ch string) {
	if !a.historyStale(ch) {
		return
	}
	k := catchUpKey{a, ch}
	if _, running := staleReading.LoadOrStore(k, struct{}{}); running {
		return
	}
	go func() {
		defer staleReading.Delete(k)
		ctx, cancel := context.WithTimeout(a.bg, time.Minute)
		defer cancel()
		if err := a.loadLatest(ctx, ch); err != nil {
			if a.bg.Err() == nil {
				log.Printf("history %s: stale read: %v", logtag.ID(ch), err)
			}
			return
		}
		a.emit(Event{Type: "history_caught_up", Channel: ch})
	}()
}
