package messages

import (
	"image"

	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/ui/icons"
)

// PersistActions reports whether messages always show their collapsed
// actions menu: Simplified layout mode's "Always show collapsed menu"
// (zgdY: bbmSettings.persistentActions && BBM). The ui root sets it.
var PersistActions = func() bool { return false }

// actionsPad is what the persistent menu takes from the body column:
// .c-message_kit__actions--bbm .c-message_kit__gutter__right
// {padding-right:44px}, over gutter__right's 8px in a channel (0 in a
// thread, whose gutter keeps its own 24px).
func (rc *rowCtx) actionsPad(gtx layout.Context) int {
	if !rc.persist {
		return 0
	}
	if rc.thread {
		return dp(gtx, 44)
	}
	return dp(gtx, 36)
}

// persistentActions draws every visible message's "Message actions"
// button, the one MessageActions renders without hover when persistActions
// is on (shouldRenderActions; no quick reactions, no other buttons, the
// ellipsis labelled "Message actions"): .c-message_kit__actions--bbm
// .c-message__actions{top:min(50% - 14px,12px);right:16px}, the group
// radius 8 with no padding, the button 28px (.c-message__actions-button--bbm,
// font-size 16px). It opens the message's menu in its context form
// (menuUsageLocation CONTEXT: the hover toolbar's group, then the rest).
func (s *listState) persistentActions(gtx layout.Context, h Host, rc *rowCtx, size image.Point) {
	c := rc.c
	th := h.Theme()
	if s.pbtn == nil {
		s.pbtn = map[string]*widget.Clickable{}
	}
	bs := dp(gtx, 28)
	for i := s.visible[0]; i < s.visible[1] && i < len(s.items); i++ {
		it := &s.items[i]
		if it.kind != itMsg || len(it.group) > 0 || s.heights[i] <= 0 {
			continue
		}
		r := s.rows[it.key]
		if r == nil || r.edit != nil || r.m.Pending || r.m.Failed || r.m.Tombstone {
			continue
		}
		btn := s.pbtn[it.key]
		if btn == nil {
			btn = &widget.Clickable{}
			s.pbtn[it.key] = btn
		}
		y := s.pos[i] + min(s.heights[i]/2-dp(gtx, 14), dp(gtx, 12))
		x := size.X - dp(gtx, 16) - bs
		if btn.Clicked(gtx) {
			at := s.anchor(h, image.Pt(x, y+bs), image.Point{})
			h.ShowOverlay(s.messageMenu(h, r, at))
		}
		rr := image.Rect(x, y, x+bs, y+bs)
		shadowCard(gtx.Ops, rr, dp(gtx, 8), dp(gtx, 1), c.Bg, c.PillBorder, c.Shadow)
		at(gtx, x, y, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Exact(image.Pt(bs, bs))
			return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				col := c.ToolbarIcon
				if btn.Hovered() {
					rrect(gtx.Ops, image.Rectangle{Max: image.Pt(bs, bs)}, dp(gtx, 8), c.ToolbarHoverBg)
					col = c.Text
				}
				is := dp(gtx, 16)
				at(gtx, (bs-is)/2, (bs-is)/2, func(gtx layout.Context) layout.Dimensions {
					return icons.Get("kebab")(gtx, unit.Dp(16), col)
				})
				return layout.Dimensions{Size: image.Pt(bs, bs)}
			})
		})
		if btn.Hovered() {
			tooltip(gtx, th, c, "Message actions", image.Pt(x+bs/2, y-dp(gtx, 4)), size)
		}
	}
}
