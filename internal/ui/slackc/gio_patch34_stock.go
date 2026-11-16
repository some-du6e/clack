//go:build stockgio

package slackc

import (
	"gioui.org/io/event"
	"gioui.org/io/input"
)

// Stock Gio v0.10.3 has no focus answers (see gio_patch34.go): a layer
// records no opener, gives no focus back and never holds the focus, so
// Escape goes to the topmost one.

func focusTag(input.Source) event.Tag                { return nil }
func canFocus(input.Source, event.Tag) bool          { return false }
func askedFocus(input.Source, event.Tag) bool        { return false }
func within(input.Source, event.Tag, event.Tag) bool { return false }
