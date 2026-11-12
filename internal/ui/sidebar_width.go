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
	"time"

	"gioui.org/gesture"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"

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
	intent    int    // dp: the dragged width ("manual"), 0 for none
	mode      string // Slack's sidebarWidthMode: "auto" or "manual"
	lastPress time.Time
	reset     bool // the press was a double click's second: no drag follows
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

// layoutSidebarResize is the 6px drag handle on the sidebar's right edge
// (BaseResizer).
func (a *App) layoutSidebarResize(gtx layout.Context, x, y, h int) {
	s := &a.sidebar
	px := gtx.Metric.PxPerDp
	base := float32(s.width) // all events this frame share last frame's origin
	def := sidebarDefaultW(float32(a.winSize.X) / px)
	for {
		e, ok := s.drag.Update(gtx.Metric, gtx.Source, gesture.Horizontal)
		if !ok {
			break
		}
		switch e.Kind {
		case pointer.Press:
			if !s.lastPress.IsZero() && gtx.Now.Sub(s.lastPress) < 500*time.Millisecond {
				// onReset: the default width, "auto"
				s.intent, s.mode, s.lastPress, s.reset = 0, "auto", time.Time{}, true
				s.width = int(def + .5)
				a.savePrefs()
				continue
			}
			s.lastPress, s.reset = gtx.Now, false
			// the handle moves with the edge, so positions are relative to
			// the current edge: keep the press offset and accumulate.
			s.dragX = e.Position.X
			s.dragW = float32(s.width)
		case pointer.Drag:
			if s.reset {
				continue
			}
			pos := base + (e.Position.X-s.dragX)/px
			if pos >= def-sbSnap && pos <= def+sbSnap {
				s.intent, s.mode = 0, "auto"
				s.width = int(def + .5)
			} else {
				s.intent, s.mode = int(clampF(pos, sbWidthMin, sbWidthMax)+.5), "manual"
				s.width = s.intent
			}
			s.dragW = float32(s.width)
		case pointer.Release, pointer.Cancel:
			if !s.reset {
				a.savePrefs()
			}
		}
	}
	r := image.Rect(x-gtx.Dp(3), y, x+gtx.Dp(3), y+h)
	defer op.Offset(r.Min).Push(gtx.Ops).Pop()
	sz := r.Size()
	g := gtx
	g.Constraints = layout.Exact(sz)
	area := image.Rectangle{Max: sz}
	pointerArea(g, &s.drag, area, pointer.CursorColResize)
}
