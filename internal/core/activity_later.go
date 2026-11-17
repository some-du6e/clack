package core

import (
	"context"
	"sync"
	"time"
)

// The Activity events' lookups run off the event pump. Slack's
// handleActivityRTMEvent (boot-shared) puts the entry into its store as
// the event arrives, and what the row shows (the message, the people) is
// fetched by the views that draw it. Clack builds an entry's item before
// it announces it (activityItems: messages.list for a message it doesn't
// hold, users/info for its people), and did that on the pump: every
// message behind an Activity entry waited for those round trips, so a
// mention showed in Activity while the channel's list was still waiting
// for it (Karim, α.102: "the activity tab is updated", the messages "a
// second" late). The pump now hands them to one worker that runs them in
// the order they came: Activity's entries keep their order among
// themselves (an update after the add, the clear-all after both), and no
// other event waits for them.

// actQueue is activityLater's queue; a worker drains it while it isn't
// empty.
type actQueue struct {
	mu      sync.Mutex
	tasks   []func(ctx context.Context)
	running bool
}

// activityLater runs f after every Activity task queued before it, off
// the caller's goroutine, with a context that ends with the session (or
// after 15 s, as the pump's).
func (a *App) activityLater(f func(ctx context.Context)) {
	q := &a.actQ
	q.mu.Lock()
	q.tasks = append(q.tasks, f)
	start := !q.running
	q.running = true
	q.mu.Unlock()
	if start {
		go a.activityWork(q)
	}
}

func (a *App) activityWork(q *actQueue) {
	for {
		q.mu.Lock()
		if len(q.tasks) == 0 {
			q.running = false
			q.tasks = nil
			q.mu.Unlock()
			return
		}
		f := q.tasks[0]
		q.tasks[0] = nil
		q.tasks = q.tasks[1:]
		q.mu.Unlock()
		if a.bg.Err() != nil {
			continue // the session ended: nothing to announce
		}
		ctx, cancel := context.WithTimeout(a.bg, 15*time.Second)
		f(ctx)
		cancel()
	}
}
