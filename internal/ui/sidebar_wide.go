package ui

// Slack's wide sidebars: the column Activity and the DMs list show beside
// the tab rail, read from build 0e695dd18302 on the public CDN without a
// cookie:
//
//   - It is the grid's sidebar column at W2CY's sizes for the tab. zfm1
//     tabHasWideSidebar (Activity, DMs, Later; Clack's To-dos page has no
//     list column) is isWideWidth: clamp(availableWidth × .38, 320, 720)
//     for "auto" (the default), the dragged intent clamped to the same
//     320 … 720 for "manual". tabHasExtraWideSidebar (Search and
//     ActivityInbox, the Activity tab under activity_inbox: 5Ns/ Lh) is
//     isExtraWideWidth: availableWidth × .5 (.24 with the split view)
//     within 320 … availableWidth less 300 for the primary view, 260 for
//     an open secondary view and 300 for the split view. availableWidth
//     is the Home sidebar's (b2oS: the window less the switcher, the 70px
//     tab rail and the 4px rightPadding): a 1440 window gets a 519 wide
//     and a 683 extra wide column.
//   - Each has its own intent and mode: VWZV's layout hook hands the
//     sidebar resizer sidebarWideWidthIntent / Mode (or …ExtraWide…) on
//     those tabs (sidebarMin 320), and its onResizeEnd / onReset dispatch
//     tm5Y setSidebarWideColumn (+65F setSidebarExtraWideColumn), which
//     write them to localConfig_v2 (localStorage: /fAs
//     viewLayoutSidebarWideWidthIntent / Mode, viewLayoutSidebarExtraWide…)
//     and broadcast them to the window's other workspaces. QXGe's
//     viewLayout store has persistenceEnabled false: nothing goes to
//     users.prefs, so the widths stay on the device. Clack keeps them in
//     ui.json (sidebar_wide_w / _mode, sidebar_extra_wide_w / _mode);
//     dragging one never moves the Home sidebar's or the other's.
//   - The resizer is the Home sidebar's BaseResizer (7M48): a drag
//     within 15px of the default snaps to it and goes back to "auto",
//     elsewhere it's the pointer's position clamped, "manual"; two
//     presses within 500 ms reset it to the default ("auto", onReset).
//   - The window squishes it (xeD9): when the primary view beside it
//     would be 300px or less, the column gives way so primary keeps 300,
//     down to its 320 minimum (sidebarWidthMin).
//   - Hiding the Home sidebar doesn't hide it (b2oS collapses the sidebar
//     for isSidebarVisible false only on Home-like tabs), but a press on
//     its resizer shows the Home sidebar again (onSidebarResizeStart).

import (
	"image"

	"gioui.org/layout"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/theme"
)

const (
	sbWideMin  = 320 // W2CY isWideWidth _ (and the extra wide minimum)
	sbWideMax  = 720 // W2CY g
	sbWideFrac = .38 // W2CY f
	// the extra wide column: .5 of the space, .24 beside the split view;
	// its maximum leaves the primary view 300, a secondary view 260 and
	// the split view 300
	sbXWideFrac      = .5
	sbXWideSplitFrac = .24
	sbXWideSecondary = 260
	// sbWidePrimaryMin is the primary view's floor (W2CY, the squish xeD9).
	sbWidePrimaryMin = 300
)

// sbWideState is a wide sidebar's intent and resizer state.
type sbWideState struct {
	intent int    // dp: the dragged width ("manual"), 0 for none
	mode   string // Slack's sidebar…WidthMode: "auto" or "manual"
	rz     colResizer
}

// wideBounds are a wide column's limits and "auto" width (dp) in
// availableWidth avail (W2CY).
type wideBounds struct{ lo, hi, def float32 }

// sidebarWideBounds is isWideWidth's: 320 … 720, .38.
func sidebarWideBounds(avail float32) wideBounds {
	return wideBounds{sbWideMin, sbWideMax, clampF(avail*sbWideFrac, sbWideMin, sbWideMax)}
}

// sidebarXWideBounds is isExtraWideWidth's, the primary view showing
// (Clack's Activity always shows the item or ActivityEmptyView there).
func sidebarXWideBounds(avail float32, secondary, split bool) wideBounds {
	hi := avail - sbWidePrimaryMin
	f := float32(sbXWideFrac)
	if secondary {
		hi -= sbXWideSecondary
	}
	if split {
		hi -= splitMinW
		f = sbXWideSplitFrac
	}
	// SwVo q is max(lo, min(hi, v)): the minimum wins a narrow window
	return wideBounds{sbWideMin, max(hi, sbWideMin), clampF(avail*f, sbWideMin, hi)}
}

// widthFor is the width (dp) before the squish: the manual intent
// clamped, else the default.
func (s *sbWideState) widthFor(b wideBounds) int {
	if s.mode == "manual" && s.intent > 0 {
		return int(clampF(float32(s.intent), b.lo, b.hi) + .5)
	}
	return int(b.def + .5) // b2oS Math.round
}

// load reads ui.json's intent and mode.
func (s *sbWideState) load(w int, mode string) {
	if mode == "manual" && w > 0 {
		s.intent, s.mode = w, "manual"
		return
	}
	s.intent, s.mode = 0, "auto"
}

// wideState is the extra wide column's state or the wide one's.
func (a *App) wideState(extra bool) *sbWideState {
	if extra {
		return &a.sbXWide
	}
	return &a.sbWide
}

// wideBoundsNow are the column's bounds for this window.
func (a *App) wideBoundsNow(gtx layout.Context, extra bool) wideBounds {
	avail := float32(a.winSize.X)/gtx.Metric.PxPerDp - theme.RailW - sbRightPad
	if extra {
		return sidebarXWideBounds(avail, a.secondary.open() && !a.bbmOn(), a.splitOpen())
	}
	return sidebarWideBounds(avail)
}

// WideSidebarWidth is views.WideSidebar: the resizer's drags first (the
// column follows the pointer in the frame a move arrives in), then the
// width for this window, squished for a region total px wide. Positions
// are the gtx frame's, whose x = 0 is the column's left edge (BaseResizer
// with the rail as its offset).
func (a *App) WideSidebarWidth(gtx layout.Context, total int, extra bool) int {
	s := a.wideState(extra)
	px := gtx.Metric.PxPerDp
	b := a.wideBoundsNow(gtx, extra)
	for {
		e, ok := s.rz.next(gtx)
		if !ok {
			break
		}
		switch e.kind {
		case resizeStart:
			// VWZV onSidebarResizeStart: a mouse press dispatches
			// toggleSidebar({isSidebarVisible: true}), so a hidden Home
			// sidebar comes back on the next Home tab
			a.sidebar.hidden = false
		case resizeReset: // onReset: the default width, "auto"
			s.intent, s.mode = 0, "auto"
			a.savePrefs()
		case resizeMove: // calculateColumnWidth: min sidebarMin, max the column's
			pos := e.pos / px
			if pos >= b.def-sbSnap && pos <= b.def+sbSnap {
				s.intent, s.mode = 0, "auto"
			} else {
				s.intent, s.mode = int(clampF(pos, b.lo, b.hi)+.5), "manual"
			}
		case resizeEnd:
			a.savePrefs()
		}
	}
	w := gtx.Dp(unit.Dp(s.widthFor(b)))
	if floor := gtx.Dp(sbWidePrimaryMin); total-w <= floor {
		w = max(total-floor, gtx.Dp(unit.Dp(b.lo)))
	}
	return max(min(w, total), 0)
}

// WideSidebarHandle is views.WideSidebar: the 6px drag handle on the
// column's right edge at x (px, the view's frame), as the Home sidebar's.
func (a *App) WideSidebarHandle(gtx layout.Context, x, h int, extra bool) {
	a.wideState(extra).rz.area(gtx, image.Rect(x-gtx.Dp(3), 0, x+gtx.Dp(3), h), x)
}
