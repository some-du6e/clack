package messages

import (
	"context"
	"sync"
	"time"
)

// The reply bar's "Unread" mark under unread_thread_indication_in_replybar
// (Slack's client, build 4d0c78085232):
//
//   - MessagePane maybeFetchUnreadThreadActivity (boot-render bU1n) fetches
//     the channel's unread thread activity (core.ChannelUnreadThreads) when
//     client.counts says the channel has unread threads
//     (threadsUnreadCountByChannel, core.ThreadsUnreadIn). Slack then keeps
//     that list current from realtime activity items; Clack fetches it
//     again whenever the channel's count changes (client.counts is read
//     again after thread_marked and update_thread_state).
//   - MessageWrapper hands ThreadReplies getUnreadThreadsCountForMessage
//     (3x6A AY) for its ts, and ReplyBar draws renderUnreadThreadsIndicator
//     ("Unread" after an 8px icon-placeholder-filled, gap 4,
//     --dt_color-content-imp) while it's above 0, in the span hover hides
//     (c-message__reply_bar_last_reply). Slack passes 0 for the thread open
//     in a focused secondary view; Clack doesn't track that focus, so the
//     mark stays until the thread is read.

// unreadThreads is a channel list's unread thread activity.
type unreadThreads struct {
	mu     sync.Mutex
	counts map[string]int // thread ts → unread items
	forN   int            // the channel's ThreadsUnreadIn it was fetched for
	busy   bool
}

// updateUnreadThreads keeps the channel's list current (channel lists
// only, with the experiment on). It never blocks: the fetch runs aside.
func (s *listState) updateUnreadThreads(h Host) {
	app := h.Core()
	if app == nil || s.thread || app.Experiment("unread_thread_indication_in_replybar") != "on" {
		return
	}
	n := app.ThreadsUnreadIn(s.ch)
	u := &s.unreadTh
	u.mu.Lock()
	defer u.mu.Unlock()
	if n == 0 {
		u.counts, u.forN = nil, 0
		return
	}
	if u.busy || u.forN == n {
		return
	}
	u.busy = true
	ch := s.ch
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		got, err := app.ChannelUnreadThreads(ctx, ch)
		u.mu.Lock()
		u.busy, u.forN = false, n // hasFetchedUnreadThreads: no retry until the count changes
		if err == nil {
			u.counts = got
		}
		u.mu.Unlock()
		h.Invalidate()
	}()
}

// unreadThreadsFor is getUnreadThreadsCountForMessage for root ts.
func (s *listState) unreadThreadsFor(ts string) int {
	u := &s.unreadTh
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.counts[ts]
}
