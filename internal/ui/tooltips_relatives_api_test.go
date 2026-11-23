package ui

import (
	"image"
	"time"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// The tooltip tests' reads (tooltips_relatives_test.go).

func tipGenT() uint64 { return theme.TipGen() }

func sidebarNameTipT(a *App, gtx layout.Context, id, name string, r image.Rectangle, truncated bool) {
	a.nameRowTooltip(gtx, id, name, r, truncated)
}

func sidebarNameTipShowingT(a *App, id string, now time.Time) bool {
	st := a.sidebar.nameTips[id]
	return st != nil && st.tip.Showing(now)
}
