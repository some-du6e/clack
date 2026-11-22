package composer

import (
	"testing"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// queuedFor counts restoreQ's entries for session a.
func queuedFor(a *core.App) (n int) {
	restoreQ.Lock()
	defer restoreQ.Unlock()
	for _, l := range restoreQ.fns {
		if l.a == a {
			n++
		}
	}
	return n
}

// TestFinishedSessionLeavesNothingQueued (internal/ui's
// TestFinishedSessionIsCollected, red on karim-1118's train): a heap dump
// found a finished session held by restoreQ, through a draft sync its
// composer queued for the UI after the process's last composer Layout.
// Once a session's core is released its composers queue nothing more,
// what they queued before neither stays (ForgetCore) nor runs, and
// another session's queued work still runs.
func TestFinishedSessionLeavesNothingQueued(t *testing.T) {
	open := func(ch string) (*core.App, *Composer) {
		app, _ := fakeslackApp(t)
		c := New(&slickHost{app: app, th: theme.New(false)}, ch, "")
		t.Cleanup(c.Close)
		return app, c
	}
	ended, ce := open("C0ENDEDSESS")   // ends: Release, then ForgetCore
	drained, cd := open("C0DRAINSESS") // ends: drained before its ForgetCore
	_, cg := open("C0GOINGSESS")       // goes on
	t.Cleanup(func() { ForgetCore(drained) })
	var ranEnded, ranDrained, ranGoing int
	ce.later(func() { ranEnded++ })
	cd.later(func() { ranDrained++ })
	cg.later(func() { ranGoing++ })
	ended.Release()
	drained.Release()
	n := queuedFor(ended) // ours, and whatever its composer's own work queued
	if n == 0 {
		t.Fatal("what the session queued before its Release isn't in restoreQ")
	}
	ce.later(func() { ranEnded++ })
	if m := queuedFor(ended); m != n {
		t.Errorf("after its session's Release a composer queued more: %d entries for it, then %d", n, m)
	}
	ForgetCore(ended)
	if n := queuedFor(ended); n != 0 {
		t.Errorf("ForgetCore left %d of the finished session's entries in restoreQ", n)
	}
	runRestores()
	if ranEnded != 0 || ranDrained != 0 {
		t.Errorf("finished sessions' queued work ran (%d, %d times), want none", ranEnded, ranDrained)
	}
	if ranGoing != 1 {
		t.Errorf("the session that goes on: its queued work ran %d times, want once", ranGoing)
	}
}
