package ui

import (
	"image"

	"gioui.org/layout"
	"gioui.org/op"

	"github.com/some-du6e/clack/internal/ui/huddleui"
)

// layoutHuddlesLinkFacepile draws the sidebar Huddles link's
// .facepile__RJWDO (Slack's HuddlesSidebarLink, boot-shared YCI0; the
// rules are in huddleui/huddles_link.go) in a row rw wide and rowH tall,
// and returns where the name has to end (right, the row's default,
// without one). The box is 80px wide at the link's right padding (8px,
// 6px with --has-badge while an invite shows), 8px after the name (the
// flex gap; the name's margin-right auto pushes the box right). In it, at
// its right edge: the unread invites' count badge under huddles_link_v2,
// else the huddle's three faces.
func (a *App) layoutHuddlesLinkFacepile(gtx layout.Context, rw, rowH, right int) int {
	hud := a.huddles()
	l := hud.Link()
	if l.Channel == "" {
		return right
	}
	pad := gtx.Dp(8)
	if l.Unread {
		pad = gtx.Dp(6) // .p-channel_sidebar__link--page.p-channel_sidebar__link--has-badge
	}
	boxR := rw - pad
	boxL := boxR - huddleui.LinkFacepileWidth(gtx)
	if l.Badge > 0 {
		// Badge count={y} className=huddleBadge__PcXv3: the c-badge pill
		// in theme-base-imp / theme-content-imp, centred in the 20px box
		c := &a.sbPal
		m := op.Record(gtx.Ops)
		bd := a.badge(gtx, l.Badge, c.Badge, c.BadgeText)
		call := m.Stop()
		t := op.Offset(image.Pt(boxR-bd.Size.X, (rowH-bd.Size.Y+1)/2)).Push(gtx.Ops)
		call.Add(gtx.Ops)
		t.Pop()
	} else {
		hud.LinkFaces(gtx, a.th, l.Channel, boxR, rowH)
	}
	return min(right, boxL-gtx.Dp(8))
}
