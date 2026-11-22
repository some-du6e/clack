package messages

import (
	"context"
	"sync"
	"time"
)

// The reply bar's "Unread" mark under unread_thread_indication_in_replybar
// (Slack's client, build 548618a2169f2a90):
//
//   - MessagePane maybeFetchUnreadThreadActivity (boot-render bU1n) fetches
//     the channel's unread thread activity (core.ChannelUnreadThreads) once
//     per mount, while client.counts says the channel has unread threads
//     (threadsUnreadCountByChannel, core.ThreadsUnreadIn) and its list
//     holds no thread_v2 item (hasLoadedThreadActivityForChannel,
//     core.ThreadActivityLoaded). From then on the socket's activity items
//     keep the list current (core channel_thread_activity.go), so a
//     reply's mark comes with its thread_v2 item, with no fetch: Clack
//     fetched again whenever the channel's count changed, and the mark came
//     an activity.feed round trip after the reply drew.
//   - MessageWrapper hands ThreadReplies getUnreadThreadsCountForMessage
//     (3x6A AY, core.UnreadThreadsFor) for its ts, and ReplyBar draws
//     renderUnreadThreadsIndicator ("Unread" after an 8px
//     icon-placeholder-filled, gap 4, --dt_color-content-imp) while it's
//     above 0, in the span hover hides (c-message__reply_bar_last_reply).
//     Slack passes 0 for the thread open in a focused secondary view;
//     Clack doesn't track that focus, so the mark stays until the thread
//     is read.

// unreadThreads is a channel list's fetch state (MessagePane's).
type unreadThreads struct {
	mu      sync.Mutex
	fetched bool // hasFetchedUnreadThreads: asked once for this list
}

// updateUnreadThreads is maybeFetchUnreadThreadActivity (channel lists
// only, with the experiment on). It never blocks: the fetch runs aside.
func (s *listState) updateUnreadThreads(h Host) {
	app := h.Core()
	if app == nil || s.thread || app.Experiment("unread_thread_indication_in_replybar") != "on" {
		return
	}
	if app.ThreadsUnreadIn(s.ch) == 0 || app.ThreadActivityLoaded(s.ch) {
		return
	}
	u := &s.unreadTh
	u.mu.Lock()
	if u.fetched {
		u.mu.Unlock()
		return
	}
	u.fetched = true
	u.mu.Unlock()
	ch := s.ch
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_, _ = app.ChannelUnreadThreads(ctx, ch) // into core's list for ch, which the rows read
		h.Invalidate()
	}()
}
