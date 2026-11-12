//go:build !windows

package huddleui

import (
	"gioui.org/app"
	"gioui.org/io/event"
)

// captionState: only Windows colours its native caption (caption_windows.go).
type captionState struct{}

func (c *captionState) sync(event.Event, *app.Window, captionLook) {}
