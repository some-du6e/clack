//go:build windows

package huddleui

import (
	"gioui.org/app"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/huddle"
)

// panelHandle is the mini panel's HWND (Win32ViewEvent).
func panelHandle(e any) (huddle.PanelHandle, bool) {
	if v, ok := e.(app.Win32ViewEvent); ok && v.HWND != 0 {
		return huddle.PanelHandle{View: v.HWND}, true
	}
	return huddle.PanelHandle{}, false
}

// panelScale: Clack is per-monitor DPI aware, so windows sit in pixels.
func panelScale(m unit.Metric) float32 { return m.PxPerDp }
