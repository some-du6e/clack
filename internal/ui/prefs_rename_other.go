//go:build !windows

package ui

// renameRefused is false off Windows: a POSIX rename replaces a file other
// handles have open, so a rename that fails (a read-only folder, a full
// disk) fails for good (writePrefsFile).
func renameRefused(string, error) bool { return false }
