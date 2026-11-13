//go:build !darwin || !cgo

package desktop

// DoNotDisturb is macOS Do Not Disturb / Focus; elsewhere it is never on.
func DoNotDisturb() bool { return false }
