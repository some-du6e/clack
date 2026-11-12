//go:build !windows

package ui

import "gioui.org/io/event"

// captionState: only Windows colours its native caption (caption_windows.go).
type captionState struct{}

func (a *App) syncCaption(event.Event) {}

// mainHWND is Windows' window handle; there is none here.
func (a *App) mainHWND() uintptr { return 0 }
