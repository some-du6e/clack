package core

import (
	"sync"
	"sync/atomic"
)

// The latest message changes, in the order emit sent them, for the views
// that read their messages as they lay out.
//
// Slack's views read one messages store as they render: an Activity row
// draws the revision of its message the store has
// (getMessageByTimestamp), so the commit after a message_changed draws
// the new one everywhere. A Clack view that keeps its own copies (the
// Activity page's) takes the "update" and "delete" events from here when
// it draws: no goroutine hop between core's change and the view's frame
// (an event subscription's goroutine may hand it over a frame after the
// message lists' does), and a view whose load is out reads what changed
// meanwhile once the answer lands. emit writes the log under subMu, so
// it holds the events in the order every subscriber gets them.

// maxMsgChanges bounds the log (a view drawn every frame reads a few;
// one that missed more reads its messages again). Each entry keeps its
// converted message alive.
const maxMsgChanges = 256

type msgChanges struct {
	mu  sync.Mutex
	n   atomic.Uint64 // changes logged so far
	evs [maxMsgChanges]Event
}

// noteMsgChange logs an "update" or a "delete" (emit, under subMu).
func (a *App) noteMsgChange(ev *Event) {
	if ev.Type != "update" && ev.Type != "delete" || ev.Channel == "" || ev.TS == "" {
		return
	}
	l := &a.msgLog
	l.mu.Lock()
	n := l.n.Load() + 1
	l.evs[n%maxMsgChanges] = *ev
	l.n.Store(n)
	l.mu.Unlock()
}

// MessageChangeSeq is how many message changes ("update" and "delete"
// events) core has emitted.
func (a *App) MessageChangeSeq() uint64 { return a.msgLog.n.Load() }

// MessageChanges are the message changes emitted after the first after
// ones, oldest first, and how many there are now. ok is false when more
// changes than the log keeps came since: the caller reads its messages
// again (the store holds them as they are now).
func (a *App) MessageChanges(after uint64) (evs []Event, now uint64, ok bool) {
	l := &a.msgLog
	l.mu.Lock()
	defer l.mu.Unlock()
	now = l.n.Load()
	if after >= now {
		return nil, now, true
	}
	if now-after > maxMsgChanges {
		return nil, now, false
	}
	evs = make([]Event, 0, now-after)
	for i := after + 1; i <= now; i++ {
		evs = append(evs, l.evs[i%maxMsgChanges])
	}
	return evs, now, true
}
