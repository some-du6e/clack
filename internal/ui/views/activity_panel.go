package views

// The persistent detail panel, as Slack lays the inbox out with
// activity_inbox_remove_persistent_detail_panel off (client-boot 5Ns/ Bb,
// VlLO EnsureActivityInboxMiddleware, L1Ms, gantry GMkG
// handleOnClickActivityItem, client-activity-empty-view Ezzs; build
// 4d0c78085232; docs/ACTIVITY.md "Detail panel"):
//
//   - Bb is on with activity_inbox, nested secondary views
//     (nested_secondary_view, not under agentic_ia) and the experiment:
//     the inbox is the Primary view at the full width and an opened item
//     is a secondary view beside it (Activity.main).
//   - Otherwise the inbox lives in the Sidebar container, the column the
//     DMs tab's list uses (Clack's measurements of it: 38% of the
//     workspace, 300 to 640px), with its own colors (the
//     p-view_contents--sidebar rules keep the tabs content-sec /
//     content-pry), and Primary always shows something: the item opened
//     (a click navigates Primary, no split view: GMkG eG is false), or
//     ActivityEmptyView, "Select a notification to view the details."
//     (p-empty_page: centred; 15px/22px, content-ter, at most 320px).

import (
	"image"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/text"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// pageWidth is the Activity page's width: the view's, or its sidebar
// column's with the persistent panel.
func (v *Activity) pageWidth(gtx C) int {
	if v.pageW > 0 {
		return v.pageW
	}
	return gtx.Constraints.Max.X
}

// actSidebarW is the Sidebar container's width (dms.go's measurements).
func actSidebarW(gtx C, total int) int {
	w := int(float32(total) * dmsPaneFrac)
	w = max(min(w, gtx.Dp(dmsPaneMax)), gtx.Dp(dmsPaneMin))
	return min(w, total)
}

// persistentLayout draws the page in the sidebar column and Primary
// beside it.
func (v *Activity) persistentLayout(gtx C) {
	size := gtx.Constraints.Max
	lw := actSidebarW(gtx, size.X)
	v.pageW = lw
	func() {
		g := gtx
		g.Constraints = layout.Exact(image.Pt(lw, size.Y))
		defer clipMax(g).Pop()
		layout.Flex{Axis: layout.Vertical}.Layout(g,
			layout.Rigid(v.head),
			layout.Flexed(1, v.listArea),
		)
	}()
	line := max(1, gtx.Dp(1))
	fill(gtx, image.Rect(lw, 0, lw+line, size.Y), v.p.Border)
	func() {
		pr := image.Rect(lw+line, 0, size.X, size.Y)
		defer op.Offset(pr.Min).Push(gtx.Ops).Pop()
		g := gtx
		g.Constraints = layout.Exact(pr.Size())
		if v.selOK {
			v.pane(g)
			return
		}
		v.activityEmpty(g)
	}()
}

// activityEmpty is ActivityEmptyView.
func (v *Activity) activityEmpty(gtx C) D {
	defer clipMax(gtx).Pop()
	size := gtx.Constraints.Max
	fill(gtx, image.Rectangle{Max: size}, v.p.Bg)
	return layout.Center.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Max.X = min(gtx.Constraints.Max.X, gtx.Dp(320))
		gtx.Constraints.Min.X = 0
		gtx.Constraints.Min.Y = 0
		return v.th.Layout(gtx, theme.TextStyle{Size: 15, Weight: wReg, Color: v.p.Muted, LineHeight: 22, Align: text.Middle},
			"Select a notification to view the details.")
	})
}
