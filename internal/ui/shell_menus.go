package ui

import (
	"gioui.org/app"

	"github.com/some-du6e/clack/internal/ui/popups"
)

// startShellMenus hands popups the window's full screen for the desktop
// shell's context menus (Copy Image, Copy Link…), which Slack ends with
// "Exit Full Screen" while the window is full screen (addToggleFullScreen).
// Both run on the UI goroutine, where the menus are built and clicked.
func (a *App) startShellMenus() {
	popups.ShellFullScreen = func() bool { return a.winMode == app.Fullscreen }
	popups.ShellExitFullScreen = func() {
		if a.win != nil {
			a.win.Option(app.Windowed.Option())
		}
	}
}
