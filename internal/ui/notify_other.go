//go:build !linux && !windows

package ui

// newNotifier: macOS has no notifier yet (UNUserNotificationCenter wants
// the signed bundle; docs/GAPS-2026-11-12.md gap 1).
func newNotifier() notifier { return nopNotifier{} }
