package core

import (
	"testing"
	"time"
)

// The tickle's test seams (tickle.go). The red commit replaces this file
// with stubs: the test then runs on the code before them.

func tickle(a *App, reason string) { a.Tickle(reason) }

func tickleThrottle(t *testing.T, d time.Duration) { t.Cleanup(SetTickleEvery(d)) }

func manualPresence(a *App) string {
	a.S.RLock()
	defer a.S.RUnlock()
	return a.S.Self.ManualPresence
}
