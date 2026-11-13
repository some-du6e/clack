//go:build stockgio

package richview

import (
	"gioui.org/io/input"
	"gioui.org/io/pointer"
)

// Stock Gio v0.10.3 has no middle-click autoscroll (see gio_patch14.go).

func autoscrollPress(pointer.Event) bool { return false }

func autoscrollVeto(input.Source, pointer.Event) {}
