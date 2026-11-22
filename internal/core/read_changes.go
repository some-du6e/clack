package core

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// Revisions core's reads bring, announced as changes.
//
// A read that brings a revision of a message the store held another of
// (a thread read for the first time after a socket gap, a window read
// after one, messages.list) gives it to every copy the store keeps
// (store/read_revisions.go). Core announces it here as an "update", with
// no notification (nothing new arrived), so the change table
// (msg_changes.go) and every view keeping copies take it, as every row of
// Slack's draws what its one store holds once a replies answer or a
// history page lands.

// announceReads emits the revisions the store's reads brought since it
// last looked, each as the store holds the message when it goes out
// (announceHeld).
func (a *App) announceReads() {
	for _, r := range a.S.TakeReadRevisions() {
		a.announceHeld(r.Channel, r.Msg.TS)
	}
	a.tellFetched()
}

// tellFetched tells the fetched hook (SetFetchedHook) of the messages
// fetches by id brought that the store held no copy of
// (store.TakeFetchedNew), by channel. A new revision of a message the
// store held goes out as an "update"; one it held nowhere has no change to
// announce, and taut's mirror, which marks the plugins' copies of a
// conversation's messages from core's events, drew its plugins' view only
// at its next sweep (Slack's fetchObjectWithId puts the message in its
// one store, and the patched selector reads it in the same render). Called
// on the fetching goroutine before the fetch's answer goes to its caller,
// so a view of one never reads the plugins' copy as ready before the mark.
func (a *App) tellFetched() {
	refs := a.S.TakeFetchedNew()
	if len(refs) == 0 {
		return
	}
	f := a.fetchedHook.Load()
	if f == nil {
		return
	}
	by := map[string][]string{}
	var order []string
	for _, r := range refs {
		if _, ok := by[r.Channel]; !ok {
			order = append(order, r.Channel)
		}
		by[r.Channel] = append(by[r.Channel], r.TS)
	}
	for _, ch := range order {
		(*f)(ch, by[ch])
	}
}

// SetFetchedHook makes f hear of the messages each fetch by id brings that
// the store held no copy of (Activity's and Saved's hydration, the Threads
// view's getView, a thread's replies), by channel, on the fetching
// goroutine: taut's mirror marks them for the plugins. nil removes it;
// restore puts back the hook it replaced.
func (a *App) SetFetchedHook(f func(ch string, tss []string)) (restore func()) {
	var p *func(ch string, tss []string)
	if f != nil {
		p = &f
	}
	prev := a.fetchedHook.Swap(p)
	return func() { a.fetchedHook.CompareAndSwap(p, prev) }
}

// announceHeld emits an "update" of ch/ts as the store holds it now, in
// order with the socket's changes: a change the socket makes after the
// store was read (a message_changed, a reaction, a deletion: each noted
// with its write, store.LiveStamp) either goes out after this one, or is
// in the copy this one carries, never older after newer. A message the
// store no longer holds goes out as nothing (its deletion went out). It
// reports whether the update went out.
func (a *App) announceHeld(ch, ts string) bool {
	for try := 0; try < 8; try++ {
		m, stamp, ok := a.S.HeldStamped(ch, ts)
		if !ok {
			return false
		}
		v := a.messageWith(ch, &m, true) // asks for nobody: the rows ask as they draw
		ev := Event{Type: "update", Channel: ch, TS: ts, ThreadTS: m.ThreadTS, Msg: &v}
		if _, sent := a.emitIf(ev, func() bool { return a.S.LiveStamp(ch, ts) == stamp }); sent {
			return true
		}
		// changed meanwhile: its own update is out or on its way; this
		// one carries the copy after it
	}
	return false
}

// refetch is a message RefetchMessages asked Slack for: out while it
// waits for its round or is in one, and after a failed one, not again
// before retry.
type refetch struct {
	out   bool
	fails int
	retry time.Time
	// waiters are closed once the message has an answer, or core stops
	// asking by itself (EnsureMessages)
	waiters []chan struct{}
}

const (
	// maxRefetchRound bounds the messages one round asks for; the rest
	// wait for the next
	maxRefetchRound = 500
	// maxRefetchKeys bounds the failed fetches remembered for their
	// backoff
	maxRefetchKeys = 4096
	// refetchTries is how many rounds a message gets in all: core asks
	// again by itself after each failure but the last (each after its
	// backoff); then a view's next call does
	refetchTries = 5
	// refetchGap is Slack's UnknownFetchManager DEBOUNCE_INTERVAL: rounds
	// start at least this far apart, so what a frame (or a burst of
	// frames) asks for goes out together
	refetchGap = 200 * time.Millisecond
	// refetchHold is how long a round waits for the rest of what a frame
	// asks for (the views ask channel by channel)
	refetchHold = 20 * time.Millisecond
	// refetchRateWait is the wait after a rate limit that named none
	refetchRateWait = time.Minute
)

// refetchBackoff is the wait after the n-th failed fetch of a message: 2 s
// (past the client's own refusal of an identical failed request,
// slack/guard.go), doubling to a minute.
func refetchBackoff(n int) time.Duration {
	d := 2 * time.Second
	for i := 1; i < n && d < time.Minute; i++ {
		d *= 2
	}
	return min(d, time.Minute)
}

// refetchQueue is RefetchMessages' rounds, one at a time, as Slack's
// UnknownFetchManager keeps one fetch per team in flight, 200 ms apart,
// every pending id in one fetchMessagesByIds (which messages.list splits
// ten channels a call, two calls at a time: slack.MessagesList).
type refetchQueue struct {
	mu      sync.Mutex
	pending map[MsgKey]time.Time // what waits, and not before when
	busy    bool                 // a round is out
	timer   *time.Timer          // the next round
	last    time.Time            // when the last round ended
}

// RefetchMessages asks Slack again for the messages tss of ch by id (as
// Slack's row fetches a message its store doesn't hold), off the caller's
// goroutine, with what every other caller asks for meanwhile in the same
// round: what comes back is kept and announced as a change; a message its
// channel answered without is gone (Slack's isNonExistent: a "gone"
// change, which removes no Activity entry). After a failure the message
// waits (refetchBackoff, longer after a rate limit) and core asks again
// by itself, a few times (a view with nothing changing draws no frame to
// ask from). A view calls it for the messages core changed, or it shows,
// that no copy is left of (the store let them go). One request per message
// while one is out or waiting.
func (a *App) RefetchMessages(ch string, tss []string) {
	if ch == "" {
		return
	}
	l := &a.msgLog
	now := time.Now()
	l.mu.Lock()
	var want []MsgKey
	for _, ts := range tss {
		k := MsgKey{ch, ts}
		r := l.refetching[k]
		if ts == "" || r.out || now.Before(r.retry) {
			continue
		}
		if l.refetching == nil {
			l.refetching = map[MsgKey]refetch{}
		}
		r.out = true
		l.refetching[k] = r
		want = append(want, k)
	}
	if len(l.refetching) > maxRefetchKeys {
		for k, r := range l.refetching {
			if !r.out && now.After(r.retry) {
				delete(l.refetching, k)
			}
		}
	}
	l.mu.Unlock()
	if len(want) == 0 {
		return
	}
	q := &a.refetchQ
	q.mu.Lock()
	if q.pending == nil {
		q.pending = map[MsgKey]time.Time{}
	}
	for _, k := range want {
		q.pending[k] = time.Time{}
	}
	a.scheduleRefetchLocked(now)
	q.mu.Unlock()
}

// scheduleRefetchLocked starts the timer for the next round when none is
// out or set: refetchGap after the last round, or when the first message
// waiting may go (q.mu held).
func (a *App) scheduleRefetchLocked(now time.Time) {
	q := &a.refetchQ
	if q.busy || q.timer != nil || len(q.pending) == 0 {
		return
	}
	at := q.last.Add(refetchGap)
	if hold := now.Add(refetchHold); hold.After(at) {
		at = hold
	}
	first := time.Time{}
	for _, nb := range q.pending {
		if first.IsZero() || nb.Before(first) {
			first = nb
		}
	}
	if first.After(at) {
		at = first
	}
	q.timer = time.AfterFunc(max(0, at.Sub(now)), a.refetchRound)
}

// refetchRound is one round: every message waiting that may go now, in one
// hydrate across their channels.
func (a *App) refetchRound() {
	q := &a.refetchQ
	now := time.Now()
	q.mu.Lock()
	q.timer = nil
	if q.busy || a.bg.Err() != nil {
		q.mu.Unlock()
		return
	}
	want := map[string][]string{}
	var keys []MsgKey
	for k, nb := range q.pending {
		if len(keys) == maxRefetchRound {
			break
		}
		if nb.After(now) {
			continue
		}
		delete(q.pending, k)
		want[k.Channel] = append(want[k.Channel], k.TS)
		keys = append(keys, k)
	}
	if len(keys) == 0 {
		a.scheduleRefetchLocked(now)
		q.mu.Unlock()
		return
	}
	q.busy = true
	q.mu.Unlock()

	ctx, cancel := context.WithTimeout(a.bg, 30*time.Second)
	got, answered, err := a.hydrateKeepErr(ctx, want, true)
	cancel()
	for _, k := range keys {
		if _, ok := got[k.Channel+"/"+k.TS]; ok {
			a.announceHeld(k.Channel, k.TS)
		} else if answered[k.Channel] {
			a.emit(Event{Type: "gone", Channel: k.Channel, TS: k.TS})
		}
	}
	wait := refetchWaitAfter(err)
	l := &a.msgLog
	at := time.Now()
	var again []MsgKey
	var retry []time.Time
	var slugs []string // the answered messages a message slug asked for (msg_slug.go)
	l.mu.Lock()
	for _, k := range keys {
		if _, ok := got[k.Channel+"/"+k.TS]; ok || answered[k.Channel] {
			closeAll(l.refetching[k].waiters)
			delete(l.refetching, k)
			if l.slugAsked[k] {
				delete(l.slugAsked, k)
				slugs = append(slugs, k.String())
			}
			continue
		}
		r := l.refetching[k]
		r.fails++
		r.retry = at.Add(max(refetchBackoff(r.fails), wait))
		r.out = r.fails < refetchTries // still waiting for core's own next try
		if !r.out {
			closeAll(r.waiters) // core asks no more: a view's next call does
			r.waiters = nil
			if l.slugAsked[k] {
				// a slug asks on every frame: it keeps the non-existent
				// stub instead (msg_slug.go)
				delete(l.slugAsked, k)
				if l.slugGone == nil || len(l.slugGone) >= maxRefetchKeys {
					l.slugGone = map[MsgKey]bool{}
				}
				l.slugGone[k] = true
			}
		}
		l.refetching[k] = r
		if r.out {
			again, retry = append(again, k), append(retry, r.retry)
		}
	}
	l.mu.Unlock()
	if len(slugs) > 0 {
		// the slugs linking to them draw again (and their conversions
		// are made again: MsgRefs.Messages), as Slack's MessageMention
		// renders again once the fetch upserts the message or its
		// non-existent stub
		a.emit(Event{Type: "names", Users: slugs})
	}
	q.mu.Lock()
	for i, k := range again {
		q.pending[k] = retry[i]
	}
	q.busy, q.last = false, time.Now()
	a.scheduleRefetchLocked(q.last)
	q.mu.Unlock()
}

// refetchWaiters registers a wait on each of keys that RefetchMessages
// has out: a channel closed once its answer is in, or once core stops
// asking for it by itself (a key not out has nothing to wait for).
func (a *App) refetchWaiters(keys []MsgKey) []chan struct{} {
	if len(keys) == 0 {
		return nil
	}
	l := &a.msgLog
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []chan struct{}
	for _, k := range keys {
		r, ok := l.refetching[k]
		if !ok || !r.out {
			continue
		}
		ch := make(chan struct{})
		r.waiters = append(r.waiters, ch)
		l.refetching[k] = r
		out = append(out, ch)
	}
	return out
}

func closeAll(chs []chan struct{}) {
	for _, ch := range chs {
		close(ch)
	}
}

// refetchWaitAfter is the least wait err asks for: a rate limit's
// Retry-After (a minute when it names none), else nothing beyond the
// backoff.
func refetchWaitAfter(err error) time.Duration {
	var he *slack.HTTPError
	switch {
	case errors.As(err, &he) && he.Status == 429:
		if he.RetryAfter > 0 {
			return time.Duration(he.RetryAfter) * time.Second
		}
		return refetchRateWait
	case errors.Is(err, slack.RateLimited):
		return refetchRateWait
	}
	return 0
}
