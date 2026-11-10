package ui

import (
	"time"

	"gioui.org/layout"
	"gioui.org/unit"
)

// The secondary column's width, as Slack's main window layout works it out
// (useCalculateMainWindowClientLayout, boot-render b2oS; build
// 4d0c78085232):
//
//   - aTtQ hW: the ideal width is half the region beside the sidebar in the
//     Activity tab (wrZz isActivityTab), 40% of it with
//     secondary_view_default_width (kl02 k5, which needs
//     nested_secondary_view), else 30% of the space beside the rail
//     (sidebar included). It is clamped between 300 and 1000, or, when the
//     column can grow to fill the region (aTtQ wE: nested_secondary_view),
//     the whole space less 100.
//   - A dragged width is "manual" (viewLayoutSecondaryWidthMode) and is
//     kept, clamped the same way; otherwise the column takes the default.
//     Slack's BaseResizer snaps a drag that ends within 15px of the
//     default back to auto, and a second press within 500ms resets it.
//   - The squish (xeD9) keeps 300 for the primary view: a nested column
//     gives way down to 260, a plain one takes what is left beside 300.
//
// Not ported: the single-column mode of a region narrower than 690
// (JiF9 columnMode Single), the separate Activity and Later widths
// (viewLayoutSecondaryWidthIntentForActivity / ForLater), the sidebar's
// own squish, and activity_inbox_in_primary_view's 320 floor (an Activity
// experiment Clack doesn't read).

const (
	secWidthManual = "manual" // Slack's secondaryWidthMode once dragged
	secSnap        = 15       // dp: BaseResizer's snap to the default
	secResetPress  = 500 * time.Millisecond
)

// secondaryColumn is the column's width mode and last layout.
type secondaryColumn struct {
	manual    bool      // secondaryWidthMode "manual" (else auto)
	dragging  bool      // the edge is being dragged: the column follows it
	shown     int       // px, the width last laid out
	def       int       // px, the default width last worked out
	lastPress time.Time // the resizer's previous press (double-press reset)
}

// nestedSecondary is kl02 isNestedSecondaryViewEnabled: the experiment,
// outside the simplified layout mode.
func (a *App) nestedSecondary() bool {
	c := a.Core()
	return c != nil && !a.bbmOn() && c.NestedSecondaryView()
}

// secondaryColumnWidth is the column's width in px for a region px wide
// beside the sidebar (and the split view), the sidebar sb px wide.
func (a *App) secondaryColumnWidth(gtx layout.Context, region, sb int) int {
	sb = max(sb, 0)
	c := a.Core()
	nested := a.nestedSecondary()
	wide := nested && c.SecondaryViewDefaultWidth()
	avail := region + sb // the space beside the rail
	var ideal float32
	switch {
	case a.router.Current().Page() == "activity":
		ideal = .5 * float32(region)
	case wide:
		ideal = .4 * float32(region)
	default:
		ideal = .3 * float32(avail)
	}
	lo, hi := gtx.Dp(300), gtx.Dp(1000)
	if nested { // canSecondaryGrowToFillRegion
		hi = avail - gtx.Dp(100)
	}
	clamp := func(v int) int { return max(lo, min(v, hi)) } // SwVo q
	def := clamp(int(ideal + .5))
	w := def
	if a.secCol.manual || a.secCol.dragging {
		w = clamp(gtx.Dp(unit.Dp(a.secondary.width)))
	}
	if region-w <= gtx.Dp(300) { // the squish: the primary view keeps 300
		if nested {
			w = max(min(w, region-gtx.Dp(300)), gtx.Dp(260))
		} else {
			w = max(region-gtx.Dp(300), gtx.Dp(260))
		}
	}
	a.secCol.shown, a.secCol.def = w, def
	return w
}

// secondaryResizePress starts a drag of the column's edge: the drag runs
// from the width on screen. A second press within 500ms is BaseResizer's
// reset to the default.
func (a *App) secondaryResizePress(gtx layout.Context, now time.Time) (reset bool) {
	if !a.secCol.lastPress.IsZero() && now.Sub(a.secCol.lastPress) < secResetPress {
		a.secCol.lastPress = time.Time{}
		a.secCol.manual, a.secCol.dragging = false, false
		return true
	}
	a.secCol.lastPress = now
	if a.secCol.shown > 0 {
		a.secondary.width = int(float32(a.secCol.shown)/gtx.Metric.PxPerDp + .5)
	}
	a.secCol.dragging = true
	return false
}

// secondaryResizeEnd ends a drag: within 15dp of the default it is auto
// again, else manual. It reports whether a drag was running.
func (a *App) secondaryResizeEnd(gtx layout.Context) bool {
	if !a.secCol.dragging {
		return false
	}
	a.secCol.dragging = false
	def := float32(a.secCol.def) / gtx.Metric.PxPerDp
	d := float32(a.secondary.width) - def
	a.secCol.manual = d > secSnap || d < -secSnap
	return true
}

// NestedSecondaryView is views.NestedSecondary.
func (a *App) NestedSecondaryView() bool { return a.nestedSecondary() }
