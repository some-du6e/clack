package ui

import (
	"testing"
	"time"
)

// The message sound's test seams (notify.go), as the code before
// soundQuietOver has them: its stretch timer's func deleted whatever held
// the key.

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

// lateExpiry is the old stretch timer's func.
func lateExpiry(a *App, k string, _ *time.Timer) {
	a.soundMu.Lock()
	delete(a.soundQuiet, k)
	a.soundMu.Unlock()
}
