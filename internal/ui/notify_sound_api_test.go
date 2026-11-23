package ui

import (
	"testing"
	"time"
)

// The message sound's test seams (notify.go). The red commit replaces this
// file with stubs: the tests then run on the code before them.

func huddleOn(t *testing.T, on bool) {
	old := inHuddle
	inHuddle = func(*App) bool { return on }
	t.Cleanup(func() { inHuddle = old })
}

func quietFor(t *testing.T, d time.Duration) {
	old := soundDebounce
	soundDebounce = d
	t.Cleanup(func() { soundDebounce = old })
}
