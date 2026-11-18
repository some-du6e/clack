package ui

// The detach tests' ways to what came with the per-runtime sessions
// (tautSession), through these alone, so the tests build against code from
// before them.

import (
	"testing"

	"github.com/some-du6e/clack/internal/taut"
	"github.com/some-du6e/clack/internal/ui/slackc"
)

// slackcHasStyle reports whether slackc has a sheet under key.
func slackcHasStyle(key string) bool { return slackc.Styles.Has(key) }

// svSecondTaut builds a second session's runtimes on e's backend, as a
// newer session would (not installed), and what takes it down again.
func svSecondTaut(t *testing.T, e *svEnv) (*taut.Taut, func()) {
	t.Helper()
	b := e.a.data
	tt, s, sess := b.buildTaut(e.a, false, b.teamID, ttGen)
	if tt == nil {
		t.Fatal("no second runtime")
	}
	return tt, func() { e.a.detachSession(sess); closeTaut(s, tt) }
}
