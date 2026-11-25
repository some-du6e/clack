package core

import (
	"context"
	"time"
)

// Red-tree shims: the connection states' APIs don't exist on the old code.

func setTroubleThresholds(window time.Duration, after int) (restore func())   { return func() {} }
func connectionTrouble(a *App) bool                                           { return false }
func connectNow(a *App)                                                       {}
func degradedLoad(a *App, ctx context.Context, ch, threadCh, threadTS string) {}
func degradedHistRead(a *App, ch string) bool                                 { return false }
func degradedCountsAt(a *App) time.Time                                       { return time.Time{} }
func degradedHistoryWait() time.Duration                                      { return time.Second }
func evTrouble(ev Event) bool                                                 { return false }
