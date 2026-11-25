//go:build !windows

package desktop

// SessionEnding is Windows' SM_SHUTTINGDOWN; elsewhere a session's end
// reaches Clack as SIGTERM or SIGHUP (cmd/clack's sessionEnding), before
// the helper starts.
func SessionEnding() bool { return false }
