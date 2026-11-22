package ui

import "gioui.org/app"

// The window's full screen for the desktop shell's context menus (Copy
// Image, Copy Link…), which Slack ends with "Exit Full Screen" while the
// window is full screen (addToggleFullScreen): popups.ShellWindow, read
// from the Host each menu gets, so every App answers for its own window
// and nothing outlives it. Both run on the UI goroutine, where the menus
// are built and clicked.

// ShellFullScreen reports whether the window is full screen.
func (a *App) ShellFullScreen() bool { return a.winMode == app.Fullscreen }

// ExitShellFullScreen leaves full screen.
func (a *App) ExitShellFullScreen() {
	if a.win != nil {
		a.win.Option(app.Windowed.Option())
	}
}
