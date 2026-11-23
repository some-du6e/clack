//go:build !windows && !darwin

package desktop

// SystemIdleTime is powerMonitor.getSystemIdleTime, which Clack reads only
// on Windows and macOS (presence_darwin.go, taskbar_windows.go): 0 here.
func SystemIdleTime() int { return 0 }

// ScreenLocked is powerMonitor's lock-screen state, which Electron reports
// on Windows and macOS only.
func ScreenLocked() bool { return false }
