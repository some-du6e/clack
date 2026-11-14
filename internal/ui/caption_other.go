//go:build !windows

package ui

import (
	"gioui.org/io/event"
)

// captionState: only Windows colours its native caption (caption_windows.go).
type captionState struct{}

func (a *App) syncCaption(event.Event) {}

// mainHWND is Windows' window handle; there is none here.
func (a *App) mainHWND() uintptr { return 0 }

// wcoEnable: no caption buttons outside Windows (titlebar.go).
func (a *App) wcoEnable() {}

// postSysCommand: Windows' WM_SYSCOMMAND; nothing to send here.
func (a *App) postSysCommand(uint32) {}
