package core

import (
	"context"
	"log"
	"sync"
	"time"
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
// users/counts) when core has none for a channel, and the conversation
// when it's outside the sidebar (a closed DM, a channel opened from a link
// or a search: its header's name). count and conv close when each read
// returned; nil for a read not started.
func (a *App) historySide(ch string) (count, conv <-chan struct{}) {
	if a.bg.Err() != nil { // the session ended: nothing more goes out
		return nil, nil
	}
	a.mu.Lock()
	_, haveCount := a.memberN[ch]
	a.mu.Unlock()
	if c, ok := a.S.Conv(ch); ok && !haveCount && !c.IsIM && !c.IsMPIM {
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
		log.Printf("history %s: side %s %s", ch, what, time.Since(t0).Round(time.Millisecond))
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
