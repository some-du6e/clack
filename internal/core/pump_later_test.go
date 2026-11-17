package core

import (
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
