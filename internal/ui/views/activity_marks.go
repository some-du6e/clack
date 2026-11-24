package views

import (
	"strconv"
	"time"

	"github.com/some-du6e/clack/internal/core"
)

// Activity's read state between a mark and the server's answer (client-boot
// pErv, tsND, build 66e72b57ade1).
//
// Pending marks (pErv recordPendingActivityMark, reconcileReadStateWithPendingMark):
// a row marked read or unread here records the mark until its write
// (activity.markRead / markUnread) answers. An activity_updated that comes
// meanwhile carries the server's state from before the mark, and Slack's
// handleActivityRTMEvent overlays the mark on it
// (applyPendingMarkToActivityUpdate): one for a newer feed_ts (a new
// reply) ends the mark and stands; one that agrees ends the mark; one that
// disagrees takes the mark's state. A mark ends by itself after five
// minutes. Clack dropped the row's mark on any realtime copy, so an update
// with is_unread true from before the mark (activity.markRead took 4.3 s
// once) put the row back to unread.
//
// A thread or a conversation read (thread_marked, subscriptions.thread.mark,
// channel_marked, conversations.mark): updateUnreadActivityItemByThreadKey
// and its channel twin mark the thread's (the conversation's) unread
// items at or before the new last read read at once, here alone (no
// activity.markRead), and take them off the badge counts
// (getOptimisticUpdates, decrementBadgeCounts), under the activity inbox.
// Clack waited for the activity_updated that the server sends after.

// pendingMarkLife is how long a mark lasts without an answer (pErv's
// isExpired: 3e5 ms).
const pendingMarkLife = 5 * time.Minute

// actPendingMark is one of pErv's records: the state the mark set, the
// entry's feed_ts then, and whether it ended (expired, or an update agreed
// with it).
type actPendingMark struct {
	id       uint64
	unread   bool
	feedTS   float64
	markedAt time.Time
	ended    bool
}

// actPending is the view's pending marks, by row key (UI goroutine).
type actPending struct {
	seq   uint64
	marks map[string]*actPendingMark
}

func parseFeedTS(ts string) float64 {
	f, _ := strconv.ParseFloat(ts, 64)
	return f
}

// record is recordPendingActivityMark: the mark of key to unread, the
// entry at feedTS. It returns the mark's id for clear.
func (p *actPending) record(key string, unread bool, feedTS string) uint64 {
	p.seq++
	if p.marks == nil {
		p.marks = map[string]*actPendingMark{}
	}
	p.marks[key] = &actPendingMark{id: p.seq, unread: unread, feedTS: parseFeedTS(feedTS), markedAt: time.Now()}
	return p.seq
}

// recordMarks is pErv ph, the bulk recordPendingActivityMark: a mark of
// each of items (keys[i] is items[i]'s) to unread, and the clear to run
// once their one write answered.
func (v *Activity) recordMarks(keys []string, items []core.ActivityItem, unread bool) func() {
	ids := make(map[string]uint64, len(keys))
	for i, k := range keys {
		ids[k] = v.pending.record(k, unread, items[i].FeedTS)
	}
	return func() {
		for k, id := range ids {
			v.pending.clear(k, id)
		}
	}
}

// clear is clearPendingActivityMark: the write of mark id answered.
func (p *actPending) clear(key string, id uint64) {
	if m := p.marks[key]; m != nil && m.id == id {
		delete(p.marks, key)
	}
}

// reconcile is reconcileReadStateWithPendingMark for a realtime entry of
// key (no request seq): the unread state to take instead of the entry's,
// and whether there is one.
func (p *actPending) reconcile(key string, unread bool, feedTS string, now time.Time) (bool, bool) {
	m := p.marks[key]
	if m == nil {
		return false, false
	}
	if !m.ended && now.Sub(m.markedAt) > pendingMarkLife {
		m.ended = true
	}
	agree := unread == m.unread
	if parseFeedTS(feedTS) > m.feedTS {
		delete(p.marks, key) // newer activity than the mark: the entry stands
		return false, false
	}
	if m.ended {
		if !agree {
			delete(p.marks, key)
		}
		return false, false
	}
	if agree {
		m.ended = true // endMark: the server has it
		return false, false
	}
	return m.unread, true
}

// actReadUpTo is a thread's (thread set) or a conversation's read cursor
// moving to cursor; gen is core's server badge set then
// (ActivityBadgesGen).
type actReadUpTo struct {
	ch, thread, cursor string
	gen                uint64
}

// actReadThread is the thread an item's read follows: YfTA formats the
// dm, channel and mpdm bundles with no threadTs (their latest message may
// be a reply), so a conversation's read reads them.
func actReadThread(it *core.ActivityItem) string {
	switch it.Type {
	case "dm", "channel", "mpdm":
		return ""
	}
	return it.ThreadTS
}

// readUpTo is updateUnreadActivityItemByThreadKey (r.thread) and the
// channel read (getChannelReadUpdatesByTimestamp) over the loaded page:
// each unread item of the thread (of the conversation, outside threads)
// whose message is at or before the cursor is read here alone, and off
// the badges. Only under the activity inbox (getOptimisticUpdates
// returns nothing without it). The mark is local, as the badges' zero's
// is (zeroRead): a load that went out after it brings the server's state.
func (v *Activity) readUpTo(p *core.ActivityPage, r actReadUpTo) {
	c := v.h.Core()
	if c == nil || !c.ActivityInbox() {
		return
	}
	var read []core.ActivityItem
	for i := range p.Items {
		it := &p.Items[i]
		key := actKey(it)
		if it.Channel != r.ch || !v.isUnread(it, key) {
			continue
		}
		if actReadThread(it) != r.thread { // a thread's items for a thread, the rest for the conversation
			continue
		}
		ts := actMessageTS(it)
		if ts == "" || actTSLess(r.cursor, ts) {
			continue
		}
		if read, ok := v.override[key]; ok && !read {
			delete(v.override, key) // marked unread here: the read is newer
		}
		if v.zeroRead == nil {
			v.zeroRead = map[string]uint64{}
		}
		v.zeroRead[key] = v.loads
		cp := *it
		cp.Unread = true // decrementBadgeCounts counts the items that were unread
		read = append(read, cp)
		v.dirty = true
	}
	if len(read) > 0 {
		// taken when the view applies it, which may be well after the
		// read (Activity undrawn): a server set installed since (a
		// badge_counts_updated, a client.counts answer) counts it already
		c.DecActivityBadges(read, r.gen)
	}
}
