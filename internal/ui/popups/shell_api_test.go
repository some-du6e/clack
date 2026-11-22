package popups

// The shell menu tests reach the window's full screen only through this.

// setShellFullScreen sets the ui root's full screen hooks for a test.
func setShellFullScreen(on func() bool, exit func()) {
	ShellFullScreen, ShellExitFullScreen = on, exit
}
