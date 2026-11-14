//go:build !windows && !darwin

package ui

import "gioui.org/app"

// mainMinW and mainMinH are the main window's minimum size (Windows and
// macOS: Slack's 668×400, titlebar_windows.go and titlebar_darwin.go).
const mainMinW, mainMinH = 800, 500

// mainWindowChrome: the window keeps its title bar outside Windows and
// macOS.
func mainWindowChrome() []app.Option { return nil }
