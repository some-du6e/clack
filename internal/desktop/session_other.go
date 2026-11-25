//go:build !windows

package desktop

// SessionEnding is Windows' SM_SHUTTINGDOWN, checked by the helper once
// Clack exited. Elsewhere the session's end reaches Clack itself before
// the helper starts: on macOS as the quit Apple Event of a log out,
// restart or shut down (Gio's TerminateEvent.SessionEnd), on Linux as SIGTERM or SIGHUP or
// the display going (cmd/clack's sessionEnds).
func SessionEnding() bool { return false }
