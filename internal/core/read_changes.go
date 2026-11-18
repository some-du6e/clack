package core

import (
	"context"
	"time"
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
		if a.emitIf(ev, func() bool { return a.S.LiveStamp(ch, ts) == stamp }) {
			return true
		}
		// changed meanwhile: its own update is out or on its way; this
		// one carries the copy after it
	}
	return false
}

// refetch is a message RefetchMessages asked Slack for: out while its
// request is, and after a failed one, not again before retry.
type refetch struct {
	out   bool
	fails int
	retry time.Time
}

const (
	// maxRefetch bounds the messages of one conversation one call asks
	// for (one messages.list); the rest wait for the next call
	maxRefetch = 100
	// maxRefetchKeys bounds the failed fetches remembered for their
	// backoff
	maxRefetchKeys = 4096
	// refetchTries is how many times core asks for a message by itself
	// after its fetch failed (each after its backoff); then a view's next
	// call does
	refetchTries = 5
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

// RefetchMessages asks Slack again for the messages tss of ch by id (as
// Slack's row fetches a message its store doesn't hold), in one
// messages.list off the caller's goroutine: what comes back is kept and
// announced as a change; a message the channel answered without is gone
// (Slack's isNonExistent: a "gone" change, which removes no Activity
// entry); after a failure the message waits before it's asked for again,
// and core asks again by itself once the wait is over, a few times (a
// view with nothing changing draws no frame to ask from). A view calls
// it for the messages core changed, or it shows, that no copy is left of
// (the store let them go). One request per message while one is out.
func (a *App) RefetchMessages(ch string, tss []string) {
	if ch == "" {
		return
	}
	l := &a.msgLog
	now := time.Now()
	l.mu.Lock()
	var want []string
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
		want = append(want, ts)
		if len(want) == maxRefetch {
			break
		}
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
	go func() {
		ctx, cancel := context.WithTimeout(a.bg, 30*time.Second)
		defer cancel()
		got, answered := a.hydrateKeep(ctx, map[string][]string{ch: want}, true)
		for _, ts := range want {
			if _, ok := got[ch+"/"+ts]; ok {
				a.announceHeld(ch, ts)
			} else if answered[ch] {
				a.emit(Event{Type: "gone", Channel: ch, TS: ts})
			}
		}
		l.mu.Lock()
		at := time.Now()
		var again []string
		wait := time.Duration(0)
		for _, ts := range want {
			k := MsgKey{ch, ts}
			if _, ok := got[ch+"/"+ts]; ok || answered[ch] {
				delete(l.refetching, k)
				continue
			}
			r := l.refetching[k]
			r.out, r.fails = false, r.fails+1
			d := refetchBackoff(r.fails)
			r.retry = at.Add(d)
			l.refetching[k] = r
			if r.fails < refetchTries {
				again, wait = append(again, ts), max(wait, d)
			}
		}
		l.mu.Unlock()
		if len(again) > 0 {
			time.AfterFunc(wait, func() {
				if a.bg.Err() == nil {
					a.RefetchMessages(ch, again)
				}
			})
		}
	}()
}
