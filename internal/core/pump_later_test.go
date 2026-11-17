package core

import (
	"context"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// laterDone waits until the event pump's later work queued so far ran
// (pump_later.go): the lookups a handled event left, and what they
// announce.
func (a *App) laterDone(t testing.TB) {
	t.Helper()
	for dl := time.Now().Add(15 * time.Second); ; time.Sleep(time.Millisecond) {
		q := &a.pumpQ
		q.mu.Lock()
		idle := !q.running && len(q.tasks) == 0
		q.mu.Unlock()
		if idle {
			return
		}
		if time.Now().After(dl) {
			t.Fatal("the event pump's later work never finished")
		}
	}
}

// handleNow is handle and the lookups it left (laterDone).
func (a *App) handleNow(t testing.TB, ev slack.Event) {
	t.Helper()
	a.handle(ev)
	a.laterDone(t)
}

// landed waits until no background catch-up of key is out
// (history_catchup.go catchUpLater).
func (a *App) landed(t testing.TB, key string) {
	t.Helper()
	for dl := time.Now().Add(20 * time.Second); a.catchingUp(key); time.Sleep(5 * time.Millisecond) {
		if time.Now().After(dl) {
			t.Fatalf("the catch-up of %s never landed", key)
		}
	}
}

// historyLanded is History once the catch-up it started (if any) landed:
// what a list shows after the "history_caught_up" event.
func (a *App) historyLanded(t testing.TB, ctx context.Context, ch string) Page {
	t.Helper()
	if _, err := a.History(ctx, ch, ""); err != nil {
		t.Fatal(err)
	}
	a.landed(t, ch)
	p, err := a.History(ctx, ch, "")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// repliesLanded is Replies once the catch-up it started (if any) landed.
func (a *App) repliesLanded(t testing.TB, ctx context.Context, ch, ts string) []Message {
	t.Helper()
	if _, err := a.Replies(ctx, ch, ts); err != nil {
		t.Fatal(err)
	}
	a.landed(t, ch+"/"+ts)
	rs, err := a.Replies(ctx, ch, ts)
	if err != nil {
		t.Fatal(err)
	}
	return rs
}
