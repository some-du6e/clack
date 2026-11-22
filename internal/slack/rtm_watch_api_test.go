package slack

import (
	"testing"
	"time"
)

// The red commit's stubs for the watchdog's test seams: the code before
// the watchdog has none, so the tests run on its behaviour.

func watchTimers(t *testing.T, frames, ticks, every time.Duration) {}

func stallPongCheck(t *testing.T) (release func()) { return func() {} }
