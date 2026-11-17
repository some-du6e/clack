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
// reach an open Activity page in the same commit as those:
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
// yet is fetched by id and the row draws without it meanwhile; Clack's
// feed answer comes hydrated, so the feed's copy is that fetch's.
//
// A row here reads the newest copy Activity has: the one a message list
// on screen draws (the Host's LiveMessages: the thread pane or the
// channel list beside the page, read as the row draws, so the two never
// show two revisions in one frame), else the one the socket brought last
// (core's "update" and "delete" of a message on the page, read from its
// log of message changes before the frame describes and draws the rows:
// core.MessageChanges), else the feed's.

import (
	"sync/atomic"
	"time"

	"github.com/some-du6e/clack/internal/core"
)

// actMsgs is what Activity knows of its page's messages since the feed
// brought them.
type actMsgs struct {
	// live is the copy of a page message the socket brought last (m nil:
	// deleted), with the order it came in
	live map[actMsgKey]actLive
	// hub is the copy a message list on screen holds (LiveMessages), with
	// its revision there
	hub map[actMsgKey]actHub
	// feed is the page's own copy of each message
	feed map[actMsgKey]*core.Message
	seq  uint64 // the socket's changes applied
	// seen is core's message change log as far as Activity has read it
	// (core.MessageChanges), from its first load on
	seen    uint64
	started bool
	// since is the previous frame's time and last this one's: the lists
	// drawn since the previous frame are on screen
	since, last time.Time
	// keys are the page's messages, for the event goroutine (wakes)
	keys atomic.Pointer[map[actMsgKey]bool]
}

// actMsgKey names a message: its channel and ts.
type actMsgKey struct{ ch, ts string }

type actLive struct {
	m   *core.Message // nil: deleted
	seq uint64
}

type actHub struct {
	m   *core.Message
	rev uint64
}

// actLiveSrc tags a copy from the socket in actRow.liSrc (a list's is its
// revision there, below this bit; the feed's 0).
const actLiveSrc = 1 << 63

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

// actLeaves reports a copy whose entries Slack's messageChanged takes out
// of Activity: a tombstone (R+b2 isTombstone), a moderated or
// thread_hidden message (DRL6 isModerated).
func actLeaves(m *core.Message) bool {
	return m.Tombstone || m.Subtype == "tombstone" || m.Subtype == "moderated" || m.Subtype == "thread_hidden"
}

// wakes reports a change of a page message (an "update", a "delete"):
// the event goroutine draws a frame for it, which reads it from core's
// log (applyMsgs).
func (m *actMsgs) wakes(ev core.Event) bool {
	if ev.Type != "update" && ev.Type != "delete" || ev.Channel == "" || ev.TS == "" {
		return false
	}
	keys := m.keys.Load()
	return keys != nil && (*keys)[actMsgKey{ev.Channel, ev.TS}]
}

// startMsgs begins reading core's message changes from now (the first
// load: its answer is read after them).
func (v *Activity) startMsgs() {
	if m := &v.msgs; !m.started {
		if c := v.h.Core(); c != nil {
			m.started, m.seen = true, c.MessageChangeSeq()
		}
	}
}

// applyMsgs takes core's changes to the page's messages since the last
// frame (core.MessageChanges, in the order core made them) before this
// frame describes and draws the rows (Layout, LayoutPeek). While a load
// is out every change is kept: its answer may bring any message, read
// before the change.
func (v *Activity) applyMsgs(gtx C) {
	m := &v.msgs
	if !gtx.Now.Equal(m.last) {
		m.since, m.last = m.last, gtx.Now
	}
	c := v.h.Core()
	if c == nil || !m.started || c.MessageChangeSeq() == m.seen {
		return
	}
	evs, now, ok := c.MessageChanges(m.seen)
	m.seen = now
	if !ok {
		v.rereadMsgs()
		return
	}
	keys := m.keys.Load()
	for i := range evs {
		if ev := &evs[i]; v.wasBusy || keys != nil && (*keys)[actMsgKey{ev.Channel, ev.TS}] {
			v.applyMsg(ev)
		}
	}
}

// rereadMsgs reads every page message from the store again, after more
// changes than core's log keeps went by unread (the page off screen a
// while): the store holds them as they are now.
func (v *Activity) rereadMsgs() {
	c := v.h.Core()
	m := &v.msgs
	if m.live == nil {
		m.live = map[actMsgKey]actLive{}
	}
	for k := range m.feed {
		sm, ok := c.S.FindHeld(k.ch, k.ts)
		if !ok {
			continue
		}
		cm := c.MessageFromSlack(k.ch, &sm)
		m.seq++
		m.live[k] = actLive{m: &cm, seq: m.seq}
	}
	for _, r := range v.rows {
		r.built, r.tm = false, nil
	}
	v.dirty = true
}

// applyMsg is one change of a message (UI goroutine).
func (v *Activity) applyMsg(ev *core.Event) {
	m := &v.msgs
	if m.live == nil {
		m.live = map[actMsgKey]actLive{}
	}
	k := actMsgKey{ev.Channel, ev.TS}
	had := v.ownMsg(k)
	m.seq++
	if ev.Type == "delete" {
		m.live[k] = actLive{seq: m.seq}
		v.dropEntriesFor(ev.Channel, ev.TS)
	} else {
		c := *ev.Msg
		if ev.Name == core.UpdateReactions && had != nil {
			// the reactions reducer touches nothing else: the rest stays
			// the page's copy (the event's can be another conversation's)
			h := *had
			h.Reactions = c.Reactions
			c = h
		}
		m.live[k] = actLive{m: &c, seq: m.seq}
		if actLeaves(&c) || ev.Name != core.UpdateReactions && v.unmentioned(had, &c) {
			v.dropEntriesFor(ev.Channel, ev.TS)
		}
	}
	v.unbuildMsg(k)
}

// ownMsg is the copy of k the page had before the lists' (the socket's,
// else the feed's): what previous_message is to messageChanged.
func (v *Activity) ownMsg(k actMsgKey) *core.Message {
	if l, ok := v.msgs.live[k]; ok {
		return l.m
	}
	return v.msgs.feed[k]
}

// unmentioned reports an edit that took the viewer's mention out of a
// message: checkForMentionInMessage of the copy before, and not of now.
func (v *Activity) unmentioned(before, now *core.Message) bool {
	c := v.h.Core()
	return before != nil && c != nil && c.MentionsViewer(before) && !c.MentionsViewer(now)
}

// dropEntriesFor is "ActivityIA4: Remove activity item" {channelId,
// messageTs} (P0K2), which messageDeleted and messageChanged dispatch:
// every entry about the message leaves the list (a new Items slice: a
// load in flight may still read the old one).
func (v *Activity) dropEntriesFor(ch, ts string) {
	if ch == "" || ts == "" {
		return
	}
	v.data.mutate(func(p *core.ActivityPage) {
		var kept []core.ActivityItem
		for i := range p.Items {
			it := &p.Items[i]
			if it.Channel == ch && actMessageTS(it) == ts {
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

// noteMsgs records the page's messages (rebuild): the keys the event
// goroutine draws a frame for and the feed's copies; what the socket and
// the lists brought for a message no longer on the page goes (unless a
// load is out: its answer may bring it back).
func (v *Activity) noteMsgs(page *core.ActivityPage) {
	m := &v.msgs
	keys := make(map[actMsgKey]bool, 2*len(page.Items)+2)
	feed := make(map[actMsgKey]*core.Message, 2*len(page.Items)+2)
	add := func(it *core.ActivityItem) {
		if k, _, ok := actMsgRef(it); ok {
			keys[k] = true
			if it.Msg != nil {
				feed[k] = it.Msg
			}
		}
		if k, ok := actRootRef(it); ok {
			keys[k] = true
			if it.Root != nil {
				feed[k] = it.Root
			}
		}
	}
	for i := range page.Items {
		add(&page.Items[i])
	}
	if v.selOK {
		add(&v.sel) // the in-view preview draws it
	}
	m.keys.Store(&keys)
	m.feed = feed
	if v.wasBusy {
		return
	}
	for k := range m.live {
		if !keys[k] {
			delete(m.live, k)
		}
	}
	for k := range m.hub {
		if !keys[k] {
			delete(m.hub, k)
		}
	}
}

// msgFor is the newest copy of k Activity has (a list's on screen, the
// socket's: nil when it was deleted, else feed) and where it's from
// (actRow.liSrc).
func (v *Activity) msgFor(k actMsgKey, feed *core.Message) (*core.Message, uint64) {
	m := &v.msgs
	if h, ok := m.hub[k]; ok {
		return h.m, h.rev
	}
	if l, ok := m.live[k]; ok {
		return l.m, actLiveSrc | l.seq
	}
	return feed, 0
}

// rowItem is it as row r shows it (getMessageFromActivityItem): its
// message and thread root the newest copies Activity has (msgFor);
// r.liSrc says which. it itself when both are the feed's.
func (v *Activity) rowItem(r *actRow, it *core.ActivityItem) *core.ActivityItem {
	r.liSrc, r.msgGone = [2]uint64{}, false
	if len(v.msgs.live) == 0 && len(v.msgs.hub) == 0 {
		return it
	}
	msg, root := it.Msg, it.Root
	if k, _, ok := actMsgRef(it); ok {
		msg, r.liSrc[0] = v.msgFor(k, it.Msg)
		r.msgGone = msg == nil && r.liSrc[0] != 0
	}
	if k, ok := actRootRef(it); ok {
		root, r.liSrc[1] = v.msgFor(k, it.Root)
	}
	if r.liSrc == [2]uint64{} {
		return it
	}
	r.li = *it
	r.li.Msg, r.li.Root = msg, root
	return &r.li
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
		msg, rev, ok := lm.LiveMessage(k.ch, k.ts, thread, m.since, h.rev)
		switch {
		case !ok:
			if had {
				delete(m.hub, k)
				changed = true
			}
		case had && rev == h.rev:
		default:
			if m.hub == nil {
				m.hub = map[actMsgKey]actHub{}
			}
			c := msg
			m.hub[k] = actHub{m: &c, rev: rev}
			changed = true
		}
	}
	if k, thread, ok := actMsgRef(it); ok {
		poll(k, thread)
	}
	if k, ok := actRootRef(it); ok {
		poll(k, k.ts)
	}
	return changed
}

// noteSent remembers the texts of the revisions of the row's message
// drawn before this one: a patched component's tree that hands one back
// was made from the props the row sent before (JS hasn't drawn the new
// ones yet), not changed by a plugin (echoed).
func (r *actRow) noteSent(m *core.Message) {
	if m == nil {
		return
	}
	switch {
	case m.Channel != r.sentKey.ch || m.TS != r.sentKey.ts:
		r.sentKey, r.sentN = actMsgKey{m.Channel, m.TS}, 0
	case m.Text != r.sentText:
		copy(r.sent[1:], r.sent[:len(r.sent)-1])
		r.sent[0] = r.sentText
		r.sentN = min(r.sentN+1, len(r.sent))
	}
	r.sentText = m.Text
}

// echoed reports a tree's msg (mp) whose text is that of a revision of
// src the row sent before src: its plugin passed the props on unchanged,
// so the row draws src, not the revision before (row.sentFor in the
// message lists).
func (r *actRow) echoed(src *core.Message, mp map[string]any) bool {
	t, ok := mp["text"].(string)
	if !ok || src == nil || t == src.Text || r.sentKey != (actMsgKey{src.Channel, src.TS}) {
		return false
	}
	for _, s := range r.sent[:r.sentN] {
		if s == t {
			return true
		}
	}
	return false
}
