package core

import (
	"testing"
	"time"
)

// The red commit's stubs for the tickle's test seams: the code before them
// sends no tickle and keeps no manual presence.

func tickle(a *App, reason string) {}

func tickleThrottle(t *testing.T, d time.Duration) {}

func manualPresence(a *App) string { return "" }
