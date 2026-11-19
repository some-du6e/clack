package core

import (
	"context"
	"time"
)

// An activity entry's lookups (its messages: messages.list, its people:
// users/info) run off the event pump, one entry after another in the
// order they came. On the pump, the events behind an entry waited for
// them: the server tells a thread's follower of a reply with activity
// entries as well as the reply's message event, and a reply whose entry
// came first reached the open thread pane only after Activity drew it
// (Karim, 2026-11-18: "i see it like in my activity tab first"). Slack's
// activity store takes an entry without holding up the socket's other
// events. The events Activity orders against its entries go through the
// same queue: the badges' zero (badge_counts_updated) marks read the rows
// the entries before it made unread, and a clear-all's undo follows them.

// activityLater runs f after every activity event's work queued before
// it, off the caller's goroutine, with a context that ends with the
// session (or after 15 s, as the pump's).
func (a *App) activityLater(f func(ctx context.Context)) {
	a.actLaterMu.Lock()
	a.actLaterQ = append(a.actLaterQ, f)
	start := !a.actLaterOn
	a.actLaterOn = true
	a.actLaterMu.Unlock()
	if start {
		go a.activityWorker()
	}
}

// activityInOrder runs f, work Activity orders against its entries (no
// lookups of its own), now on the caller's goroutine when no entry's work
// is queued or running, else behind it (activityLater): in socket order
// either way. The event pump is the queue's only producer, so nothing can
// queue between the check and f.
func (a *App) activityInOrder(ctx context.Context, f func(ctx context.Context)) {
	a.actLaterMu.Lock()
	busy := a.actLaterOn
	a.actLaterMu.Unlock()
	if busy {
		a.activityLater(f)
		return
	}
	f(ctx)
}

func (a *App) activityWorker() {
	for {
		a.actLaterMu.Lock()
		if len(a.actLaterQ) == 0 {
			a.actLaterOn = false
			a.actLaterMu.Unlock()
			return
		}
		f := a.actLaterQ[0]
		a.actLaterQ[0] = nil
		a.actLaterQ = a.actLaterQ[1:]
		a.actLaterMu.Unlock()
		if a.bg.Err() != nil {
			continue // the session ended: drop what is left
		}
		ctx, cancel := context.WithTimeout(a.bg, 15*time.Second)
		f(ctx)
		cancel()
	}
}
