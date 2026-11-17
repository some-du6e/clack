package views

// The Threads view's messages as they are now.
//
// Slack's Threads view draws its threads with the thread pane's message
// components over the messages store, so an edit, an app's chat.update,
// a reaction or a deletion shows in it in the commit it lands in, as it
// does in the thread pane (Slack's Saved rows read the store the same
// way: LaterItemRow's getMessageByTimestamp). Clack's cards drew the
// copies subscriptions.thread.getView returned until a new reply reloaded
// the view. They draw core's changes over those copies now (its log of
// message changes, core.MessageChanges, read before each frame draws),
// with no reload.

import (
	"sync/atomic"

	"github.com/some-du6e/clack/internal/core"
)

// viewMsgLog is a view's place in core's log of message changes.
type viewMsgLog struct {
	seen    uint64
	started bool
}

// start reads the changes from now on (before the view's first load:
// its answer is read after them).
func (l *viewMsgLog) start(c *core.App) {
	if !l.started && c != nil {
		l.started, l.seen = true, c.MessageChangeSeq()
	}
}

// read is the changes since the last read, oldest first; ok is false when
// more came than core's log keeps (the view reads its messages again).
func (l *viewMsgLog) read(c *core.App) (evs []core.Event, ok bool) {
	if c == nil || !l.started || c.MessageChangeSeq() == l.seen {
		return nil, true
	}
	evs, l.seen, ok = c.MessageChanges(l.seen)
	return evs, ok
}

// msgAfter is held as the "update" ev leaves it: ev's copy, or for a
// reactions update (core.UpdateReactions) held with ev's reactions, since
// Slack's reactions reducer touches nothing else and the event's copy can
// be another conversation's (the channel's copy of a thread root).
func msgAfter(held *core.Message, ev *core.Event) core.Message {
	m := *ev.Msg
	if ev.Name == core.UpdateReactions && held != nil {
		h := *held
		h.Reactions = m.Reactions
		return h
	}
	return m
}

// thrLive is what the Threads view knows of its messages since its feed
// brought them: the copy core's changes brought last (m nil: deleted).
type thrLive struct {
	log  viewMsgLog
	live map[actMsgKey]actLive
	seq  uint64
	// keys are the messages the cards show, for the event goroutine
	keys atomic.Pointer[map[actMsgKey]bool]
}

// wakes reports a change of a message the view shows (the event
// goroutine): the frame it draws reads it from core's log.
func (l *thrLive) wakes(ev core.Event) bool {
	if ev.Type != "update" && ev.Type != "delete" || ev.Channel == "" || ev.TS == "" {
		return false
	}
	keys := l.keys.Load()
	return keys != nil && (*keys)[actMsgKey{ev.Channel, ev.TS}]
}

// noteLiveKeys records the messages the cards show (roots, replies, the
// older replies a card loaded); what core's changes brought for a message
// no longer shown goes, unless a load is out (its answer may bring it).
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
	if v.wasBusy {
		return
	}
	for k := range v.lv.live {
		if !keys[k] {
			delete(v.lv.live, k)
		}
	}
}

// applyLive takes core's changes to the messages the view shows before
// the frame draws (Layout). More than core's log keeps went by unread:
// the view reads its threads again.
func (v *Threads) applyLive() {
	c := v.h.Core()
	evs, ok := v.lv.log.read(c)
	if !ok {
		clear(v.lv.live)
		if !(v.data.busy() && v.mode == actFresh) {
			v.load(actRefresh)
		}
		return
	}
	if len(evs) == 0 {
		return
	}
	keys := v.lv.keys.Load()
	for i := range evs {
		ev := &evs[i]
		k := actMsgKey{ev.Channel, ev.TS}
		if !v.wasBusy && (keys == nil || !(*keys)[k]) {
			continue
		}
		if v.lv.live == nil {
			v.lv.live = map[actMsgKey]actLive{}
		}
		v.lv.seq++
		if ev.Type == "delete" {
			v.lv.live[k] = actLive{seq: v.lv.seq}
		} else {
			m := msgAfter(v.thrHeld(k), ev)
			v.lv.live[k] = actLive{m: &m, seq: v.lv.seq}
		}
		v.unbuildThread(ev.Channel, ev.TS)
	}
}

// thrHeld is the copy of k the view draws now: core's change's, else the
// feed's.
func (v *Threads) thrHeld(k actMsgKey) *core.Message {
	if l, ok := v.lv.live[k]; ok {
		return l.m
	}
	d, _, _ := v.data.get()
	for i := range d.items {
		t := &d.items[i]
		if t.Channel != k.ch {
			continue
		}
		if t.Root.TS == k.ts {
			return &t.Root
		}
		for j := range t.Replies {
			if t.Replies[j].TS == k.ts {
				return &t.Replies[j]
			}
		}
		if c := v.cards[t.key]; c != nil {
			for j := range c.older {
				if c.older[j].TS == k.ts {
					return &c.older[j]
				}
			}
		}
	}
	return nil
}

// unbuildThread makes the header of the card showing ch/ts again (its
// people, its "Show N more replies").
func (v *Threads) unbuildThread(ch, ts string) {
	d, _, _ := v.data.get()
	for i := range d.items {
		if t := &d.items[i]; t.Channel == ch {
			if c := v.cards[t.key]; c != nil {
				c.built = false
			}
		}
	}
}

// liveRoot is t's root as the card draws it: core's change's copy, else
// the feed's (a deleted root keeps the feed's: one with replies comes
// back as a tombstone's update).
func (v *Threads) liveRoot(t *thrItem) *core.Message {
	if l, ok := v.lv.live[actMsgKey{t.Channel, t.Root.TS}]; ok && l.m != nil {
		return l.m
	}
	return &t.Root
}

// liveReplies is the card's replies as it draws them: core's changes'
// copies, the deleted ones left out (a new slice when any changed).
func (v *Threads) liveReplies(ch string, rs []core.Message) []core.Message {
	if len(v.lv.live) == 0 {
		return rs
	}
	var out []core.Message
	for i := range rs {
		l, ok := v.lv.live[actMsgKey{ch, rs[i].TS}]
		if !ok {
			if out != nil {
				out = append(out, rs[i])
			}
			continue
		}
		if out == nil {
			out = append(make([]core.Message, 0, len(rs)), rs[:i]...)
		}
		if l.m != nil {
			out = append(out, *l.m)
		}
	}
	if out == nil {
		return rs
	}
	return out
}
