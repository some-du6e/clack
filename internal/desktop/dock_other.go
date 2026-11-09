//go:build !darwin || !cgo

package desktop

// SetDockBadge is the macOS Dock badge; elsewhere there is none.
func SetDockBadge(string) {}

// BounceDock is the macOS Dock bounce; elsewhere there is none.
func BounceDock(bool) {}
