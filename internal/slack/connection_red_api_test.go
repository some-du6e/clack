package slack

import (
	"context"
	"time"
)

// Red-tree shims: the connection states' APIs don't exist on the old code.

func setAwaitOnline(r *RTM, f func(ctx context.Context) bool) {}
func setOnTrouble(r *RTM, f func(on bool))                    {}
func rtmTrouble(r *RTM) bool                                  { return false }
func rtmWaiting(r *RTM) bool                                  { return true }
func connectNow(r *RTM)                                       {}
func setTroubleThresholds(window time.Duration, after int) (restore func()) {
	return func() {}
}
