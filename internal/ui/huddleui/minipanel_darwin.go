//go:build darwin

package huddleui

import (
	"gioui.org/app"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/huddle"
)

// panelHandle is the mini panel's NSView (AppKitViewEvent).
func panelHandle(e any) (huddle.PanelHandle, bool) {
	if v, ok := e.(app.AppKitViewEvent); ok && v.View != 0 {
		return huddle.PanelHandle{View: v.View}, true
	}
	return huddle.PanelHandle{}, false
}

// panelScale: macOS places windows in points.
func panelScale(unit.Metric) float32 { return 1 }
