package slack

import (
	"context"
	"time"
)

// setAwaitOnline sets the RTM's WAIT_FOR_CONNECTIVITY hook (the red tree's
// shim leaves it alone: the old RTM has none).
func setAwaitOnline(r *RTM, f func(ctx context.Context) bool) { r.AwaitOnline = f }

func setOnTrouble(r *RTM, f func(on bool)) { r.OnTrouble = f }

func rtmTrouble(r *RTM) bool { return r.Trouble() }

func rtmWaiting(r *RTM) bool { return r.waiting.Load() }

func connectNow(r *RTM) { r.ConnectNow() }

func setTroubleThresholds(window time.Duration, after int) (restore func()) {
	return SetTroubleThresholds(window, after)
}
