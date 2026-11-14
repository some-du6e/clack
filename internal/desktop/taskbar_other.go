//go:build !windows

package desktop

// The taskbar button's overlay badge and frame flash are Windows'
// (taskbar_windows.go); elsewhere there is no taskbar button to set.

// SetTaskbarOverlay is Windows' taskbar overlay badge.
func SetTaskbarOverlay(uintptr, int, string) error { return nil }

// FlashFrame is Windows' FlashWindowEx.
func FlashFrame(uintptr, bool) {}

// WindowVisible is Windows' IsWindowVisible.
func WindowVisible(uintptr) bool { return true }

// ShowInactive is Windows' SW_SHOWNOACTIVATE.
func ShowInactive(uintptr) {}

// Minimize is Windows' SC_MINIMIZE.
func Minimize(uintptr) {}

// SystemIdleTime is Windows' GetLastInputInfo idle time.
func SystemIdleTime() int { return 0 }

// ToastsEnabled is Windows' ToastNotifier.Setting.
func ToastsEnabled() bool { return true }

// MoveToCurrentDisplay is Windows' MOVE_WINDOW_TO_PRIMARY_SCREEN move.
func MoveToCurrentDisplay(uintptr) bool { return false }
