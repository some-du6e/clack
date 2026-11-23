package views

import (
	"slices"

	"github.com/some-du6e/clack/internal/core"
)

// A reaction pill's click changes the store's copy of its message (core's
// ReactBegin, messages.ClickReactionGroup), which every view draws once
// the "update" reaches it. A message the store doesn't hold (a feed copy
// no list keeps) changes here instead: the view keeps the viewer's
// changes to it and draws them over its copy, as the message list's
// reactNow mutates its own copy, until the copy shows them (an op the
// copy already agrees with goes), and takes one back when its
// reactions.add/remove failed.

// rxnOp is the state one of the viewer's reactions should show: name
// added or removed.
type rxnOp struct {
	name string
	add  bool
	seq  uint64 // MessageChangeSeq when it was made
}

// withLocalReactions is m with the view's local reaction changes laid
// over its reactions (m itself when there are none). UI goroutine.
func (b *base) withLocalReactions(m *core.Message) *core.Message {
	k := m.Channel + "/" + m.TS
	b.rxnMu.Lock()
	ops := b.rxnLocal[k]
	if len(ops) > 0 && b.storeTookBack(m, ops) {
		// the store holds the message now, or core changed it since: its
		// copy, not these, says what the viewer's reactions are
		delete(b.rxnLocal, k)
		ops = nil
	}
	if len(ops) > 0 {
		// an op the copy shows already is done
		keep := ops[:0]
		for _, op := range ops {
			if reactedIn(m.Reactions, op.name) != op.add {
				keep = append(keep, op)
			}
		}
		if len(keep) == 0 {
			delete(b.rxnLocal, k)
		} else {
			b.rxnLocal[k] = keep
		}
		ops = append([]rxnOp(nil), keep...)
	}
	b.rxnMu.Unlock()
	if len(ops) == 0 {
		return m
	}
	c := *m
	c.Reactions = b.applyRxnOps(m.Reactions, ops)
	return &c
}

// storeTookBack reports whether core has the message ops change: the
// store holds it (HeldMessage) and the view draws the store's copy (not
// ownCopy), or a change core made to it is newer than the ops
// (MessageChange). Call with rxnMu held.
func (b *base) storeTookBack(m *core.Message, ops []rxnOp) bool {
	c := b.h.Core()
	if c == nil {
		return false
	}
	if _, ok := c.HeldMessage(m.Channel, m.TS); ok && (b.ownCopy == nil || !b.ownCopy(m.Channel, m.TS)) {
		return true
	}
	if ch, ok := c.MessageChange(m.Channel, m.TS); ok {
		for _, op := range ops {
			if ch.Seq > op.seq {
				return true
			}
		}
	}
	return false
}

// reactedIn reports whether the viewer's reaction name is among rs.
func reactedIn(rs []core.Reaction, name string) bool {
	for i := range rs {
		if rs[i].Name == name {
			return rs[i].Self
		}
	}
	return false
}

// applyRxnOps is rs (not changed) with ops applied, as core lays the
// viewer's unsettled changes over a message (overlayReactions).
func (b *base) applyRxnOps(rs []core.Reaction, ops []rxnOp) []core.Reaction {
	out := make([]core.Reaction, len(rs))
	copy(out, rs)
	self := b.self()
	for _, op := range ops {
		i := slices.IndexFunc(out, func(r core.Reaction) bool { return r.Name == op.name })
		switch {
		case i < 0 && op.add:
			url := ""
			if c := b.h.Core(); c != nil {
				url, _, _ = c.Emoji(op.name)
			}
			out = append(out, core.Reaction{Name: op.name, Count: 1, Users: []string{self}, URL: url, Self: true})
		case i >= 0 && op.add && !out[i].Self:
			out[i].Self, out[i].Count = true, out[i].Count+1
			out[i].Users = append(slices.Clip(out[i].Users), self)
		case i >= 0 && !op.add && out[i].Self:
			out[i].Self, out[i].Count = false, out[i].Count-1
			out[i].Users = slices.DeleteFunc(slices.Clone(out[i].Users), func(u string) bool { return u == self })
			if out[i].Count <= 0 {
				out = slices.Delete(out, i, i+1)
			}
		}
	}
	return out
}

// rxnLocalCopy is messages.ReactionLocal for a view's copy m of a message.
type rxnLocalCopy struct {
	b *base
	m core.Message // the copy as drawn (the changes lay over it again idempotently)
}

func (l *rxnLocalCopy) key() string { return l.m.Channel + "/" + l.m.TS }

// Reactions are the copy's with the local changes now.
func (l *rxnLocalCopy) Reactions() []core.Reaction {
	l.b.rxnMu.Lock()
	ops := append([]rxnOp(nil), l.b.rxnLocal[l.key()]...)
	l.b.rxnMu.Unlock()
	return l.b.applyRxnOps(l.m.Reactions, ops)
}

// Change records the viewer's change: the state name's reaction should
// show, one per name (undo: the state before it, the change having
// failed), and asks for a frame.
func (l *rxnLocalCopy) Change(name string, add, undo bool) {
	if undo {
		add = !add
	}
	k := l.key()
	l.b.rxnMu.Lock()
	if l.b.rxnLocal == nil {
		l.b.rxnLocal = map[string][]rxnOp{}
	}
	var seq uint64
	if c := l.b.h.Core(); c != nil {
		seq = c.MessageChangeSeq()
	}
	ops := l.b.rxnLocal[k]
	if i := slices.IndexFunc(ops, func(o rxnOp) bool { return o.name == name }); i >= 0 {
		ops[i].add, ops[i].seq = add, seq
	} else {
		ops = append(ops, rxnOp{name, add, seq})
	}
	l.b.rxnLocal[k] = ops
	l.b.rxnMu.Unlock()
	l.b.h.Invalidate()
}
