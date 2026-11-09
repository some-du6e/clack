package ui

import (
	"testing"

	"gioui.org/io/key"
)

// Slack's mod+shift+h ("Toggle huddles start / stop") is bound.
func TestHuddleShortcut(t *testing.T) {
	a := &App{}
	for _, sc := range a.defaultShortcuts().list {
		if sc.name == "H" && sc.mods == key.ModShortcut|key.ModShift {
			return
		}
	}
	t.Fatal("no Ctrl/Cmd+Shift+H")
}
