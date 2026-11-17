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
}

func (l *thrLive) has(k actMsgKey) bool {
	m := l.keys.Load()
	return m != nil && (*m)[k]
}

// wakes reports a change of a message the view shows (the event
// goroutine): the frame it draws reads it from core.
func (l *thrLive) wakes(ev core.Event) bool {
	if ev.Type != "update" && ev.Type != "delete" || ev.Channel == "" || ev.TS == "" {
		return false
	}
	return l.has(actMsgKey{ev.Channel, ev.TS})
}

// noteLiveKeys records the messages the cards show: roots, replies, the
// older replies a card loaded.
func (v *Threads) noteLiveKeys() {
	d, _, _ := v.data.get()
	keys := make(map[actMsgKey]bool, 4*len(d.items))
	for i := range d.items {
		t := &d.items[i]
		keys[actMsgKey{t.Channel, t.Root.TS}] = true
		for j := range t.Replies {
			keys[actMsgKey{t.Channel, t.Replies[j].TS}] = true
		}
		if c := v.cards[t.key]; c != nil {
			for j := range c.older {
				keys[actMsgKey{t.Channel, c.older[j].TS}] = true
			}
		}
	}
	v.lv.keys.Store(&keys)
	v.lv.items = d.items
}

// applyLive brings the threads up to date before the frame draws
// (Layout): a load that landed takes what changed after its request went
// out, then every thread what changed since the last frame.
func (v *Threads) applyLive() {
	c := v.h.Core()
	if c == nil {
		return
	}
	if d, _, _ := v.data.get(); !thrSameItems(d.items, v.lv.items) {
		v.landLive(c)
	}
	keys, prev, ok := v.lv.log.read(c)
	if !ok {
		// further behind than core names: every thread looks its
		// messages up (a change core forgot means reading them again)
		if c.MessageChangeFloor() > prev {
			if !(v.data.busy() && v.mode == actFresh) {
				v.load(actRefresh)
			}
			return
		}
		v.landLive(c)
		return
	}
	for i, k := range keys {
		if (i == 0 || k != keys[i-1]) && v.lv.has(actMsgKey{k.Channel, k.TS}) {
			if ch, ok := c.MessageChange(k.Channel, k.TS); ok {
				v.patchThreads(c, actMsgKey{k.Channel, k.TS}, &ch)
			}
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
			if nm, ok := c.ChangedMessage(m.Channel, m.TS, &ch, m, card.olderSeq); ok {
				out = append(out, nm)
			} else if !ch.Deleted {
				out = append(out, *m)
			}
			card.built = false
		}
		if out != nil {
			card.older = out
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
		if m, ok := c.ChangedMessage(k.ch, k.ts, &ch, &t.Root, t.seq); ok {
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
		if m, ok := c.ChangedMessage(t.Channel, r.TS, &ch, r, t.seq); ok {
			out = append(out, m)
		} else if !ch.Deleted {
			out = append(out, *r)
		}
		v.unbuildThread(t.key)
	}
	if out != nil {
		t.Replies = out
	}
	t.seq = now
}

// patchThreads takes change ch of k into every thread showing k (a new
// threads slice, the thread's replies copied once): a reply deleted
// leaves its card; a deleted root keeps its copy (one with replies comes
// back as a tombstone's update).
func (v *Threads) patchThreads(c *core.App, k actMsgKey, ch *core.MsgChange) {
	v.data.mutate(func(d *thrData) {
		var items []thrItem
		for i := range d.items {
			t := &d.items[i]
			if t.Channel != k.ch || t.seq >= ch.Seq {
				continue
			}
			nt, changed := *t, false
			if t.Root.TS == k.ts && !ch.Deleted {
				if m, ok := c.ChangedMessage(k.ch, k.ts, ch, &t.Root, t.seq); ok {
					nt.Root, changed = m, true
				}
			}
			if j := slices.IndexFunc(t.Replies, func(m core.Message) bool { return m.TS == k.ts }); j >= 0 {
				rs := slices.Clone(t.Replies)
				if m, ok := c.ChangedMessage(k.ch, k.ts, ch, &rs[j], t.seq); ok {
					rs[j] = m
				} else if ch.Deleted {
					rs = slices.Delete(rs, j, j+1)
				}
				nt.Replies, changed = rs, true
			}
			if !changed {
				continue
			}
			// changes come in core's order: every one before this is in
			nt.seq = ch.Seq
			if items == nil {
				items = slices.Clone(d.items)
			}
			items[i] = nt
			v.unbuildThread(t.key)
		}
		if items != nil {
			d.items = items
		}
	})
	for key, card := range v.cards {
		j := slices.IndexFunc(card.older, func(m core.Message) bool { return m.Channel == k.ch && m.TS == k.ts })
		if j < 0 || card.olderSeq >= ch.Seq {
			continue
		}
		if m, ok := c.ChangedMessage(k.ch, k.ts, ch, &card.older[j], card.olderSeq); ok {
			card.older[j] = m
		} else if ch.Deleted {
			card.older = slices.Delete(slices.Clone(card.older), j, j+1)
		}
		card.olderSeq = ch.Seq
		v.unbuildThread(key)
	}
	d, _, _ := v.data.get()
	v.lv.items = d.items // its own change: nothing landed
}

// unbuildThread makes the header of the card key again (its people, its
// "Show N more replies").
func (v *Threads) unbuildThread(key string) {
	if c := v.cards[key]; c != nil {
		c.built = false
	}
}
