package ui

import (
	"testing"
	"time"
)

// The red commit's stubs for the message sound's test seams: the code
// before them has none, so the tests run on its behaviour.

func huddleOn(t *testing.T, on bool) {}

func quietFor(t *testing.T, d time.Duration) {}
