package desktop

// SessionEnding reports whether Windows is shutting down or signing the
// user out (GetSystemMetrics(SM_SHUTTINGDOWN), through the tray's
// procGetSystemMetrics): an install started now could be killed halfway.
func SessionEnding() bool {
	const smShuttingDown = 0x2000
	r, _, _ := procGetSystemMetrics.Call(smShuttingDown)
	return r != 0
}
