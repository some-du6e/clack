//go:build !linux && !windows && !darwin

package ui

// newNotifier: no desktop notifications on this OS.
func newNotifier() notifier { return nopNotifier{} }
