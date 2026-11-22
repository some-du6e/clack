package core

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/logtag"
)

// History's side reads: what a conversation's header shows beside its
// messages, read alongside the newest page. Slack's message pane renders
// the channel's history from its store and never waits for the header's
// data: the people count is membershipCounts, a fetch-managed object
// (client-boot PBfs, build 2c0d02ca130f) refetched when the header needs
// it, at most once per 2 s per channel (fetchAndUpsertMembershipCountsForChannel,
// Gpq2), and the header redraws when it lands. So here: a cached
// timeline's page returns at once, and the reads land on their own, the
// count with a "member_count" event the header repaints on.
//
// The reads aren't the caller's: they run under core's background
// context, so the session's end (an auth failure, sign-out's StopTraffic)
// cancels them, and Shutdown and Close wait for them, so none is left in
// flight once the session is gone.

// sideReadTimeout bounds one side read.
const sideReadTimeout = 20 * time.Second

// sideReads tracks the running side reads.
type sideReads struct {
	mu   sync.Mutex
	done bool // wait was called: no new reads start
	wg   sync.WaitGroup
}

// start counts one more read in; false once the session is over.
func (s *sideReads) start() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done {
		return false
	}
	s.wg.Add(1)
	return true
}

// wait stops new reads from starting and returns once the running ones
// returned (core's background context, cancelled first, ends them).
func (s *sideReads) wait() {
	s.mu.Lock()
	s.done = true
	s.mu.Unlock()
	s.wg.Wait()
}

// historySide starts History's side reads for ch: the member count (edge
// users/counts) when core has none for a channel, or a join or a leave
// made it stale, and the conversation when it's outside the sidebar (a
// closed DM, a channel opened from a link or a search: its header's name).
// count and conv close when each read returned; nil for a read not
// started.
func (a *App) historySide(ch string) (count, conv <-chan struct{}) {
	if a.bg.Err() != nil { // the session ended: nothing more goes out
		return nil, nil
	}
	a.mu.Lock()
	_, haveCount := a.memberN[ch]
	stale := a.memberStale[ch]
	a.mu.Unlock()
	if stale { // a join or a leave since it was read: the header's draw refetches it (membership_counts.go)
		a.RefreshMemberCount(ch)
	} else if c, ok := a.S.Conv(ch); ok && !haveCount && !c.IsIM && !c.IsMPIM {
		count = a.sideRead(ch, "users/counts", func(ctx context.Context) { a.loadMemberCount(ctx, ch) })
	}
	if _, ok := a.Conv(ch); !ok {
		conv = a.sideRead(ch, "conversations.info", func(ctx context.Context) { a.ResolveConv(ctx, ch) })
	}
	return count, conv
}

// sideRead runs one of History's side reads on its own goroutine and logs
// its time apart from the timeline read's ("history C1: 2ms (cache)").
// The channel closes when it returned (nil when the session is over).
func (a *App) sideRead(ch, what string, f func(ctx context.Context)) <-chan struct{} {
	if !a.side.start() {
		return nil
	}
	done := make(chan struct{})
	go func() {
		defer a.side.wg.Done()
		defer close(done)
		ctx, cancel := context.WithTimeout(a.bg, sideReadTimeout)
		defer cancel()
		t0 := time.Now()
		f(ctx)
		log.Printf("history %s: side %s %s", logtag.ID(ch), what, time.Since(t0).Round(time.Millisecond))
	}()
	return done
}

// waitSide waits for the side reads History started (nil ones are skipped).
func waitSide(chs ...<-chan struct{}) {
	for _, c := range chs {
		if c != nil {
			<-c
		}
	}
}

// PrefetchHistory starts reading ch's newest page before its view opens:
// the press on its sidebar row (ui sidebar_prefetch.go, Slack's
// prefetchPointerdownHistory). It only fills the store: the view's own
// History on the click returns the cached page, or joins this read while
// it's out (loadLatestIfMissing's onceCtx). Unlike History it leaves the
// conversation on screen alone (watch: the poll's refetch, typing's
// lookups) and doesn't mark the conversation opened (noteAccess), since a
// press may never become a click; the page it loads counts for the cache
// budget like any prefetch's (fetchLatest). It reports whether a read
// started: none for a conversation core holds already or is reading
// (d7's has_history and loading), or once the session is over. Skipping
// a read in flight also keeps the tracked side reads off a view's own
// History, which runs under the view's context, not core's.
func (a *App) PrefetchHistory(ch string) bool {
	if ch == "" || a.bg.Err() != nil || a.S.Timeline(ch) != nil || a.inFlight("h:"+ch) {
		return false
	}
	return a.sideRead(ch, "prefetch", func(ctx context.Context) { _ = a.loadLatestIfMissing(ctx, ch) }) != nil
}

// inFlight reports whether a once-style call for key is running.
func (a *App) inFlight(key string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	_, ok := a.inflight[key]
	return ok
}

// onceCtx runs f once for key at a time, like once, for callers that
// each stop waiting when their own ctx ends: one that finds a call in
// flight (another caller's, under that caller's ctx) waits only as long
// as ctx allows. A send's 10 s @channel check doesn't take on a side
// read's 20 s, and the side reads Close waits for, cancelled with core's
// background context, never hang on a view's History or a popup's
// lookup. A call that ended because its own caller's ctx did runs again
// under ctx. f must use its caller's ctx.
func (a *App) onceCtx(ctx context.Context, key string, f func() error) error {
	var err error
	for try := 0; try < 2; try++ {
		err = a.onceWait(ctx, key, f)
		if err == nil || ctx.Err() != nil || !(errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
			return err
		}
	}
	return err
}

// onceWait is once whose waiters return when ctx ends (ctx.Err()); the
// caller that starts the call runs f, under its own ctx.
func (a *App) onceWait(ctx context.Context, key string, f func() error) error {
	a.mu.Lock()
	if c, ok := a.inflight[key]; ok {
		a.mu.Unlock()
		select {
		case <-c.done:
			return c.err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	c := &call{done: make(chan struct{})}
	a.inflight[key] = c
	a.mu.Unlock()
	c.err = f()
	a.mu.Lock()
	delete(a.inflight, key)
	a.mu.Unlock()
	close(c.done)
	return c.err
}

// memberCountChanged is loadMemberCount's "member_count" event: the
// count changed (or arrived), and the conversation as core has it now
// (Conv.NumMembers is the header's count).
func (a *App) memberCountChanged(ch string) {
	ev := Event{Type: "member_count", Channel: ch}
	if c, ok := a.Conv(ch); ok {
		ev.Conv = &c
	}
	a.emit(ev)
}
