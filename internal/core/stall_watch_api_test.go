package core

import (
	"testing"
	"time"
)

// The red commit's stubs for the watchdogs' test seams: the code before
// them has none, so the tests run on its behaviour.

func stallTimers(t *testing.T, frames, ticks, every time.Duration) {}

func pumpTimers(t *testing.T, after, every time.Duration) {}
