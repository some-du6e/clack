//go:build !windows

package ui

import (
	"gioui.org/app"
	"gioui.org/io/event"
)

// captionState: only Windows colours its native caption (caption_windows.go).
type captionState struct{}

func (a *App) syncCaption(event.Event) {}

// mainHWND is Windows' window handle; there is none here.
func (a *App) mainHWND() uintptr { return 0 }

// mainMinW and mainMinH are the main window's minimum size (Windows:
// Slack's 668×400, titlebar_windows.go).
const mainMinW, mainMinH = 800, 500

// mainWindowChrome: the window keeps its title bar outside Windows
// (macOS's hidden title bar is a separate package, gap 3–4).
func mainWindowChrome() []app.Option { return nil }

// wcoEnable: no caption buttons outside Windows (titlebar.go).
func (a *App) wcoEnable() {}

// postSysCommand: Windows' WM_SYSCOMMAND; nothing to send here.
func (a *App) postSysCommand(uint32) {}
