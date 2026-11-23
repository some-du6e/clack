package core

import (
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// The watchdogs' test seams (slack rtm.go, pump_watch.go). The red commit
// replaces this file with stubs: the tests then run on the code before
// them.

func stallTimers(t *testing.T, frames, ticks, every time.Duration) {
	t.Cleanup(slack.SetStallTimers(frames, ticks, every))
}

func pumpTimers(t *testing.T, after, every time.Duration) {
	t.Cleanup(setPumpTimers(after, every))
}

// forgetWake: the code before it keeps no wake.
func forgetWake(t *testing.T) {}
