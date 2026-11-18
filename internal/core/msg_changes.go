package core

import (
	"slices"
	"sync"
	"sync/atomic"
)

// What core last did to each message, for the views that keep copies of
// their messages and read them as they lay out.
//
// Slack's views read one messages store as they render: an Activity row
// draws the revision of its message the store has
// (getMessageByTimestamp), a saved row too (LaterItemRow), and the
// Threads view draws the thread pane's components over it. So the commit
// after a message_changed draws the new revision everywhere, and nothing
// a view did meanwhile (being off screen, a load in flight) can leave it
// on an older one. A Clack view that keeps its own copies (Activity's,
// Threads', Saved's) asks here, before a frame describes its rows, what
// changed of them: no goroutine hop between core's change and the view's
// frame (an event subscription's goroutine may hand it over a frame after
// the message lists'), and no log to keep up with. The table holds each
// message's latest change however many came since, and a copy stamped
// with the MessageChangeSeq it was read at (an Activity item, a page that
// just landed) is current unless the table has a later change for it.
//
// emit records the "update" and "delete" events under subMu, so the
// order here is the order every subscriber gets them in, and each event
// carries its place in it (Event.Seq).

// MsgKey names a message: its conversation and ts.
type MsgKey struct{ Channel, TS string }

// MsgChange is the latest change core made to a message.
type MsgChange struct {
	// Seq is its place in the order core changed messages in
	// (MessageChangeSeq). FullSeq is the latest change that wasn't a
	// reactions update (UpdateReactions: Slack's reactions reducer
	// touches nothing else), 0 when the table has seen only reactions
	// change since it began keeping the message.
	Seq, FullSeq uint64
	// RemoveSeq is the latest change that took the entries about the
	// message out of Activity (P0K2 "Remove activity item": a deletion, or
	// an update with Event.Removes); 0 when none did.
	RemoveSeq uint64
	// Deleted: deleted, or Slack said there is no such message ("gone":
	// RemoveSeq stays)
	Deleted  bool
	ThreadTS string // the thread a reply is in, as the latest change said
	// Msg is the message as the change left it, while the table keeps its
	// copy (the latest maxChangeCopies changes); nil otherwise, and when
	// it was deleted. The store holds it then (HeldMessage).
	Msg *Message
}

const (
	// maxChangeKeys bounds the messages the table remembers. Past it the
	// quarter changed longest ago is forgotten, and the floor rises to the
	// newest change forgotten (MessageChangeFloor).
	maxChangeKeys = 8192
	// maxChangeCopies is how many of the latest changes keep their
	// converted message (a reader a frame behind reads it from here; one
	// further behind converts the store's copy).
	maxChangeCopies = 128
	// maxRecentChanges is how many of the latest changes
	// MessageChangedSince names. A reader further behind looks up each of
	// its messages instead.
	maxRecentChanges = 4096
)

type msgChanges struct {
	mu     sync.Mutex
	n      atomic.Uint64 // changes so far
	floor  atomic.Uint64 // the newest change the table forgot
	recs   map[MsgKey]*MsgChange
	recent [maxRecentChanges]MsgKey // the latest changes' messages, by seq
	copies [maxChangeCopies]changeCopy
	nc     uint64 // copies kept so far
	// removing is the message whose next update removes the Activity
	// entries about it (removesNext)
	removing MsgKey
	// refetching are the messages RefetchMessages asked Slack for
	// (read_changes.go)
	refetching map[MsgKey]refetch
}

// changeCopy is the change whose message a slot of msgChanges.copies keeps.
type changeCopy struct {
	k   MsgKey
	seq uint64
}

// noteMsgChange records an "update", a "delete" or a "gone" and stamps
// it with its place in the order (emit, under subMu, before any
// subscriber gets it).
func (a *App) noteMsgChange(ev *Event) {
	if !msgChangeEvent(ev) {
		return
	}
	l := &a.msgLog
	l.mu.Lock()
	defer l.mu.Unlock()
	n := l.n.Load() + 1
	ev.Seq = n
	k := MsgKey{ev.Channel, ev.TS}
	if ev.Type == "update" && ev.Name != UpdateReactions && l.removing == k {
		ev.Removes, l.removing = true, MsgKey{} // message_changed's, not a reaction's in between
	}
	if l.recs == nil {
		l.recs = map[MsgKey]*MsgChange{}
	}
	r := l.recs[k]
	if r == nil {
		r = &MsgChange{}
		l.recs[k] = r
	}
	r.Seq = n
	switch {
	case ev.ThreadTS != "":
		r.ThreadTS = ev.ThreadTS
	case ev.Msg != nil && ev.Msg.ThreadTS != "":
		r.ThreadTS = ev.Msg.ThreadTS
	}
	switch {
	case ev.Type == "delete":
		r.FullSeq, r.RemoveSeq, r.Deleted, r.Msg = n, n, true, nil
	case ev.Type == "gone":
		// Slack answered a fetch by id without it (isNonExistent): no
		// copy to draw, and no Activity entry removed for it
		r.FullSeq, r.Deleted, r.Msg = n, true, nil
	case ev.Name == UpdateReactions && ev.Msg != nil:
		// the rest stays what the last full change left (the event's copy
		// can be another conversation's: the channel's copy of a root)
		if r.Msg != nil {
			m := *r.Msg
			m.Reactions = ev.Msg.Reactions
			r.Msg = &m
		} else {
			r.Msg = ev.Msg
		}
		r.Deleted = false
		l.keepCopyLocked(k, n)
	default:
		r.FullSeq, r.Deleted, r.Msg = n, false, ev.Msg
		if ev.Removes {
			r.RemoveSeq = n
		}
		if ev.Msg != nil {
			l.keepCopyLocked(k, n)
		}
	}
	l.recent[n%maxRecentChanges] = k
	l.n.Store(n)
	if len(l.recs) > maxChangeKeys {
		l.forgetLocked()
	}
}

// msgChangeEvent reports an event the change table records: a message's
// "update" or "delete", or "gone" (RefetchMessages).
func msgChangeEvent(ev *Event) bool {
	return (ev.Type == "update" || ev.Type == "delete" || ev.Type == "gone") && ev.Channel != "" && ev.TS != ""
}

// removesNext marks the next update of ch/ts as one that removes the
// Activity entries about it (Event.Removes): the update message_changed
// sends right after (emitUpdate, emitStreamDone).
func (a *App) removesNext(ch, ts string) {
	l := &a.msgLog
	l.mu.Lock()
	l.removing = MsgKey{ch, ts}
	l.mu.Unlock()
}

// keepCopyLocked notes that k's change seq keeps its message, and lets go
// of the message of the change that kept the slot before.
func (l *msgChanges) keepCopyLocked(k MsgKey, seq uint64) {
	c := &l.copies[l.nc%maxChangeCopies]
	if c.seq != 0 {
		if r := l.recs[c.k]; r != nil && r.Seq == c.seq {
			r.Msg = nil
		}
	}
	*c = changeCopy{k, seq}
	l.nc++
}

// forgetLocked forgets the quarter of the messages changed longest ago.
func (l *msgChanges) forgetLocked() {
	seqs := make([]uint64, 0, len(l.recs))
	for _, r := range l.recs {
		seqs = append(seqs, r.Seq)
	}
	slices.Sort(seqs)
	cut := seqs[len(seqs)/4]
	for k, r := range l.recs {
		if r.Seq <= cut {
			delete(l.recs, k)
		}
	}
	if cut > l.floor.Load() {
		l.floor.Store(cut)
	}
}

// MessageChangeSeq is how many message changes ("update" and "delete"
// events) core has made: a copy read now is current as of this.
func (a *App) MessageChangeSeq() uint64 { return a.msgLog.n.Load() }

// MessageChangeFloor is the newest change the table forgot: a copy older
// than it, of a message MessageChange doesn't know, may have missed one.
func (a *App) MessageChangeFloor() uint64 { return a.msgLog.floor.Load() }

// MessageChangedSince names the messages changed after change after,
// oldest change first (a message changed twice is named twice), and the
// changes made now. ok is false when more came since than it names: the
// caller looks up each message it holds (MessageChange).
func (a *App) MessageChangedSince(after uint64) (keys []MsgKey, now uint64, ok bool) {
	l := &a.msgLog
	if now = l.n.Load(); after >= now {
		return nil, now, true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now = l.n.Load()
	if now-after > maxRecentChanges {
		return nil, now, false
	}
	keys = make([]MsgKey, 0, now-after)
	for i := after + 1; i <= now; i++ {
		keys = append(keys, l.recent[i%maxRecentChanges])
	}
	return keys, now, true
}

// MessageChange is the latest change core made to ch/ts; ok is false when
// the table knows of none (none since the session began, or it was
// forgotten: MessageChangeFloor).
func (a *App) MessageChange(ch, ts string) (MsgChange, bool) {
	l := &a.msgLog
	l.mu.Lock()
	defer l.mu.Unlock()
	if r := l.recs[MsgKey{ch, ts}]; r != nil {
		return *r, true
	}
	return MsgChange{}, false
}

// HeldMessage is ch/ts as the store keeps it current from the socket (a
// window's, a cached thread's or a copy fetched by id: store.FindHeld),
// converted with what the store holds now and asking for nobody, as
// StoredMessage does.
func (a *App) HeldMessage(ch, ts string) (Message, bool) {
	m, ok := a.S.FindHeld(ch, ts)
	if !ok {
		return Message{}, false
	}
	return a.messageWith(ch, &m, true), true
}

// ChangedMessage is what change c left of the message: its own copy, else
// the store's (HeldMessage); with only reactions changed since have (a
// copy current as of seq haveSeq), have with the change's reactions. ok
// is false when the message is gone, or c names no copy the store still
// holds.
func (a *App) ChangedMessage(ch, ts string, c *MsgChange, have *Message, haveSeq uint64) (Message, bool) {
	if c.Deleted {
		return Message{}, false
	}
	m := c.Msg
	if m == nil {
		h, ok := a.HeldMessage(ch, ts)
		if !ok {
			return Message{}, false
		}
		m = &h
	}
	if have != nil && c.FullSeq <= haveSeq {
		out := *have
		out.Reactions = m.Reactions
		return out, true
	}
	return *m, true
}
