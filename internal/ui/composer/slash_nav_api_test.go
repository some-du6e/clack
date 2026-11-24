package composer

import "github.com/some-du6e/clack/internal/core"

// The B2 tests' reads of what the fix adds (a proof run on the code
// before it replaces this file with stand-ins).

// queuedFor reports whether c has work queued for its own Layout
// (laterHere).
func queuedFor(c *Composer) bool {
	restoreQ.Lock()
	defer restoreQ.Unlock()
	for _, l := range restoreQ.fns {
		if l.here == c {
			return true
		}
	}
	return false
}

// expandedIn is a's expandables store's state for key.
func expandedIn(a *core.App, key string) (on, ok bool) { return a.Expanded(key) }
