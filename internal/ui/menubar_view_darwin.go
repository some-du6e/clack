//go:build darwin && cgo

package ui

import (
	"gioui.org/app"
	"gioui.org/io/event"

	"github.com/some-du6e/clack/internal/ui/menubar"
)

func init() {
	// The main window's view, for File > Close (menubar.SetMainView).
	menuViewEvent = func(ev event.Event) {
		if e, ok := ev.(app.AppKitViewEvent); ok && e.View != 0 {
			menubar.SetMainView(e.View)
		}
	}
}
