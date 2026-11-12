package views

// The saved views' tab strip, as Slack's activity inbox draws it (gantry
// client-boot-render GuYW ActivityInboxSavedViewTabs on E/Yw
// DraggableDroppableTabs, 4gYA R, P0K2's views reducer; build
// 4d0c78085232; docs/ACTIVITY.md "Tabs"):
//
//   - Order: All pinned first, then every view, the default ones too, by
//     position compared as strings, ties in the server's order; DMs only
//     while DMs show in Activity.
//   - Badges are the tabs' unread counts (3x6A cR, client.counts
//     activity_v2): with activity_entry_layout_revisions_2 the All tab's
//     themed dot alone (16px wide, the 16px circle scaled to .4445, 8px
//     after the name); without it, All's themed count pill (18px, 0 9px,
//     12px bold) and the default views' count (content-ter, 13px, 8px
//     after; 4px after an icon alone).
//   - Without layout revisions the DMs, Mentions and Threads tabs turn to
//     their icons when the strip is narrower than 60 + 90 a default view
//     shown + 80 with any custom one + 36 (20px more to come back), the
//     name (or "{n} unread DMs") in the tab's tooltip.
//   - Every tab but All drags (react-dnd): the dragged tab at half
//     opacity, a copy of it under the pointer (1px otl-ter, radius 6,
//     base-pry, a 0 4px 12px theme-surf-sec shadow), the tab it would go
//     before marked by a 2px base-inv-hgl-1 bar on its left (the last tab:
//     on its right over its right 40%). A drop goes before that tab (4gYA
//     R on the views list, every position numbered again) and writes
//     updateActivityViewsByApi {nextViewId}: activity.views.update
//     {view_id, sort, density, next_view_id}.

import (
	"context"
	"image"
	"image/color"
	"slices"
	"strconv"
	"strings"
	"time"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// actTabStrip is the strip's own state: the drag, the tabs' places last
// frame, the icon-only latch and the icon tabs' tooltips.
type actTabStrip struct {
	tags     []actTabCtx // the tabs' drag targets
	xs, ws   []int       // each tab's x and width in the strip (last frame)
	h        int
	from     int // the tab pressed (-1)
	start    f32.Point
	dragging bool
	ptr      f32.Point // the pointer in the strip
	lastPos  string    // the last tab's drop side ("left" until hovered)
	iconOnly bool      // GuYW v.current
	tips     []popups.TipState
	moved    int // drops made (tests)
}

// actDragSlop is how far a press moves before the tab drags.
const actDragSlop = 4

// slotOrder is the views in the store's order (3x6A Ck after P0K2's
// reducer): All, then the views by position as strings, ties in the
// order activity.views answered them. Hidden default views aren't there.
func (v *Activity) slotOrder() []int {
	var out []int
	for i := 1; i < actSlotN; i++ {
		if !v.hidden[actTabIDs[i]] {
			out = append(out, i)
		}
	}
	for k := range v.customs {
		out = append(out, actSlotN+k)
	}
	slices.SortStableFunc(out, func(a, b int) int {
		if c := strings.Compare(v.slotView(a).Position, v.slotView(b).Position); c != 0 {
			return c
		}
		return v.slotIndex(a) - v.slotIndex(b)
	})
	return append([]int{0}, out...)
}

// slotIndex is a view's place in activity.views' answer (its slot number
// before the views loaded).
func (v *Activity) slotIndex(slot int) int {
	if i, ok := v.viewIdx[v.slotKey(slot)]; ok {
		return i
	}
	return slot
}

// wantSlots is GuYW R: the views in order, DMs only while DMs show in
// Activity.
func (v *Activity) wantSlots() []int {
	var out []int
	for _, s := range v.slotOrder() {
		if s == 1 && !v.dmsInActivity() {
			continue
		}
		out = append(out, s)
	}
	return out
}

// tabCounts is cR: All, DMs, Mentions and Threads' unread counts.
func (v *Activity) tabCounts() [4]int {
	c, _ := v.h.Core().ActivityTabCounts()
	return c
}

// stripIconOnly is GuYW A for the strip's width.
func (v *Activity) stripIconOnly(width int, gtx C) bool {
	c := v.h.Core()
	if !c.ActivityInbox() || c.ActivityLayoutRevisions() || width == 0 {
		v.strip.iconOnly = false
		return false
	}
	need, custom := 60, false
	for _, s := range v.wantSlots() {
		switch {
		case s == 0:
		case s < actSlotN:
			need += 90
		default:
			custom = true
		}
	}
	if custom {
		need += 80
	}
	need += 36
	f := int(float32(width) / gtx.Metric.PxPerDp)
	if v.strip.iconOnly {
		v.strip.iconOnly = f < need+20
	} else {
		v.strip.iconOnly = f < need
	}
	return v.strip.iconOnly
}

// actTabTip is GuYW G: an icon tab's tooltip, the name or its unread
// count.
func actTabTip(slot, n int) string {
	if n > 0 {
		switch slot {
		case 1:
			return nsActivity.T("{count, plural, =1 {# unread DM} other {# unread DMs}}", "count", n)
		case 2:
			return nsActivity.T("{count, plural, =1 {# unread mention} other {# unread mentions}}", "count", n)
		case 3:
			return nsActivity.T("{count, plural, =1 {# unread thread} other {# unread threads}}", "count", n)
		}
	}
	return nsActivity.T(actTabLabels[slot])
}

func actPlural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// viewTabs is the strip: 13px bold labels, 9px 8px padding, the 2px
// underline under the shown one; a custom tab leads with its emoji or
// icon (16px, 4px gap), the default ones with theirs.
func (v *Activity) viewTabs(gtx C) D {
	t := v.tabs
	st := &v.strip
	n := len(t.Labels)
	if len(st.tags) != n {
		st.tags = make([]actTabCtx, n)
		st.xs, st.ws = make([]int, n), make([]int, n)
		st.tips = make([]popups.TipState, n)
		st.from, st.dragging = -1, false
		if st.lastPos == "" {
			st.lastPos = "left"
		}
	}
	v.tabDragEvents(gtx)
	xi := v.h.Core().ActivityLayoutRevisions()
	counts := v.tabCounts()
	icons := v.stripIconOnly(gtx.Constraints.Max.X, gtx)
	target, side := v.dropTarget()
	x := 0
	h := 0
	for i := range t.Labels {
		slot := i
		if i < len(v.slots) {
			slot = v.slots[i]
		}
		count := 0
		if slot < len(counts) {
			count = counts[slot]
		}
		iconOnly := icons && slot >= 1 && slot < actSlotN
		m := op.Record(gtx.Ops)
		g := gtx
		g.Constraints.Min = image.Point{}
		d := v.viewTab(g, i, slot, count, xi, iconOnly)
		call := m.Stop()
		st.xs[i], st.ws[i] = x, d.Size.X
		h = max(h, d.Size.Y)
		func() {
			defer op.Offset(image.Pt(x, 0)).Push(gtx.Ops).Pop()
			if st.dragging && i == st.from {
				// channel_tab_is_dragging: opacity .5
				defer paint.PushOpacity(gtx.Ops, .5).Pop()
			}
			call.Add(gtx.Ops)
			if iconOnly {
				popups.Tooltip(gtx, v.th, &st.tips[i], t.clicks[i].Hovered() && !st.dragging, d.Size, popups.Below, actTabTip(slot, count), "")
			}
		}()
		x += d.Size.X
	}
	st.h = h
	if st.dragging && target >= 0 {
		// channel_tab_hovered: a 2px base-inv-hgl-1 bar (margin -2px)
		bar := gtx.Dp(2)
		bx := st.xs[target] - bar
		if side == "right" {
			bx = st.xs[target] + st.ws[target]
		}
		fill(gtx, image.Rect(bx, 0, bx+bar, h), v.dropBarColor())
	}
	if st.dragging && st.from >= 0 && st.from < n {
		v.dragPreview(gtx, st.from)
	}
	return D{Size: image.Pt(x, h)}
}

// dropBarColor is base-inv-hgl-1.
func (v *Activity) dropBarColor() color.NRGBA {
	if v.th.Dark {
		return rgb(0x0a77a7)
	}
	return rgb(0x1264a3)
}

// viewTab draws tab i: its click and drag targets, the lead, the name
// (none on an icon tab), the badge, the hover fill and the underline.
func (v *Activity) viewTab(gtx C, i, slot, count int, xi, iconOnly bool) D {
	t := v.tabs
	return t.clicks[i].Layout(gtx, func(gtx C) D {
		if v.strip.dragging {
			pointer.CursorGrabbing.Add(gtx.Ops)
		} else {
			pointerCursor(gtx)
		}
		sel := i == t.Sel
		fg := v.p.Sec
		if sel {
			fg = v.p.Text
		}
		m := op.Record(gtx.Ops)
		d := v.tabBody(gtx, slot, t.label(i), fg, count, xi, iconOnly)
		call := m.Stop()
		if t.clicks[i].Hovered() && !v.strip.dragging {
			r := gtx.Dp(8)
			rr := clip.RRect{Rect: image.Rectangle{Max: d.Size}, NE: r, NW: r}
			paint.FillShape(gtx.Ops, v.p.TabHover, rr.Op(gtx.Ops))
		}
		call.Add(gtx.Ops)
		if sel {
			h := gtx.Dp(2)
			fill(gtx, image.Rect(0, d.Size.Y-h, d.Size.X, d.Size.Y), v.p.TabLine)
		}
		// the tab's context menu (GuYW contextMenuTemplate) and its drag
		// (E/Yw DraggableDroppable; not All's: isDragDisabled)
		cl := clip.Rect(image.Rectangle{Max: d.Size}).Push(gtx.Ops)
		if i < len(v.tabCtx) {
			pass := pointer.PassOp{}.Push(gtx.Ops)
			event.Op(gtx.Ops, &v.tabCtx[i])
			pass.Pop()
		}
		if slot != 0 && i < len(v.strip.tags) {
			pass := pointer.PassOp{}.Push(gtx.Ops)
			event.Op(gtx.Ops, &v.strip.tags[i])
			pass.Pop()
		}
		cl.Pop()
		return d
	})
}

// tabBody is the tab's contents (9px 8px): the lead, the name, the badge.
func (v *Activity) tabBody(gtx C, slot int, label string, fg color.NRGBA, count int, xi, iconOnly bool) D {
	return layout.Inset{Top: 9, Bottom: 8, Left: 8, Right: 8}.Layout(gtx, func(gtx C) D {
		return row(gtx, layout.Middle,
			layout.Rigid(func(gtx C) D {
				if iconOnly {
					return v.icon(gtx, actTabIcons[slot], 16, fg)
				}
				return v.tabLead(gtx, slot, fg)
			}),
			layout.Rigid(func(gtx C) D {
				if iconOnly {
					return D{}
				}
				return v.label(gtx, 13, wBold, fg, label)
			}),
			layout.Rigid(func(gtx C) D { return v.tabBadge(gtx, slot, count, xi, iconOnly) }),
		)
	})
}

// actTabBadgeKind is GuYW O's choice: "dot" (All, layout revisions),
// "pill" (All), "count" (a default view without them), "" for none.
func actTabBadgeKind(slot, count int, xi bool) string {
	switch {
	case count <= 0 || slot >= actSlotN:
		return ""
	case slot == 0 && xi:
		return "dot"
	case slot == 0:
		return "pill"
	case xi:
		return ""
	}
	return "count"
}

// tabBadge is GuYW O.
func (v *Activity) tabBadge(gtx C, slot, count int, xi, iconOnly bool) D {
	kind := actTabBadgeKind(slot, count, xi)
	switch {
	case kind == "":
		return D{}
	case kind == "dot":
		// c-badge--dot: 16px wide, the circle scaled to .4445
		return layout.Inset{Left: 8}.Layout(gtx, func(gtx C) D {
			s := gtx.Dp(16)
			dd := int(float32(s)*.4445 + .5)
			off := (s - dd) / 2
			defer op.Offset(image.Pt(off, off)).Push(gtx.Ops).Pop()
			v.dot(gtx, unit.Dp(float32(dd)/gtx.Metric.PxPerDp), v.p.Accent)
			return D{Size: image.Pt(s, s)}
		})
	case kind == "pill":
		// the themed pill: 18px, padding 0 9px, 12px bold
		return layout.Inset{Left: 8}.Layout(gtx, func(gtx C) D {
			txt := strconv.Itoa(count)
			tw := v.th.Measure(gtx, 12, wBold, txt)
			hh, pad := gtx.Dp(18), gtx.Dp(9)
			sz := image.Pt(max(hh, tw+2*pad), hh)
			rrect(gtx, image.Rectangle{Max: sz}, hh/2, v.p.Accent)
			dirAt(gtx, image.Pt((sz.X-tw)/2, 0), func(gtx C) D {
				gtx.Constraints = layout.Exact(image.Pt(tw, hh))
				return layout.Center.Layout(gtx, func(gtx C) D { return v.label(gtx, 12, wBold, v.p.AccentText, txt) })
			})
			return D{Size: sz}
		})
	}
	// p-activity_ia4_page__unread_badge__default
	left := unit.Dp(8)
	if iconOnly {
		left = 4
	}
	return layout.Inset{Left: left}.Layout(gtx, func(gtx C) D { return v.label(gtx, 13, wReg, v.p.Muted, strconv.Itoa(count)) })
}

// tabDragEvents reads the tabs' pointer events: a press, a drag past the
// slop (the pointer is grabbed, so the tab's click doesn't fire), the
// drop.
func (v *Activity) tabDragEvents(gtx C) {
	st := &v.strip
	for i := range st.tags {
		for {
			e, ok := gtx.Event(pointer.Filter{Target: &st.tags[i], Kinds: pointer.Press | pointer.Drag | pointer.Release | pointer.Cancel})
			if !ok {
				break
			}
			pe, ok := e.(pointer.Event)
			if !ok {
				continue
			}
			at := pe.Position.Add(f32.Pt(float32(st.xs[i]), 0))
			switch pe.Kind {
			case pointer.Press:
				if pe.Buttons == pointer.ButtonPrimary {
					st.from, st.start, st.ptr, st.dragging = i, at, at, false
				}
			case pointer.Drag:
				if st.from != i {
					continue
				}
				st.ptr = at
				if !st.dragging && (abs32(at.X-st.start.X) > float32(gtx.Dp(actDragSlop)) || abs32(at.Y-st.start.Y) > float32(gtx.Dp(actDragSlop))) {
					st.dragging = true
					gtx.Execute(pointer.GrabCmd{Tag: &st.tags[i], ID: pe.PointerID})
				}
				if st.dragging {
					v.hoverSide()
					gtx.Execute(op.InvalidateCmd{})
				}
			case pointer.Release:
				if st.from == i && st.dragging {
					st.ptr = at
					v.hoverSide()
					if target, side := v.dropTarget(); target >= 0 {
						v.dropTab(st.from, target, side)
					}
					gtx.Execute(op.InvalidateCmd{})
				}
				st.from, st.dragging = -1, false
			case pointer.Cancel:
				if st.from == i {
					st.from, st.dragging = -1, false
				}
			}
		}
	}
}

func abs32(f float32) float32 {
	if f < 0 {
		return -f
	}
	return f
}

// hoverTab is the tab under the pointer that takes the drop (not All),
// -1 for none.
func (v *Activity) hoverTab() int {
	st := &v.strip
	if st.ptr.Y < 0 || st.ptr.Y >= float32(st.h) {
		return -1
	}
	for i := 1; i < len(st.xs); i++ {
		if x := st.ptr.X; x >= float32(st.xs[i]) && x < float32(st.xs[i]+st.ws[i]) {
			return i
		}
	}
	return -1
}

// hoverSide is DraggableDroppable's hover on the last tab: its left 40%
// is "left", its right 40% "right", between them the side stays.
func (v *Activity) hoverSide() {
	st := &v.strip
	i := v.hoverTab()
	if i < 0 || i != len(st.xs)-1 || st.ws[i] == 0 {
		return
	}
	rel := (st.ptr.X - float32(st.xs[i])) / float32(st.ws[i])
	switch {
	case rel < .4:
		st.lastPos = "left"
	case rel > .6:
		st.lastPos = "right"
	}
}

// dropTarget is the tab a drop would go before (shouldShowHover: not
// the dragged one) and the last tab's side.
func (v *Activity) dropTarget() (int, string) {
	st := &v.strip
	if !st.dragging {
		return -1, ""
	}
	i := v.hoverTab()
	if i < 0 || i == st.from {
		return -1, ""
	}
	if i == len(st.xs)-1 {
		return i, st.lastPos
	}
	return i, "left"
}

// actDropIndex is E/Yw calculateDropIndex for a tab on a tab.
func actDropIndex(base int, side string, last bool, from int) int {
	l := base
	if side == "left" && last {
		l--
	}
	if from < base && !last {
		l--
	}
	return l
}

// dropTab drops tab from on target: GuYW es (nothing for the same
// place), 4gYA R on the views list, then the write.
func (v *Activity) dropTab(from, target int, side string) {
	to := actDropIndex(target, side, target == len(v.strip.xs)-1, from)
	if from == to {
		return
	}
	order := v.slotOrder() // d: the store's views, which the tabs' indexes index
	if from < 0 || from >= len(order) || to < 0 || to >= len(order) {
		return
	}
	moved := order[from]
	n := slices.Delete(slices.Clone(order), from, from+1)
	n = slices.Insert(n, to, moved)
	next := ""
	if to < len(n)-1 {
		next = v.slotView(n[to+1]).ID
	}
	for i, s := range n {
		v.setSlotPosition(s, strconv.Itoa(i))
	}
	v.strip.moved++
	vw := v.slotView(moved)
	if vw.ID == "" {
		v.syncTabs()
		return
	}
	vw.Sort, vw.Density = v.viewSortAndDensity(moved)
	c := v.h.Core()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		// +9Gp logs a failed write; the new order stays
		_, _ = c.MoveActivityView(ctx, vw, next)
	}()
	v.syncTabs()
}

// setSlotPosition is P0K2 ew's position for a view.
func (v *Activity) setSlotPosition(slot int, pos string) {
	switch {
	case slot >= 0 && slot < actSlotN:
		v.views[slot].Position = pos
	case slot-actSlotN >= 0 && slot-actSlotN < len(v.customs):
		v.customs[slot-actSlotN].Position = pos
	}
}

// viewSortAndDensity is the dragged view's state as +9Gp sends it: the
// global sort and density with activity_global_settings (formatFilterViewState
// lays them over every view), else the view's own (newest, or VIP unreads
// on top with VIPs; compact).
func (v *Activity) viewSortAndDensity(slot int) (string, string) {
	vw := v.slotView(slot)
	sort, dense := vw.Sort, vw.Density
	if v.globalSettings() {
		if v.globalSort != "" {
			sort = v.globalSort
		}
		dense = "compact"
		if v.dense {
			dense = "dense"
		}
	}
	if sort == "" {
		sort = "newest"
		if len(v.h.Core().VIPs()) > 0 {
			sort = "vip_unreads_first"
		}
	}
	if dense == "" {
		dense = "compact"
	}
	return sort, dense
}

// dragPreview is DraggingTabPreviewLayer: the tab at the pointer.
func (v *Activity) dragPreview(gtx C, i int) {
	st := &v.strip
	slot := i
	if i < len(v.slots) {
		slot = v.slots[i]
	}
	m := op.Record(gtx.Ops)
	func() {
		defer op.Offset(image.Pt(int(st.ptr.X), int(st.ptr.Y))).Push(gtx.Ops).Pop()
		g := gtx
		g.Constraints.Min = image.Point{}
		rec := op.Record(gtx.Ops)
		d := v.tabBody(g, slot, v.tabs.label(i), v.p.Text, 0, true, false)
		call := rec.Stop()
		r := image.Rectangle{Max: d.Size}
		rad := gtx.Dp(6)
		sh := argb(0x12611f69) // theme-surf-sec
		if v.th.Dark {
			sh = argb(0x4c7d3986)
		}
		theme.Shadow(gtx, r.Add(image.Pt(0, gtx.Dp(4))), rad, gtx.Dp(12), sh)
		rrect(gtx, r, rad, v.p.Bg)
		call.Add(gtx.Ops)
		border(gtx, r, rad, max(1, gtx.Dp(1)), v.p.Border)
	}()
	op.Defer(gtx.Ops, m.Stop())
}
