package core

import (
	"encoding/json"
	"sort"

	"github.com/some-du6e/clack/internal/store"
)

// The reply bar's unread thread activity, one list per channel, kept as
// Slack's activityIA4 keeps channelActivityLists and channelActivityCursors
// (client-boot tsND, build 548618a2169f2a90) under
// unread_thread_indication_in_replybar:
//
//   - a channel's list is there once fetchActivitiesForChannel answered for
//     it (shared oKuF upsertChannelActivityItems with refreshItems: the
//     answer replaces the list and sets the channel's cursor), which the
//     message pane asks for once per mount while the channel has unread
//     threads and its list holds no thread_v2 item (MessagePane
//     maybeFetchUnreadThreadActivity, hasLoadedThreadActivityForChannel;
//     messages/unread_threads.go);
//   - from then on each activity item the socket brings for the channel
//     goes into its list as it comes, with no fetch (client-boot PY/r:
//     upsertChannelActivityItems; a read one, under
//     replybar_unread_reliability_fix, removeChannelActivityItem), so a
//     reply's "Unread" mark comes with its thread_v2 item, in the socket's
//     order right behind the reply's own message events. A channel that
//     has no list yet drops it, as Slack's has no cursor to put it under;
//   - a thread read here or in another client (markThread, threadMarked:
//     /DLK updateActivitiesUnreadState → updateUnreadActivityItemByThreadKey)
//     takes the thread's items up to the read cursor out.
//
// Clack kept the fetch's counts alone and fetched again whenever the
// channel's unread thread count changed, so a reply's mark came an
// activity.feed round trip after the reply drew (Karim, 2026-11-22: "i
// swear i saw your new reply before the Unread thing popped up").
//
// What the socket can't keep current, Clack repairs: a threads state with
// no unread threads in a channel clears its list (pruneThreadActivity),
// the pane fetches it again when the channel's count moves and after a
// slow reconnect (ThreadActivityGen; messages/unread_threads.go), and a
// thread read, or an item read, before the list is there is kept and
// taken out of the answer that lists it (a tombstone).

// expReplyBarUnread is the experiment the reply bar's mark, and the lists
// behind it, are under. Slack keeps the lists under its thread badging
// and programmable sidebar experiments too; Clack draws nothing else from
// them.
const expReplyBarUnread = "unread_thread_indication_in_replybar"

// chActList is one channel's list.
type chActList struct {
	// loaded: a fetch answered for the channel (its cursor is set); before
	// that the socket's unread items are dropped (what takes items out is
	// kept for the answer)
	loaded bool
	at     uint64               // the answer's place in chActSeq (its request's)
	items  map[string]chActItem // by key
	// live is what the socket, the thread marks and the threads' states
	// changed, by key, with their place in chActSeq: an answer to a fetch
	// requested before a change keeps it (Slack formats a fetch's items
	// against the socket sequence it was requested at, requestRtmSeq)
	live map[string]chActOp
}

// chActItem is what getUnreadThreadsCountForMessage reads of an item.
type chActItem struct {
	Type, ThreadTS, TS string
	Unread             bool
}

type chActOp struct {
	seq  uint64
	item chActItem
	gone bool
	// a thread read to markRead: its items up to it go (threadActivityRead)
	markRoot, markRead string
	// the threads' state at seq said the channel has no unread threads:
	// the followed threads' items from before it go (pruneThreadActivity)
	prune bool
}

// markKey is a thread mark's key among a list's live ops, pruneKey the
// channel's last no-unread-threads state's.
func markKey(root string) string { return "\x00mark:" + root }

const pruneKey = "\x00prune"

// applyLocked is op on l's items (a.chActMu held).
func (l *chActList) applyLocked(key string, op chActOp) {
	switch {
	case op.prune:
		l.pruneLocked(op.seq)
	case op.markRoot != "":
		for k, it := range l.items {
			if it.ThreadTS == op.markRoot && !store.TSLess(op.markRead, it.TS) {
				delete(l.items, k)
			}
		}
	case op.gone:
		delete(l.items, key)
	default:
		l.items[key] = op.item
	}
}

// pruneLocked takes out the items a no-unread-threads state at seq covers:
// the count is the followed threads', so a thread_v2 item's thread (a
// followed one) and its other items (a mention in it), all but those newer
// than the state (the socket brought them, or an answer requested, after
// seq). An @here or a keyword in a thread the viewer doesn't follow isn't
// the count's, and stays as Slack keeps it. It reports whether an item
// went.
func (l *chActList) pruneLocked(seq uint64) bool {
	newer := func(k string) bool {
		if op, ok := l.live[k]; ok {
			return op.seq > seq // the socket's (an item a gone one took out isn't there)
		}
		return l.at > seq // the answer's
	}
	followed := map[string]bool{}
	for k, it := range l.items {
		if it.Type == "thread_v2" && it.ThreadTS != "" && !newer(k) {
			followed[it.ThreadTS] = true
		}
	}
	gone := false
	for k, it := range l.items {
		if followed[it.ThreadTS] && !newer(k) {
			delete(l.items, k)
			gone = true
		}
	}
	return gone
}

func actListItem(it *ActivityItem) chActItem {
	return chActItem{Type: it.Type, ThreadTS: it.ThreadTS, TS: it.TS, Unread: it.Unread}
}

// chActListLocked is ch's list, made (not loaded) if it has none
// (a.chActMu held).
func (a *App) chActListLocked(ch string) *chActList {
	if a.chActs == nil {
		a.chActs = map[string]*chActList{}
	}
	l := a.chActs[ch]
	if l == nil {
		l = &chActList{items: map[string]chActItem{}, live: map[string]chActOp{}}
		a.chActs[ch] = l
	}
	return l
}

// chActMark is a place of its own in the sequence, for a request going
// out (a counts read) or a threads' state taken: every change before it
// is older, every one after it newer.
func (a *App) chActMark() uint64 {
	a.chActMu.Lock()
	defer a.chActMu.Unlock()
	a.chActSeq++
	return a.chActSeq
}

// chActBegin is chActMark for a fetch of ch going out: ch's list is there
// from now on (not loaded), so what changes it while the fetch is out
// (a thread read, a no-unread-threads state) is kept for the answer.
func (a *App) chActBegin(ch string) uint64 {
	a.chActMu.Lock()
	defer a.chActMu.Unlock()
	a.chActListLocked(ch)
	a.chActSeq++
	return a.chActSeq
}

// installChannelActivity is upsertChannelActivityItems with refreshItems
// for a fetch of ch requested at since: its items replace the list, the
// changes since then go back on top, and the list is loaded.
func (a *App) installChannelActivity(ch string, items []ActivityItem, since uint64) {
	a.chActMu.Lock()
	l := a.chActListLocked(ch)
	if l.loaded && since < l.at {
		a.chActMu.Unlock()
		return // an answer requested before the one the list holds (another pane's)
	}
	l.at = since
	l.items = make(map[string]chActItem, len(items))
	for i := range items {
		if k := items[i].Key; k != "" {
			l.items[k] = actListItem(&items[i])
		}
	}
	// the changes after the request, oldest first: the answer has the rest
	keys := make([]string, 0, len(l.live))
	for k, op := range l.live {
		if op.seq <= since {
			delete(l.live, k)
			continue
		}
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return l.live[keys[i]].seq < l.live[keys[j]].seq })
	for _, k := range keys {
		l.applyLocked(k, l.live[k])
	}
	l.loaded = true
	a.chActMu.Unlock()
	a.emit(Event{Type: "thread_activity", Channel: ch})
}

// noteChannelThreadActivity is the socket's activity event for the
// channel lists (PY/r), on the pump in the socket's order: an unread item
// for a channel whose list is loaded goes in at once, a read one comes out
// under replybar_unread_reliability_fix (without it Slack leaves the list
// as it is).
func (a *App) noteChannelThreadActivity(raw json.RawMessage) {
	if a.experiment(expReplyBarUnread) != "on" {
		return
	}
	var e struct {
		Subtype string      `json:"subtype"`
		Entry   activityRaw `json:"entry"`
	}
	if json.Unmarshal(raw, &e) != nil || e.Subtype != "activity_updated" || !activityLiveType(e.Entry.Item.Type) {
		return // onActivityEvent drops an entry of any other type before a store sees it
	}
	it, _ := activityItem(&e.Entry)
	if it.Channel == "" || it.Key == "" {
		return
	}
	if !it.Unread && a.experiment("replybar_unread_reliability_fix") != "on" {
		return
	}
	a.chActMu.Lock()
	l := a.chActs[it.Channel]
	if (l == nil || !l.loaded) && it.Unread {
		a.chActMu.Unlock()
		return // no cursor: Slack's goes nowhere (the pane's fetch lists it)
	}
	// a read one before the list is there is kept for the answer that lists
	// it (a fetch out meanwhile was answered before the read)
	l = a.chActListLocked(it.Channel)
	a.chActSeq++
	op := chActOp{seq: a.chActSeq, item: actListItem(&it), gone: !it.Unread}
	l.live[it.Key] = op
	l.applyLocked(it.Key, op)
	a.chActMu.Unlock()
	a.emit(Event{Type: "thread_activity", Channel: it.Channel})
}

// threadActivityRead is updateUnreadActivityItemByThreadKey for the
// channel lists: the thread root of ch read to lastRead takes out its
// items up to it (an item's messageTs, a thread_v2 bundle's latest_ts, at
// or before the cursor). The mark is kept, the list there or not yet (a
// tombstone): a fetch out meanwhile, answered before the mark, doesn't
// bring them back. The thread's latest mark is its read cursor, also one
// marked unread to an earlier reply.
func (a *App) threadActivityRead(ch, root, lastRead string) {
	if ch == "" || root == "" || lastRead == "" || a.experiment(expReplyBarUnread) != "on" {
		return
	}
	a.chActMu.Lock()
	l := a.chActListLocked(ch)
	before := len(l.items)
	a.chActSeq++
	op := chActOp{seq: a.chActSeq, markRoot: root, markRead: lastRead}
	k := markKey(root)
	l.live[k] = op
	l.applyLocked(k, op)
	changed := len(l.items) != before
	a.chActMu.Unlock()
	if changed {
		a.emit(Event{Type: "thread_activity", Channel: ch})
	}
}

// pruneThreadActivity is the threads' state in the store for the channel
// lists: a channel with no unread threads (unread_count_by_channel, from
// a counts read or an update_thread_state) has none of its followed
// threads' items in its list either, whatever the socket missed (a sleep,
// a slow reconnect: the thread read in another client). since is when
// that state was asked for (chActMark): an item the socket brought after it
// is newer and stays, and an answer to a fetch requested before it loses
// them too (the state is kept among the list's live changes).
func (a *App) pruneThreadActivity(since uint64) {
	a.S.RLock()
	counts := make(map[string]int, len(a.S.ThreadsUnreadByChannel))
	for ch, n := range a.S.ThreadsUnreadByChannel {
		counts[ch] = n
	}
	a.S.RUnlock()
	a.chActMu.Lock()
	var changed []string
	for ch, l := range a.chActs {
		if counts[ch] > 0 {
			continue
		}
		if old, ok := l.live[pruneKey]; !ok || old.seq < since {
			l.live[pruneKey] = chActOp{seq: since, prune: true}
		}
		if l.pruneLocked(since) {
			changed = append(changed, ch)
		}
	}
	a.chActMu.Unlock()
	for _, ch := range changed {
		a.emit(Event{Type: "thread_activity", Channel: ch})
	}
}

// threadActivityStale is a slow reconnect for the channel lists: what the
// socket said while it was down is lost, so each list is fetched again
// as the pane next draws it (ThreadActivityGen).
func (a *App) threadActivityStale() {
	a.chActMu.Lock()
	a.chActGen++
	a.chActMu.Unlock()
	a.emit(Event{Type: "thread_activity"})
}

// ThreadActivityGen counts the slow reconnects the channel lists went
// through: a pane holding a list fetched before the last one fetches it
// again (messages/unread_threads.go).
func (a *App) ThreadActivityGen() uint64 {
	a.chActMu.Lock()
	defer a.chActMu.Unlock()
	return a.chActGen
}

// UnreadThreadsFor is getUnreadThreadsCountForMessage (3x6A AY): the
// unread thread items of root ts in ch's list (isUnreadThreadReply).
func (a *App) UnreadThreadsFor(ch, ts string) int {
	a.chActMu.Lock()
	defer a.chActMu.Unlock()
	l := a.chActs[ch]
	if l == nil {
		return 0
	}
	n := 0
	for _, it := range l.items {
		if it.ThreadTS == ts && listUnreadThreadReply(it) {
			n++
		}
	}
	return n
}

// unreadThreadCounts is UnreadThreadsFor for every thread of ch's list.
func (a *App) unreadThreadCounts(ch string) map[string]int {
	a.chActMu.Lock()
	defer a.chActMu.Unlock()
	out := map[string]int{}
	if l := a.chActs[ch]; l != nil {
		for _, it := range l.items {
			if listUnreadThreadReply(it) {
				out[it.ThreadTS]++
			}
		}
	}
	return out
}

// ThreadActivityLoaded is hasLoadedThreadActivityForChannel: ch's list
// holds a thread_v2 item.
func (a *App) ThreadActivityLoaded(ch string) bool {
	a.chActMu.Lock()
	defer a.chActMu.Unlock()
	if l := a.chActs[ch]; l != nil {
		for _, it := range l.items {
			if it.Type == "thread_v2" {
				return true
			}
		}
	}
	return false
}

// listUnreadThreadReply is isUnreadThreadReply (3x6A I6) of a list item.
func listUnreadThreadReply(it chActItem) bool {
	return it.ThreadTS != "" && it.Unread && it.Type != "message_reaction" && (it.Type == "thread_v2" || it.TS != it.ThreadTS)
}
