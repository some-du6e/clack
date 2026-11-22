package views

// The rows' messages as they are now.
//
// Slack's Activity rows keep no copy of their message. Each one reads it
// from the messages store as it renders (getMessageFromActivityItem,
// boot-shared Gjwd uU: getMessageByTimestamp, client-boot iR00 Ld, on the
// entry's channel and feed_ts, a reaction's message_ts, a reminder's
// message; ActivityThread, ActivityDM, ActivityMention and
// ActivityReaction read getMessageByTimestamp the same way). activity.feed
// names the messages and messages.list fetched them into that store, the
// one the channel list and the thread pane read, so the socket's changes
// reach an open Activity page in the same commit as those, and nothing
// that happened meanwhile (the page off screen, a load in flight, a pane
// closing) can leave a row on an older revision:
//
//   - message_changed (boot-render messageChanged, replaceMessages): the
//     row draws the new copy (a vote's chat.update, an edit). A copy that
//     is a tombstone, or a moderated or thread_hidden message, and an edit
//     that took the viewer's mention out (checkForMentionInMessage, c+16,
//     of previous_message and the new copy) remove the entries about it
//     ("ActivityIA4: Remove activity item" {channelId, messageTs}, P0K2).
//   - reaction_added / reaction_removed: the reactions reducer changes the
//     stored message's reactions (the reaction row's, "You reacted").
//   - message_deleted (messageDeleted: setMessageDeleted, then the same
//     Remove activity item): the entries whose messageTs it was go, and a
//     row that only drew it (a thread's latest reply) draws no message.
//
// None of them fetches the feed again. A message the store doesn't hold
// yet is fetched by id, and the row draws the loading shimmer meanwhile
// (isUnknown) and no body once Slack said there is none (isNonExistent:
// the item's MsgGone). Clack's feed answer comes hydrated, so the feed's
// copy is that fetch's.
//
// Clack keeps one copy of each page message, the newest by the order core
// changed messages in (core's MessageChangeSeq), wherever it came from:
// the feed's (as of the item's MsgSeq), a change core made after that
// (MessageChange, read before the frame describes and draws the rows), or
// the store's when a message list stops drawing it. A row draws that
// copy, except while a message list on screen draws the message (the
// Host's LiveMessages: the thread pane or the channel list beside the
// page, read as the row draws): then it draws the list's revision, so the
// two never show two revisions in one frame. A list's copy going away
// (the pane closing) leaves the newest copy, never an older one.

import (
	"sync/atomic"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/slackc"
)

// actMsgs is what Activity knows of its page's messages.
type actMsgs struct {
	// own is the copy of a page message core changed after the feed read
	// it, or the store's when a list stopped drawing it, with the change
	// it is current as of (m nil: deleted)
	own map[actMsgKey]actCopy
	// feed is each page message's newest copy an item brought (as of its
	// MsgSeq)
	feed map[actMsgKey]actCopy
	// hub is the copy a message list on screen drew in the frame it was
	// polled (pollLists)
	hub map[actMsgKey]actHub
	// refs are the page's messages: what each item draws (its message and
	// its thread's root) and its messageTs (what Remove activity item
	// matches); page is the Items they were read from
	refs map[actMsgKey]actRef
	page []core.ActivityItem
	sel  string // the in-view preview's item they were read with
	// seen is core's message changes as far as Activity has taken them
	seen    uint64
	started bool
	// stale are page messages core forgot the changes of (the page further
	// behind than its table remembers): each takes the store's copy, as of
	// the change, when a row first draws it (msgFor), so no frame converts
	// and describes the whole page again
	stale map[actMsgKey]uint64
	// refetch are the messages this frame found no copy of, by channel:
	// fetched again by id in one request each once the frame has read the
	// changes (flushRefetch)
	refetch map[string][]string
	// since is the previous frame's time and last this one's: the lists
	// drawn since the previous frame are on screen
	since, last time.Time
	// keys are refs, for the event goroutine (wakes)
	keys atomic.Pointer[map[actMsgKey]bool]
}

// actMsgKey names a message: its channel and ts.
type actMsgKey struct{ ch, ts string }

// actRef is what the page does with a message: draws it (an item's
// message or its thread's root), or removes the items it is the
// messageTs of.
type actRef uint8

const (
	actRefDraws actRef = 1 << iota
	actRefRemoves
)

// actCopy is a copy of a message as of change seq; m nil when it was
// deleted (own) or Slack said there is none (feed: gone).
type actCopy struct {
	m    *core.Message
	seq  uint64
	gone bool
}

type actHub struct {
	m     *core.Message
	rev   uint64
	frame time.Time // the frame a list drew it in (polled)
}

// The sources of a row's copy in actRow.liSrc: a list's is its revision
// there (below both bits), the feed's item's own 0.
const (
	actOwnSrc  = 1 << 63 // own: core's change seq, or the store's
	actFeedSrc = 1 << 62 // another item's newer copy, by its MsgSeq
)

// actMsgRef is where it's message is and its thread (ok false for an
// item without one): the row's message, hydrated from it.TS.
func actMsgRef(it *core.ActivityItem) (k actMsgKey, thread string, ok bool) {
	k, thread = actMsgKey{it.Channel, it.TS}, it.ThreadTS
	if m := it.Msg; m != nil {
		k.ts, thread = m.TS, m.ThreadTS
		if m.Channel != "" {
			k.ch = m.Channel
		}
	}
	return k, thread, k.ch != "" && k.ts != ""
}

// actRootRef is where it's thread root is (ok false for an item about no
// thread's reply).
func actRootRef(it *core.ActivityItem) (k actMsgKey, ok bool) {
	k = actMsgKey{it.Channel, it.ThreadTS}
	if it.Root != nil {
		k.ts = it.Root.TS
	}
	return k, k.ch != "" && k.ts != "" && (it.Root != nil || k.ts != it.TS)
}

// actMessageTS is Slack's activityItem.messageTs (client-boot /29I): the
// message an entry is about, what "Remove activity item" matches. A
// thread bundle's is its first unread reply, else its latest; the
// entries without a channelId (lists, external invites, reminders,
// calendar events, system alerts) have none.
func actMessageTS(it *core.ActivityItem) string {
	switch {
	case it.Type == "thread_v2":
		return firstNonEmpty(it.MinUnreadTS, it.LatestTS, it.FeedTS)
	case it.Type == "quietly_added_to_channel", it.Type == "prejoin_dm_welcome_party_alert":
		return it.FeedTS
	case actExternal(it.Type), actIsList(it.Type), it.Type == "saved_reminder", it.Type == "calendar_upcoming_event",
		it.Type == "generic_system_alert", it.Type == "example_user_alert":
		return ""
	}
	return firstNonEmpty(it.TS, it.FeedTS)
}

// wakes reports a change of a page message (an "update", a "delete"):
// the event goroutine draws a frame for it, which reads it from core
// (applyMsgs).
func (m *actMsgs) wakes(ev core.Event) bool {
	if ev.Type != "update" && ev.Type != "delete" && ev.Type != "gone" || ev.Channel == "" || ev.TS == "" {
		return false
	}
	keys := m.keys.Load()
	return keys != nil && (*keys)[actMsgKey{ev.Channel, ev.TS}]
}

// startMsgs begins taking core's message changes from now (the first
// load: its items are current as of then).
func (v *Activity) startMsgs() {
	if m := &v.msgs; !m.started {
		if c := v.h.Core(); c != nil {
			m.started, m.seen = true, c.MessageChangeSeq()
		}
	}
}

// applyMsgs brings the page's messages up to date before this frame
// describes and draws the rows (Layout, LayoutPeek): items that came
// since the last frame (a load, a realtime entry) take what core changed
// after they were read, and every page message what core changed since
// the last frame, in the order core made the changes.
func (v *Activity) applyMsgs(gtx C) {
	m := &v.msgs
	if !gtx.Now.Equal(m.last) {
		m.since, m.last = m.last, gtx.Now
		v.expireLists()
	}
	c := v.h.Core()
	if c == nil || !m.started {
		return
	}
	sel := ""
	if v.selOK {
		sel = v.selKey
	}
	defer v.flushRefetch(c)
	if page, _, _ := v.data.get(); !actSameItems(page.Items, m.page) || sel != m.sel {
		v.landMsgs(c, &page)
		m.sel = sel
	}
	if c.MessageChangeSeq() == m.seen {
		return
	}
	keys, now, ok := c.MessageChangedSince(m.seen)
	m.seen = now
	if !ok {
		// further behind than core names (the page off screen through a
		// busy while): every page message is looked up; one core forgot
		// since its copy was read may have missed a change, and the page
		// is read again when one is gone from the store
		floor, gone := c.MessageChangeFloor(), false
		for k, ref := range m.refs {
			known, missing := v.takeChange(c, k, true, false)
			if missing {
				gone = true // no copy of its change: the reload reads it
				continue
			}
			if known || v.copyOf(k).seq >= floor {
				continue
			}
			// core forgot whether it changed: the store's copy, which the
			// socket kept current, is the message as it is now (on every
			// page loaded, not just the first a reload reads), taken when
			// a row draws it (msgFor)
			if _, ok := c.S.FindHeld(k.ch, k.ts); !ok {
				gone = true // deleted, or let go: the feed says which
			} else if ref&actRefDraws != 0 {
				if m.stale == nil {
					m.stale = map[actMsgKey]uint64{}
				}
				m.stale[k] = now
			}
		}
		if len(m.stale) > 0 && v.searchOn && v.query.text() != "" {
			v.dirty = true // the search filters on the rows' text: all are described again
		}
		if gone && !(v.data.busy() && v.mode == actFresh) {
			v.load(actRefresh)
		}
		return
	}
	for i, k := range keys {
		if m.refs[actMsgKey{k.Channel, k.TS}] != 0 && (i == 0 || k != keys[i-1]) {
			v.takeChange(c, actMsgKey{k.Channel, k.TS}, true, true)
		}
	}
}

// queueRefetch asks for k again by id once the frame has read core's
// changes (flushRefetch): one messages.list per channel a frame.
func (v *Activity) queueRefetch(k actMsgKey) {
	m := &v.msgs
	if m.refetch == nil {
		m.refetch = map[string][]string{}
	}
	m.refetch[k.ch] = append(m.refetch[k.ch], k.ts)
}

// flushRefetch fetches again the messages the frame found no copy of
// (core.RefetchMessages: it skips one already out or backing off after a
// failure, and settles one Slack answers without as gone).
func (v *Activity) flushRefetch(c *core.App) {
	m := &v.msgs
	for ch, tss := range m.refetch {
		c.RefetchMessages(ch, tss)
	}
	clear(m.refetch)
}

// actSameItems reports whether a and b are the same Items slice (every
// change to the page makes a new one).
func actSameItems(a, b []core.ActivityItem) bool {
	return len(a) == len(b) && (len(a) == 0 || &a[0] == &b[0])
}

// landMsgs reads the page's messages from its items, and has each item
// take what core changed of them after the item was read (its MsgSeq): a
// realtime entry announced before a change the page took in a frame
// before it was upserted, a load's answer read before a change, the
// rows a load's answer joins (actUpsertPage), and the entries core's
// changes removed meanwhile, which stay out.
func (v *Activity) landMsgs(c *core.App, page *core.ActivityPage) {
	m := &v.msgs
	refs := make(map[actMsgKey]actRef, 3*len(page.Items)+3)
	feed := make(map[actMsgKey]actCopy, 2*len(page.Items)+2)
	newer := func(k actMsgKey, msg *core.Message, gone bool, seq uint64) {
		refs[k] |= actRefDraws
		f, had := feed[k]
		known := msg != nil || gone
		if !known {
			seq = 0 // its fetch failed: no copy, older than any there is
		}
		if !had || known && (seq > f.seq || f.m == nil && !f.gone) {
			feed[k] = actCopy{m: msg, seq: seq, gone: gone}
		}
	}
	note := func(it *core.ActivityItem) {
		if k, _, ok := actMsgRef(it); ok {
			newer(k, it.Msg, it.MsgGone, it.MsgSeq)
		}
		if k, ok := actRootRef(it); ok {
			newer(k, it.Root, it.RootGone, it.MsgSeq)
		}
		if ts := actMessageTS(it); ts != "" && it.Channel != "" {
			refs[actMsgKey{it.Channel, ts}] |= actRefRemoves
		}
	}
	for i := range page.Items {
		note(&page.Items[i])
	}
	if v.selOK {
		note(&v.sel) // the in-view preview draws it
	}
	m.refs, m.feed, m.page = refs, feed, page.Items
	keys := make(map[actMsgKey]bool, len(refs))
	drawn := map[string][]string{}
	for k, ref := range refs {
		keys[k] = true
		if ref&actRefDraws == 0 {
			continue
		}
		drawn[k.ch] = append(drawn[k.ch], k.ts)
		if f := feed[k]; f.m == nil && !f.gone {
			// an item with no copy (its fetch failed): the store's, if it
			// has one, rather than a shimmer; else it is fetched again by
			// id, and draws what that brings, or no body once Slack says
			// there is none (gone)
			if _, ok := m.own[k]; !ok {
				if sm, ok := c.HeldMessage(k.ch, k.ts); ok {
					v.adopt(k, &sm, c.MessageChangeSeq())
				} else {
					v.queueRefetch(k)
				}
			}
		}
	}
	m.keys.Store(&keys)
	// the page's messages are in use: the store lets go of others first
	for ch, tss := range drawn {
		c.S.KeepFetched(ch, tss)
	}
	for k := range m.own {
		if refs[k] == 0 {
			delete(m.own, k)
		}
	}
	for k := range m.hub {
		if refs[k] == 0 {
			delete(m.hub, k)
		}
	}
	for k := range m.stale {
		if refs[k] == 0 {
			delete(m.stale, k)
		}
	}
	// what changed after each item was read
	removed := false
	for i := range page.Items {
		it := &page.Items[i]
		if ts := actMessageTS(it); ts != "" && it.Channel != "" {
			if ch, ok := c.MessageChange(it.Channel, ts); ok && ch.RemoveSeq > it.MsgSeq {
				removed = true
			}
		}
		if k, _, ok := actMsgRef(it); ok {
			v.takeChange(c, k, false, true)
		}
		if k, ok := actRootRef(it); ok {
			v.takeChange(c, k, false, true)
		}
	}
	if removed {
		v.dropRemoved(func(it *core.ActivityItem, ts string) bool {
			ch, ok := c.MessageChange(it.Channel, ts)
			return ok && ch.RemoveSeq > it.MsgSeq
		})
	}
}

// copyOf is the newest copy of k Activity holds: own's, else the feed's.
func (v *Activity) copyOf(k actMsgKey) actCopy {
	m := &v.msgs
	f := m.feed[k]
	if o, ok := m.own[k]; ok && o.seq > f.seq {
		return o
	}
	return f
}

// takeChange takes core's latest change of k when it's newer than the
// copy Activity holds: the copy is the change's, and with drops the
// entries it removed go. A change no copy is left of is fetched again by
// id with refetch (else the caller reads the page again). It reports
// whether core knows of a change to k, and whether no copy of it was to
// be had.
func (v *Activity) takeChange(c *core.App, k actMsgKey, drops, refetch bool) (known, missing bool) {
	ch, ok := c.MessageChange(k.ch, k.ts)
	if !ok {
		return false, false
	}
	m := &v.msgs
	ref := m.refs[k]
	if drops && ref&actRefRemoves != 0 && ch.RemoveSeq > 0 {
		v.dropRemoved(func(it *core.ActivityItem, ts string) bool {
			return it.Channel == k.ch && ts == k.ts && ch.RemoveSeq > it.MsgSeq
		})
	}
	cur := v.copyOf(k)
	if ref&actRefDraws == 0 || ch.Seq <= cur.seq {
		return true, false
	}
	if m.own == nil {
		m.own = map[actMsgKey]actCopy{}
	}
	if ch.Deleted {
		m.own[k] = actCopy{seq: ch.Seq}
	} else if msg, ok := c.ChangedMessage(k.ch, k.ts, &ch, cur.m, cur.seq); ok {
		m.own[k] = actCopy{m: &msg, seq: ch.Seq}
	} else {
		// no copy to be had (the store let it go, and core's copy of the
		// change with it): fetched again by id, as Slack's row fetches a
		// message its store doesn't hold; it comes back as a change
		if refetch {
			v.queueRefetch(k)
		}
		return true, true
	}
	delete(m.stale, k) // the change is newer
	v.unbuildMsg(k)
	return true, false
}

// adopt makes msg, current as of change seq, Activity's copy of k.
func (v *Activity) adopt(k actMsgKey, msg *core.Message, seq uint64) {
	m := &v.msgs
	if m.own == nil {
		m.own = map[actMsgKey]actCopy{}
	}
	m.own[k] = actCopy{m: msg, seq: seq}
	v.unbuildMsg(k)
}

// dropRemoved is "ActivityIA4: Remove activity item" {channelId,
// messageTs} (P0K2), which messageDeleted and messageChanged dispatch:
// the entries gone says a change after they were read removed (given
// each one's messageTs) leave the list (a new Items slice: a load in
// flight may still read the old one). An entry read after the change
// stays.
func (v *Activity) dropRemoved(gone func(it *core.ActivityItem, messageTS string) bool) {
	v.data.mutate(func(p *core.ActivityPage) {
		var kept []core.ActivityItem
		for i := range p.Items {
			it := &p.Items[i]
			ts := actMessageTS(it)
			if ts != "" && it.Channel != "" && gone(it, ts) {
				if kept == nil {
					kept = append(make([]core.ActivityItem, 0, len(p.Items)), p.Items[:i]...)
				}
				continue
			}
			if kept != nil {
				kept = append(kept, *it)
			}
		}
		if kept != nil {
			p.Items = kept
			v.dirty = true
		}
	})
}

// unbuildMsg describes again the rows that draw the message k.
func (v *Activity) unbuildMsg(k actMsgKey) {
	page, _, _ := v.data.get()
	for i := range page.Items {
		it := &page.Items[i]
		mk, _, mok := actMsgRef(it)
		rk, rok := actRootRef(it)
		if !(mok && mk == k) && !(rok && rk == k) {
			continue
		}
		if r := v.rows[actKey(it)]; r != nil {
			r.built, r.tm = false, nil
		}
		v.dirty = true
	}
}

// msgFor is the copy of k a row draws in this frame, and where it's from
// (actRow.liSrc): a list's on screen, else the newest by core's order of
// the copies Activity holds and the item's own (feed, current as of
// change seq: source 0); nil when deleted, or none exists. Another
// item's copy or a change's takes the item's place only when newer.
func (v *Activity) msgFor(k actMsgKey, feed *core.Message, seq uint64) (*core.Message, uint64) {
	return v.msgForAdopt(k, feed, seq, true)
}

// msgForAdopt is msgFor; with adopt false a message core forgot the
// changes of (stale) keeps the copy Activity holds, its store copy taken
// only once a row draws it: a rebuild describes every row, drawn or not,
// and converting them all there was a page-wide conversion in one frame.
func (v *Activity) msgForAdopt(k actMsgKey, feed *core.Message, seq uint64, adopt bool) (*core.Message, uint64) {
	m := &v.msgs
	if h, ok := m.hub[k]; ok && (h.frame.Equal(m.last) || h.frame.Equal(m.since)) {
		// a list on screen: this frame's copy once the row polled it,
		// the last frame's before (rebuild describes the rows before they
		// poll, and must describe them from what they will draw)
		return h.m, h.rev
	}
	if at, ok := m.stale[k]; ok && adopt {
		// core forgot this message's changes: the store's copy, as of the
		// pass that found it (applyMsgs), taken now a row draws it
		delete(m.stale, k)
		if o, had := m.own[k]; !had || o.seq < at {
			if c := v.h.Core(); c != nil {
				if sm, ok := c.HeldMessage(k.ch, k.ts); ok {
					if m.own == nil {
						m.own = map[actMsgKey]actCopy{}
					}
					m.own[k] = actCopy{m: &sm, seq: at}
				}
			}
		}
	}
	best, bestSeq, src := feed, seq, uint64(0)
	if f, ok := m.feed[k]; ok && f.seq > seq && f.m != feed && (f.m != nil || f.gone) {
		best, bestSeq, src = f.m, f.seq, actFeedSrc|f.seq
	}
	if o, ok := m.own[k]; ok && o.seq > bestSeq {
		return o.m, actOwnSrc | o.seq
	}
	return best, src
}

// rowItem is it as row r shows it (getMessageFromActivityItem): its
// message and thread root the copies msgFor has; r.liSrc says which, and
// r.msgGone that it has no message to draw (deleted, or none exists). it
// itself when both are its own.
func (v *Activity) rowItem(r *actRow, it *core.ActivityItem) *core.ActivityItem {
	return v.rowItemAdopt(r, it, true)
}

// rowItemAdopt is rowItem; with adopt false it converts no store copy of a
// message core forgot the changes of (msgForAdopt): the rows a rebuild
// describes and the list doesn't draw.
func (v *Activity) rowItemAdopt(r *actRow, it *core.ActivityItem, adopt bool) *core.ActivityItem {
	r.liSrc, r.msgGone = [2]uint64{}, it.MsgGone
	m := &v.msgs
	if len(m.own) == 0 && len(m.hub) == 0 && len(m.feed) == 0 && len(m.stale) == 0 {
		return it
	}
	msg, root := it.Msg, it.Root
	if k, _, ok := actMsgRef(it); ok {
		msg, r.liSrc[0] = v.msgForAdopt(k, it.Msg, knownSeq(it.Msg, it.MsgGone, it.MsgSeq), adopt)
		r.msgGone = msg == nil && (r.liSrc[0] != 0 || it.MsgGone)
	}
	if k, ok := actRootRef(it); ok {
		root, r.liSrc[1] = v.msgForAdopt(k, it.Root, knownSeq(it.Root, it.RootGone, it.MsgSeq), adopt)
	}
	if r.liSrc == [2]uint64{} {
		return it
	}
	r.li = *it
	r.li.Msg, r.li.Root = msg, root
	return &r.li
}

// knownSeq is the change an item's copy of a message is current as of: 0
// when it has none and doesn't know there is none (its fetch failed), so
// any copy Activity holds outranks it.
func knownSeq(m *core.Message, gone bool, seq uint64) uint64 {
	if m == nil && !gone {
		return 0
	}
	return seq
}

// liveCopy is rowItem for an item drawn outside a row (the in-view
// preview, a key's message): buf holds the copy.
func (v *Activity) liveCopy(it *core.ActivityItem, buf *actRow) *core.ActivityItem {
	if it == nil {
		return nil
	}
	return v.rowItem(buf, it)
}

// pollLists reads it's message and thread root from the message lists
// on screen (LiveMessages) as its row draws: the revision they draw this
// frame. It reports whether one changed.
func (v *Activity) pollLists(it *core.ActivityItem) bool {
	lm, ok := v.h.(LiveMessages)
	m := &v.msgs
	if !ok || m.since.IsZero() {
		return false
	}
	changed := false
	poll := func(k actMsgKey, thread string) {
		h, had := m.hub[k]
		if had && h.frame.Equal(m.last) {
			return // another row found it this frame: one copy a frame
		}
		msg, rev, ok := lm.LiveMessage(k.ch, k.ts, thread, m.since, h.rev)
		switch {
		case !ok:
			// no list holds it for this lookup: the copy goes once no
			// row's lookup finds it in a frame (expireLists), not at one
			// that missed what another row's found
		case had && rev == h.rev:
			h.frame = m.last
			m.hub[k] = h
		default:
			if m.hub == nil {
				m.hub = map[actMsgKey]actHub{}
			}
			c := msg
			m.hub[k] = actHub{m: &c, rev: rev, frame: m.last}
			changed = true
		}
	}
	if k, thread, ok := actMsgRef(it); ok {
		// the thread the copy the row draws names: one hydrated before
		// the message had replies names none, a change since its root's
		if cur, _ := v.msgFor(k, it.Msg, knownSeq(it.Msg, it.MsgGone, it.MsgSeq)); cur != nil && cur.ThreadTS != "" {
			thread = cur.ThreadTS
		}
		poll(k, thread)
	}
	if k, ok := actRootRef(it); ok {
		poll(k, k.ts)
	}
	return changed
}

// expireLists lets go of the lists' copies no row polled in the previous
// frame (their rows scrolled away, the lists closed).
func (v *Activity) expireLists() {
	m := &v.msgs
	for k, h := range m.hub {
		if h.frame.Before(m.since) {
			v.listGone(k)
		}
	}
}

// listGone is a list's copy of k going (the pane closed, the row scrolled
// away): the store's copy, which has everything the list drew, becomes
// Activity's, as of now (a list can hold a revision no change carried:
// its thread read again after a socket gap), taken when a row next draws
// it (msgFor's stale), so a row scrolling off converts nothing and
// describes no page again. Nothing older takes its place.
func (v *Activity) listGone(k actMsgKey) {
	m := &v.msgs
	delete(m.hub, k)
	c := v.h.Core()
	if c == nil || m.refs[k]&actRefDraws == 0 {
		return
	}
	if ch, ok := c.MessageChange(k.ch, k.ts); ok && ch.Deleted {
		return // deleted: own has it
	}
	if m.stale == nil {
		m.stale = map[actMsgKey]uint64{}
	}
	m.stale[k] = c.MessageChangeSeq()
	if v.searchOn && v.query.text() != "" {
		v.dirty = true // the search filters on the rows' text: all are described again
	}
}

// ---- what the row's patched components were sent ----

// sentSet is what a row sent one level of its patched components of its
// message, by content, and the content of that level's tree's msg it
// read last. Each level is judged by its own: under Slick Censorship's
// ActivityItem mask and BetterBlock's inner patches, the masked msg the
// row hands ActivityThread and the RowContent components is the plugin's
// text, and taken for props the row sent ActivityItem it undid the mask
// from the second frame on (plugin-render-3's per-level sets in
// messages/row.go: outSent, inSent, bodySent).
type sentSet struct {
	fps  slackc.SentFPs
	last *core.Message
	text string
	tree slackc.FPMemo
}

// send is msgProps(m) for the level's component, noting what it says.
func (s *sentSet) send(m *core.Message) slackc.Props {
	p := msgProps(m)
	if m != nil && (m != s.last || s.text != m.Text) {
		s.last, s.text = m, m.Text
		s.fps.Add(slackc.ContentFP(p))
	}
	return p
}

// echoed reports a tree's msg (mp) that says what a msg the row sent this
// level says: JS passed the props on unchanged, maybe from a revision
// before the one the row draws now, so the row draws its own copy, not
// that revision's text (row.overlay's sent rule in the message lists).
func (s *sentSet) echoed(mp slackc.Props) bool {
	return mp != nil && s.fps.Has(s.tree.Of(mp))
}
