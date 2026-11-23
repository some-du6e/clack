package ui

import (
	"testing"
	"time"
)

// The red commit's stubs for the presence tickles' test seams: the code
// before them sends no tickle and polls nothing.

func presenceTimers(t *testing.T, throttle, poll time.Duration) {}

func systemIdle(t *testing.T, secs int) func(int) { return func(int) {} }

func screenLocked(t *testing.T, on bool) func(bool) { return func(bool) {} }
