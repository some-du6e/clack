package views

// The Threads view's messages as they are now.
//
// Slack's Threads view draws its threads with the thread pane's message
// components over the messages store, so an edit, an app's chat.update,
// a reaction or a deletion shows in it in the commit it lands in, as it
// does in the thread pane (Slack's Saved rows read the store the same
// way: LaterItemRow's getMessageByTimestamp). Clack's cards drew the
// copies subscriptions.thread.getView returned until a new reply reloaded
// the view. Core keeps getView's messages where the socket's changes
// reach them now (store/fetched.go), and the view takes each change into
// its threads once, before the frame after core made it draws (core's
// MessageChangedSince and MessageChange), with no reload. A load that
// lands takes what changed after its request went out, so a refresh's
// copies are never older than what the cards drew, nor newer copies
// outranked by older ones.

import (
	"slices"
	"sync/atomic"

	"github.com/some-du6e/clack/internal/core"
)

// viewMsgLog is a view's place in core's message changes.
type viewMsgLog struct {
	seen    uint64
	started bool
}

// start takes the changes from now on (before the view's first load: its
// answer is current as of then).
func (l *viewMsgLog) start(c *core.App) {
	if !l.started && c != nil {
		l.started, l.seen = true, c.MessageChangeSeq()
	}
}

// read names the messages changed since the last read, the change the
// view's copies were current as of before it (prev), and ok false when
// more changed than core names (the view looks its messages up again).
func (l *viewMsgLog) read(c *core.App) (keys []core.MsgKey, prev uint64, ok bool) {
	if c == nil || !l.started || c.MessageChangeSeq() == l.seen {
		return nil, l.seen, true
	}
	prev = l.seen
	keys, l.seen, ok = c.MessageChangedSince(l.seen)
	return keys, prev, ok
}

// thrLive is what the Threads view knows of its messages.
type thrLive struct {
	log viewMsgLog
	// items are the threads keys was read from (the data's)
	items []thrItem
	// keys are the messages the cards show, for the event goroutine
	keys atomic.Pointer[map[actMsgKey]bool]
	// shown: the view drew in the latest frame (a change of its messages
	// draws one only then; off screen the next draw takes it)
	shown shownTracker
	// refetch are the messages a change of which this frame found no copy
	// of, by channel: fetched again by id once the frame has read the
	// changes (core.RefetchMessages), as Activity's
	refetch map[string][]string
}

func (l *thrLive) has(k actMsgKey) bool {
	m := l.keys.Load()
	return m != nil && (*m)[k]
}

// wakes reports a change of a message the view shows (the event
// goroutine): the frame it draws reads it from core.
func (l *thrLive) wakes(ev core.Event) bool {
	if ev.Type != "update" && ev.Type != "delete" && ev.Type != "gone" || ev.Channel == "" || ev.TS == "" {
		return false
	}
	return l.has(actMsgKey{ev.Channel, ev.TS})
}

// noteLiveKeys records the messages the cards show: roots, replies, the
// older replies a card loaded. They're in use: the store lets go of
// others first (KeepFetched), as Activity's.
func (v *Threads) noteLiveKeys() {
	d, _, _ := v.data.get()
	keys := make(map[actMsgKey]bool, 4*len(d.items))
	byCh := map[string][]string{}
	add := func(ch, ts string) {
		k := actMsgKey{ch, ts}
		if !keys[k] {
			keys[k] = true
			byCh[ch] = append(byCh[ch], ts)
		}
	}
	for i := range d.items {
		t := &d.items[i]
		add(t.Channel, t.Root.TS)
		for j := range t.Replies {
			add(t.Channel, t.Replies[j].TS)
		}
		if c := v.cards[t.key]; c != nil {
			for j := range c.older {
				add(t.Channel, c.older[j].TS)
			}
		}
	}
	v.lv.keys.Store(&keys)
	v.lv.items = d.items
	if c := v.h.Core(); c != nil {
		for ch, tss := range byCh {
			c.S.KeepFetched(ch, tss)
		}
	}
}

// refetchLater asks for ch/ts again by id once the frame has read core's
// changes (applyLive).
func (v *Threads) refetchLater(ch, ts string) {
	if v.lv.refetch == nil {
		v.lv.refetch = map[string][]string{}
	}
	v.lv.refetch[ch] = append(v.lv.refetch[ch], ts)
}

// changedCopy is what change ch left of m, a copy current as of seq: the
// change's (ChangedMessage); with none to be had (the store let it go,
// and core's copy of the change with it) m stays and the message is
// fetched again by id, as Slack's row fetches a message its store
// doesn't hold: it comes back as a change.
func (v *Threads) changedCopy(c *core.App, k actMsgKey, ch *core.MsgChange, m *core.Message, seq uint64) (core.Message, bool) {
	nm, ok := c.ChangedMessage(k.ch, k.ts, ch, m, seq)
	if !ok && !ch.Deleted {
		v.refetchLater(k.ch, k.ts)
	}
	return nm, ok
}

// applyLive brings the threads up to date before the frame draws
// (Layout): a load that landed takes what changed after its request went
// out, then every thread what changed since the last frame.
func (v *Threads) applyLive() {
	c := v.h.Core()
	if c == nil {
		return
	}
	defer func() {
		for ch, tss := range v.lv.refetch {
			c.RefetchMessages(ch, tss)
		}
		clear(v.lv.refetch)
	}()
	if d, _, _ := v.data.get(); !thrSameItems(d.items, v.lv.items) {
		v.landLive(c)
	}
	keys, prev, ok := v.lv.log.read(c)
	if !ok {
		// further behind than core names: every thread looks its
		// messages up, on every page the view loaded; past what core
		// remembers, each takes the store's copy (floorLive)
		if c.MessageChangeFloor() > prev {
			v.floorLive(c)
		}
		v.landLive(c)
		return
	}
	// each message the batch names once, with its latest change: a key
	// named again later in the batch, or changed again since, brings
	// nothing older, and every copy older than the change takes it (the
	// change is the same one wherever it lands)
	done := make(map[core.MsgKey]bool, len(keys))
	for _, k := range keys {
		if done[k] || !v.lv.has(actMsgKey{k.Channel, k.TS}) {
			continue
		}
		done[k] = true
		if ch, ok := c.MessageChange(k.Channel, k.TS); ok {
			v.patchThreads(c, actMsgKey{k.Channel, k.TS}, &ch)
		}
	}
}

// floorLive is applyLive further behind than core remembers: a message
// core forgot the changes of whose copy is older than what it forgot
// takes the store's copy (the socket kept it current), in every thread
// and every card's older replies the view loaded; one the store doesn't
// hold is fetched again by id (it comes back as a change, or gone).
// landLive takes the changes core remembers after it.
func (v *Threads) floorLive(c *core.App) {
	floor := c.MessageChangeFloor()
	held := func(ch string, m *core.Message, seq uint64) (core.Message, bool) {
		if seq >= floor {
			return core.Message{}, false
		}
		if _, ok := c.MessageChange(ch, m.TS); ok {
			return core.Message{}, false // landLive takes it
		}
		if sm, ok := c.HeldMessage(ch, m.TS); ok {
			return sm, true
		}
		v.refetchLater(ch, m.TS)
		return core.Message{}, false
	}
	v.data.mutate(func(d *thrData) {
		items := slices.Clone(d.items)
		for i := range items {
			t := &items[i]
			if m, ok := held(t.Channel, &t.Root, t.seq); ok {
				t.Root = m
				v.unbuildThread(t.key)
			}
			var rs []core.Message
			for j := range t.Replies {
				if m, ok := held(t.Channel, &t.Replies[j], t.seq); ok {
					if rs == nil {
						rs = slices.Clone(t.Replies)
					}
					rs[j] = m
				}
			}
			if rs != nil {
				t.Replies = rs
				v.unbuildThread(t.key)
			}
		}
		d.items = items
	})
	for key, card := range v.cards {
		var older []core.Message
		for j := range card.older {
			m := &card.older[j]
			if nm, ok := held(m.Channel, m, card.olderSeq); ok {
				if older == nil {
					older = slices.Clone(card.older)
				}
				older[j] = nm
			}
		}
		if older != nil {
			card.older, card.shown = older, nil
			v.unbuildThread(key)
		}
	}
}

// thrSameItems reports whether a and b are the same threads slice (every
// change to the data makes a new one).
func thrSameItems(a, b []thrItem) bool {
	return len(a) == len(b) && (len(a) == 0 || &a[0] == &b[0])
}

// landLive has each thread take what core changed of its messages after
// its copies were read (thrItem.seq), the cards' older replies too, and
// stamps them current as of now.
func (v *Threads) landLive(c *core.App) {
	now := c.MessageChangeSeq()
	v.data.mutate(func(d *thrData) {
		items := slices.Clone(d.items)
		for i := range items {
			t := &items[i]
			v.landThread(c, t, now)
		}
		d.items = items
	})
	for _, card := range v.cards {
		if card.olderSeq >= now {
			continue
		}
		var out []core.Message
		for j := range card.older {
			m := &card.older[j]
			ch, ok := c.MessageChange(m.Channel, m.TS)
			if !ok || ch.Seq <= card.olderSeq {
				if out != nil {
					out = append(out, *m)
				}
				continue
			}
			if out == nil {
				out = append(make([]core.Message, 0, len(card.older)), card.older[:j]...)
			}
			if nm, ok := v.changedCopy(c, actMsgKey{m.Channel, m.TS}, &ch, m, card.olderSeq); ok {
				out = append(out, nm)
			} else if !ch.Deleted {
				out = append(out, *m)
			}
			card.built = false
		}
		if out != nil {
			card.older, card.shown = out, nil
		}
		card.olderSeq = now
	}
	v.noteLiveKeys()
}

// landThread brings t's copies up to date (newer than t.seq), and stamps
// it current as of now.
func (v *Threads) landThread(c *core.App, t *thrItem, now uint64) {
	if t.seq >= now {
		return
	}
	k := actMsgKey{t.Channel, t.Root.TS}
	if ch, ok := c.MessageChange(k.ch, k.ts); ok && ch.Seq > t.seq && !ch.Deleted {
		if m, ok := v.changedCopy(c, k, &ch, &t.Root, t.seq); ok {
			t.Root = m
			v.unbuildThread(t.key)
		}
	}
	var out []core.Message
	for j := range t.Replies {
		r := &t.Replies[j]
		ch, ok := c.MessageChange(t.Channel, r.TS)
		if !ok || ch.Seq <= t.seq {
			if out != nil {
				out = append(out, *r)
			}
			continue
		}
		if out == nil {
			out = append(make([]core.Message, 0, len(t.Replies)), t.Replies[:j]...)
		}
		if m, ok := v.changedCopy(c, actMsgKey{t.Channel, r.TS}, &ch, r, t.seq); ok {
			out = append(out, m)
		} else if !ch.Deleted {
			out = append(out, *r)
		} else {
			thrUnreadGone(t, j)
		}
		v.unbuildThread(t.key)
	}
	if out != nil {
		t.Replies = out
	}
	t.seq = now
}

// patchThreads takes change ch of k into every thread showing k whose
// copies are older than it (a new threads slice, the thread's replies
// copied once): a reply deleted leaves its card (and its unread count, so
// the New line stays on the first unread reply); a deleted root keeps its
// copy (one with replies comes back as a tombstone's update). A thread's
// stamp stays the point its load was current at: other messages of it can
// have changes older than this one still to take.
func (v *Threads) patchThreads(c *core.App, k actMsgKey, ch *core.MsgChange) {
	v.data.mutate(func(d *thrData) {
		if !thrSameItems(d.items, v.lv.items) {
			return // a load landed: the next frame's landLive takes it all
		}
		var items []thrItem
		for i := range d.items {
			t := &d.items[i]
			if t.Channel != k.ch || t.seq >= ch.Seq {
				continue
			}
			nt, changed := *t, false
			if t.Root.TS == k.ts && !ch.Deleted {
				if m, ok := v.changedCopy(c, k, ch, &t.Root, t.seq); ok {
					nt.Root, changed = m, true
				}
			}
			if j := slices.IndexFunc(t.Replies, func(m core.Message) bool { return m.TS == k.ts }); j >= 0 {
				if m, ok := v.changedCopy(c, k, ch, &t.Replies[j], t.seq); ok {
					rs := slices.Clone(t.Replies)
					rs[j] = m
					nt.Replies, changed = rs, true
				} else if ch.Deleted {
					rs := slices.Clone(t.Replies)
					thrUnreadGone(&nt, j)
					nt.Replies, changed = slices.Delete(rs, j, j+1), true
				}
			}
			if !changed {
				continue
			}
			if items == nil {
				items = slices.Clone(d.items)
			}
			items[i] = nt
			v.unbuildThread(t.key)
		}
		if items != nil {
			d.items = items
			v.lv.items = items // its own change: nothing landed
		}
	})
	for key, card := range v.cards {
		j := slices.IndexFunc(card.older, func(m core.Message) bool { return m.Channel == k.ch && m.TS == k.ts })
		if j < 0 || card.olderSeq >= ch.Seq {
			continue
		}
		var older []core.Message
		if m, ok := v.changedCopy(c, k, ch, &card.older[j], card.olderSeq); ok {
			older = slices.Clone(card.older)
			older[j] = m
		} else if ch.Deleted {
			older = slices.Delete(slices.Clone(card.older), j, j+1)
		} else {
			continue // fetched again: it comes back as a change
		}
		card.older, card.shown = older, nil
		v.unbuildThread(key)
	}
}

// thrUnreadGone is the reply at j of t's replies leaving: when it was one
// of the unread ones (the last UnreadCount), the count drops with it.
func thrUnreadGone(t *thrItem, j int) {
	if t.UnreadCount > 0 && j >= len(t.Replies)-t.UnreadCount {
		t.UnreadCount--
	}
}

// unbuildThread makes the header of the card key again (its people, its
// "Show N more replies").
func (v *Threads) unbuildThread(key string) {
	if c := v.cards[key]; c != nil {
		c.built = false
	}
}
