//go:build (linux || freebsd || openbsd) && !android && !nox11

package huddleui

import (
	"gioui.org/app"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/huddle"
)

// panelHandle is the mini panel's X11 window (X11ViewEvent). A Wayland
// window can't be placed or kept on top, so it has none.
func panelHandle(e any) (huddle.PanelHandle, bool) {
	if v, ok := e.(app.X11ViewEvent); ok && v.Window != 0 {
		return huddle.PanelHandle{View: v.Window, Display: v.Display}, true
	}
	return huddle.PanelHandle{}, false
}

// panelScale: X11 places windows in pixels.
func panelScale(m unit.Metric) float32 { return m.PxPerDp }
