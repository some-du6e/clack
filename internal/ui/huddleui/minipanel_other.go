//go:build !darwin && !windows && !((linux || freebsd || openbsd) && !android && !nox11)

package huddleui

import (
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/huddle"
)

func panelHandle(any) (huddle.PanelHandle, bool) { return huddle.PanelHandle{}, false }

func panelScale(m unit.Metric) float32 { return m.PxPerDp }
