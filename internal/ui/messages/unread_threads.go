package messages

import (
	"context"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/slack"
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
//   - What the socket can't keep current, the pane repairs: when the
//     channel's count moves, or the socket came back slowly
//     (core.ThreadActivityGen), the list is fetched again, as the answer
//     replaces it (refreshItems), behind the marks the socket already put
//     in; a fetch that failed is tried again when either moves, and after
//     unreadThreadsRetry (doubling) when neither does. Fetched once, a
//     thread read in another client during a sleep read as unread for the
//     session, and a failed fetch left the channel with no marks.
//   - MessageWrapper hands ThreadReplies getUnreadThreadsCountForMessage
//     (3x6A AY, core.UnreadThreadsFor) for its ts, and ReplyBar draws
//     renderUnreadThreadsIndicator ("Unread" after an 8px
//     icon-placeholder-filled, gap 4, --dt_color-content-imp) while it's
//     above 0, in the span hover hides (c-message__reply_bar_last_reply).
//     Slack passes 0 for the thread open in a focused secondary view;
//     Clack doesn't track that focus, so the mark stays until the thread
//     is read.

// unreadThreadsRetry is a failed fetch's wait before it's tried again
// with nothing moving; each failure in a row doubles it, up to 32 times.
const unreadThreadsRetry = 10 * time.Second

// unreadThreads is a channel list's fetch state (MessagePane's).
type unreadThreads struct {
	mu      sync.Mutex
	started bool      // the mount's check ran
	n       int       // the channel's unread thread count as last seen
	gen     uint64    // core.ThreadActivityGen as last seen
	want    bool      // a fetch is due
	busy    bool      // one is out
	fails   int       // fetches failed in a row
	retryAt time.Time // the next try after a failure
}

// updateUnreadThreads is maybeFetchUnreadThreadActivity (channel lists
// only, with the experiment on), and the repairs above. It never blocks:
// the fetch runs aside.
func (s *listState) updateUnreadThreads(h Host) {
	app := h.Core()
	if app == nil || s.thread || app.Experiment("unread_thread_indication_in_replybar") != "on" {
		return
	}
	n, gen := app.ThreadsUnreadIn(s.ch), app.ThreadActivityGen()
	u := &s.unreadTh
	u.mu.Lock()
	defer u.mu.Unlock()
	switch {
	case !u.started:
		u.started, u.n, u.gen = true, n, gen
		u.want = n > 0 && !app.ThreadActivityLoaded(s.ch)
	case n != u.n || gen != u.gen:
		// with none left core clears the list (pruneThreadActivity)
		u.n, u.gen = n, gen
		u.want, u.fails, u.retryAt = n > 0, 0, time.Time{}
	}
	if !u.want || u.busy || n == 0 || time.Now().Before(u.retryAt) {
		return
	}
	u.want, u.busy = false, true
	ch := s.ch
	go func() {
		// a moved count is new, not a retry loop: the client's identical
		// failure backoff (slack guard.go) would answer it with the last
		// error; the pane's own wait spaces the retries
		ctx, cancel := context.WithTimeout(slack.NoBackoff(context.Background()), 20*time.Second)
		defer cancel()
		_, err := app.ChannelUnreadThreads(ctx, ch) // into core's list for ch, which the rows read
		u.mu.Lock()
		u.busy = false
		if err != nil {
			wait := unreadThreadsRetry << min(u.fails, 5)
			u.want, u.fails, u.retryAt = true, u.fails+1, time.Now().Add(wait)
			time.AfterFunc(wait, h.Invalidate)
		} else {
			u.fails = 0
		}
		u.mu.Unlock()
		h.Invalidate()
	}()
}
