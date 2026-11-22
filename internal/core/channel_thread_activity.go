package core

import (
	"encoding/json"

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

// expReplyBarUnread is the experiment the reply bar's mark, and the lists
// behind it, are under. Slack keeps the lists under its thread badging
// and programmable sidebar experiments too; Clack draws nothing else from
// them.
const expReplyBarUnread = "unread_thread_indication_in_replybar"

// chActList is one channel's list.
type chActList struct {
	// loaded: a fetch answered for the channel (its cursor is set); before
	// that the socket's items are dropped
	loaded bool
	items  map[string]chActItem // by key
	// live is what the socket and the thread marks changed, by key, with
	// their place in chActSeq: an answer to a fetch requested before a
	// change keeps it (Slack formats a fetch's items against the socket
	// sequence it was requested at, requestRtmSeq)
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

// chActNow is the sequence's position: a fetch notes it as it goes out.
func (a *App) chActNow() uint64 {
	a.chActMu.Lock()
	defer a.chActMu.Unlock()
	return a.chActSeq
}

// installChannelActivity is upsertChannelActivityItems with refreshItems
// for a fetch of ch requested at since: its items replace the list, the
// changes since then go back on top, and the list is loaded.
func (a *App) installChannelActivity(ch string, items []ActivityItem, since uint64) {
	a.chActMu.Lock()
	l := a.chActListLocked(ch)
	l.items = make(map[string]chActItem, len(items))
	for i := range items {
		if k := items[i].Key; k != "" {
			l.items[k] = actListItem(&items[i])
		}
	}
	for k, op := range l.live {
		switch {
		case op.seq <= since:
			delete(l.live, k) // the answer has it
		case op.gone:
			delete(l.items, k)
		default:
			l.items[k] = op.item
		}
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
	if l == nil || !l.loaded {
		a.chActMu.Unlock()
		return // no cursor: Slack's goes nowhere (the pane's fetch lists it)
	}
	a.chActSeq++
	op := chActOp{seq: a.chActSeq, item: actListItem(&it), gone: !it.Unread}
	l.live[it.Key] = op
	if op.gone {
		delete(l.items, it.Key)
	} else {
		l.items[it.Key] = op.item
	}
	a.chActMu.Unlock()
	a.emit(Event{Type: "thread_activity", Channel: it.Channel})
}

// threadActivityRead is updateUnreadActivityItemByThreadKey for the
// channel lists: the thread root of ch read to lastRead takes out its
// items up to it (an item's messageTs, a thread_v2 bundle's latest_ts, at
// or before the cursor). A fetch out meanwhile doesn't bring them back.
func (a *App) threadActivityRead(ch, root, lastRead string) {
	if ch == "" || root == "" || lastRead == "" || a.experiment(expReplyBarUnread) != "on" {
		return
	}
	a.chActMu.Lock()
	changed := false
	if l := a.chActs[ch]; l != nil {
		for k, it := range l.items {
			if it.ThreadTS == root && !store.TSLess(lastRead, it.TS) {
				a.chActSeq++
				l.live[k] = chActOp{seq: a.chActSeq, gone: true}
				delete(l.items, k)
				changed = true
			}
		}
	}
	a.chActMu.Unlock()
	if changed {
		a.emit(Event{Type: "thread_activity", Channel: ch})
	}
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
