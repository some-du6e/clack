package ui

// The sidebar's width, as Slack's web client sizes its Home sidebar
// column (build fa938a086569, read from the public CDN without a cookie):
//
//   - It isn't on the account. QXGe's viewLayout store has
//     persistenceEnabled false; its sidebarWidthIntent / sidebarWidthMode
//     come from localConfig_v2 (localStorage: ieP4 getByKey with /fAs
//     viewLayoutSidebarWidthIntent / viewLayoutSidebarWidthMode), and the
//     setSidebarColumn thunk writes them back there and broadcasts them to
//     the other workspaces in the window. viewLayoutLastSidebarWidth is
//     only the boot placeholder's width (XwtS renderBootPlaceholder, 260
//     when unset). Clack keeps the intent and mode per device in ui.json
//     (sidebar_w, sidebar_w_mode).
//   - "auto" (the default) is a share of the window: W2CY's
//     clamp(availableWidth × .24, 180, 450), availableWidth being b2oS's
//     window width less the workspace switcher (0 unless shown), the tab
//     rail (70) and the 4px rightPadding. A 1440 window gets 328, a 1920
//     one 443.
//   - "manual" is the dragged width, clamped to 180 … 450 (the manual
//     minimum is the same 180) at every layout.
//   - BaseResizer (client-boot-shared 7M48): a drag within 15px of the
//     default snaps to it and goes back to "auto" (calculateColumnWidth);
//     elsewhere it's the pointer's position clamped, "manual". Two presses
//     within 500 ms reset it to the default ("auto", onReset).
//
// Activity, DMs and To-dos have Slack's wide sidebar (.38, 320 … 720) and
// draw it themselves (views/dms.go).

import (
	"image"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/ui/theme"
)

const (
	sbWidthMin  = 180 // W2CY _
	sbWidthMax  = 450 // W2CY g
	sbWidthFrac = .24 // W2CY f
	sbSnap      = 15  // calculateColumnWidth: the default within ±15px
	sbRightPad  = 4   // b2oS rightPadding
	// sbLegacyDefault is the fixed width builds before auto saved to
	// ui.json for everyone, dragged or not.
	sbLegacyDefault = 232
)

// sbWidthState is the sidebar column's intent and resizer state.
type sbWidthState struct {
	intent int    // dp: the dragged width ("manual"), 0 for none
	mode   string // Slack's sidebarWidthMode: "auto" or "manual"
	rz     colResizer
}

// sidebarDefaultW is the "auto" width (dp) for a window winDp wide.
func sidebarDefaultW(winDp float32) float32 {
	avail := winDp - theme.RailW - sbRightPad
	return clampF(avail*sbWidthFrac, sbWidthMin, sbWidthMax)
}

// widthFor is the drawn width (dp) for a window winDp wide: the
// manual intent clamped, else the default.
func (s *sidebarState) widthFor(winDp float32) int {
	if s.mode == "manual" && s.intent > 0 {
		return int(clampF(float32(s.intent), sbWidthMin, sbWidthMax) + .5)
	}
	return int(sidebarDefaultW(winDp) + .5) // b2oS Math.round
}

// loadWidth reads ui.json's intent and mode. Files from before
// auto have only sidebar_w: the old default there was never dragged.
func (s *sidebarState) loadWidth(w int, mode string) {
	switch {
	case mode == "manual" && w > 0:
		s.intent, s.mode = w, "manual"
	case mode == "" && w > 0 && w != sbLegacyDefault:
		s.intent, s.mode = w, "manual"
	default:
		s.intent, s.mode = 0, "auto"
	}
}

// sidebarResizeEvents applies the resizer's drags (BaseResizer, its
// sidebar props: offset the tab rail, fromRight false) before the shell
// sizes the column, so a drag shows in the frame it arrives in. left is
// the sidebar's left edge, px in the shell's (window) frame.
func (a *App) sidebarResizeEvents(gtx layout.Context, left int) {
	s := &a.sidebar
	px := gtx.Metric.PxPerDp
	def := sidebarDefaultW(float32(gtx.Constraints.Max.X) / px)
	for {
		e, ok := s.rz.next(gtx)
		if !ok {
			break
		}
		switch e.kind {
		case resizeReset: // onReset: the default width, "auto"
			s.intent, s.mode = 0, "auto"
			a.savePrefs()
		case resizeMove:
			pos := (e.pos - float32(left)) / px
			if pos >= def-sbSnap && pos <= def+sbSnap {
				s.intent, s.mode = 0, "auto"
			} else {
				s.intent, s.mode = int(clampF(pos, sbWidthMin, sbWidthMax)+.5), "manual"
			}
		case resizeEnd:
			a.savePrefs()
		}
	}
}

// layoutSidebarResize is the 6px drag handle on the sidebar's right edge
// (BaseResizer) at x, px in the shell's frame.
func (a *App) layoutSidebarResize(gtx layout.Context, x, y, h int) {
	a.sidebar.rz.area(gtx, image.Rect(x-gtx.Dp(3), y, x+gtx.Dp(3), y+h), x)
}

// secTabWidths are the secondary pane's widths by tab: Slack keeps one
// intent and mode for the channel views (secondaryWidthIntent), one for
// Activity (secondaryWidthIntentForActivity, lJYS
// setSecondaryColumnForActivity) and one for To-dos
// (secondaryWidthIntentForLater, k0bF), each in localStorage. Clack's
// pane keeps the current tab's in secondary.width; changing tabs swaps
// it (syncSecondaryTab). 0 is auto.
type secTabWidths struct {
	cls string         // the tab whose width secondary.width holds
	w   map[string]int // the other tabs' ("": the channel views)
}

// secTabClass is which of Slack's three secondary widths a route uses.
func secTabClass(r Route) string {
	switch r.Page() {
	case "activity", "later":
		return r.Page()
	}
	return ""
}

// syncSecondaryTab swaps secondary.width when the route's tab changes.
func (a *App) syncSecondaryTab() {
	t := &a.secTabs
	c := secTabClass(a.router.Current())
	if c == t.cls {
		return
	}
	if t.w == nil {
		t.w = map[string]int{}
	}
	t.w[t.cls] = a.secondary.width
	a.secondary.width = t.w[c]
	t.cls = c
}

// secTabWidth is a tab's width for ui.json.
func (a *App) secTabWidth(cls string) int {
	if cls == a.secTabs.cls {
		return a.secondary.width
	}
	return a.secTabs.w[cls]
}
