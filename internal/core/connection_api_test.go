package core

import (
	"context"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// The connection states' APIs the core tests use, through helpers (the
// red tree's shim stubs them: the old code has none of them).

func setTroubleThresholds(window time.Duration, after int) (restore func()) {
	return slack.SetTroubleThresholds(window, after)
}

func connectionTrouble(a *App) bool { return a.ConnectionTrouble() }

func connectNow(a *App) { a.ConnectNow() }

func degradedLoad(a *App, ctx context.Context, ch, threadCh, threadTS string) {
	a.DegradedLoad(ctx, ch, threadCh, threadTS)
}

// degradedHistRead reports whether the banner's load read ch's history
// (its throttle noted it).
func degradedHistRead(a *App, ch string) bool {
	a.degraded.mu.Lock()
	defer a.degraded.mu.Unlock()
	_, ok := a.degraded.histAt[ch+"/"]
	return ok
}

func degradedCountsAt(a *App) time.Time {
	a.degraded.mu.Lock()
	defer a.degraded.mu.Unlock()
	return a.degraded.countsAt
}

func degradedHistoryWait() time.Duration { return degradedHistoryEvery }

func evTrouble(ev Event) bool { return ev.Trouble }
