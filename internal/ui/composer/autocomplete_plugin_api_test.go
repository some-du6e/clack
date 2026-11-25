package composer

import (
	"time"

	"github.com/some-du6e/clack/internal/taut"
)

// What the plugin answer tests set and read through APIs the round adds
// (a red tree on the code before replaces this file with stand-ins).

// setPluginTimeout sets acPluginTimeout for a test; it returns the undo.
func setPluginTimeout(d time.Duration) func() {
	old := acPluginTimeout
	acPluginTimeout = d
	return func() { acPluginTimeout = old }
}

// inFlightDispatches is how many of tt's dispatches wait on their JS.
func inFlightDispatches(tt *taut.Taut) int { return tt.InFlightDispatches() }
