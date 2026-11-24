package core

import (
	"context"
	"sync"
	"time"
)

// The DegradedBanner's "Load new messages" (boot-shared 7JQN
// loadNewMessages, build 889ac449a32d): while the socket has trouble
// connecting, the view asks for what the socket would have brought.
//
//   - requestOfflineHistory (wy9s): the open conversation's newest
//     history (conversations.history, reason fetchHistoryInDegradedMode),
//     or an open thread's replies, each at most once a second. Nothing for
//     a conversation the store doesn't hold, or while the socket is up and
//     synced (hasSyncedConnection).
//   - requestOfflineCounts: client.counts, at most once a minute (the
//     first call goes at once: throttle 6e4 with trailing false).
//   - reconnectImmediately (zNCe): a socket waiting out its backoff
//     connects now (ConnectNow).

// degradedState is the throttles' last calls.
type degradedState struct {
	mu       sync.Mutex
	countsAt time.Time
	histAt   map[string]time.Time
}

// Degraded throttles (vars for tests).
var (
	degradedCountsEvery  = time.Minute
	degradedHistoryEvery = time.Second
)

// DegradedLoad runs the banner's loads for conversation ch (a channel id,
// "" for none) and the thread threadTS of threadCh open beside it, and
// returns once they're answered.
func (a *App) DegradedLoad(ctx context.Context, ch, threadCh, threadTS string) {
	var wg sync.WaitGroup
	run := func(f func()) {
		wg.Add(1)
		go func() { defer wg.Done(); f() }()
	}
	if LooksLikeChannelID(ch) {
		run(func() { a.degradedHistory(ctx, ch, "") })
	}
	if threadCh != "" && threadTS != "" {
		run(func() { a.degradedHistory(ctx, threadCh, threadTS) })
	}
	run(func() { a.requestOfflineCounts(ctx) })
	a.ConnectNow()
	wg.Wait()
}

// degradedHistory is requestOfflineHistory for one conversation or thread.
func (a *App) degradedHistory(ctx context.Context, ch, threadTS string) {
	a.mu.Lock()
	up := a.connected
	a.mu.Unlock()
	if up { // hasSyncedConnection
		return
	}
	if _, ok := a.S.Conv(ch); !ok {
		return
	}
	d := &a.degraded
	k := ch + "/" + threadTS
	d.mu.Lock()
	if at, ok := d.histAt[k]; ok && time.Since(at) < degradedHistoryEvery {
		d.mu.Unlock()
		return
	}
	if d.histAt == nil {
		d.histAt = map[string]time.Time{}
	}
	d.histAt[k] = time.Now()
	d.mu.Unlock()
	if threadTS != "" {
		_, _ = a.RefreshReplies(ctx, ch, threadTS)
		return
	}
	_, _ = a.Refresh(ctx, ch)
}

// requestOfflineCounts is client.counts at most once a minute.
func (a *App) requestOfflineCounts(ctx context.Context) {
	d := &a.degraded
	d.mu.Lock()
	if !d.countsAt.IsZero() && time.Since(d.countsAt) < degradedCountsEvery {
		d.mu.Unlock()
		return
	}
	d.countsAt = time.Now()
	d.mu.Unlock()
	a.refreshCounts(ctx)
}
