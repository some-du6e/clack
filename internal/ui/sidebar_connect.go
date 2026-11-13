package ui

import (
	"context"
	"image"
	"time"

	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"

	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Slack Connect in the sidebar, as Slack's ConnectedChannel row draws it
// (boot-render PZNh, boot-shared Un00 BaseChannelName, web client build
// of 2026-11-12). The row's prefix icon has no shared variant
// (getChannelPrefixKind / UGOO getIconTypeForChannelPrefix: public,
// private, archived, mpim…), so the markers are:
//
//   - an external DM: BaseChannelNameImName's externalOrgNameForMember
//     (the partner isExternal, their team known: its name) after the
//     person's name, span.p-channel_sidebar__member_label.caption:
//     margin-left 8px, 13px regular (.caption, line-height 1.25),
//     theme-content-inv-ter (theme-content-hgl-1 on the selected row);
//   - an externally shared channel: the name's tooltip,
//     SharedChannelTooltipContent (popups.SharedChannelTip), the external
//     teams' icons and "{externalTeamName} is in this channel".
//
// Not ported: "externally-shared-pending" (a channel frozen in its grace
// period after a Slack Connect trial, F9tY isFrozenAndInGracePeriod, which
// Clack doesn't read) and "org-shared" (Enterprise Grid's multi-workspace
// channels).

// sbTeam is a team's name and its icon for px, asking core for a team it
// doesn't know yet (Slack's getTeamById fetches a missing team); the row
// draws it once it arrives.
func (a *App) sbTeam(id string, px int) (name, icon string, ok bool) {
	if a.data == nil || a.data.core == nil || id == "" {
		return "", "", false
	}
	c := a.data.core
	if name, icon, ok = c.Team(id, px); ok {
		return name, icon, true
	}
	if a.sidebar.teamAsked == nil {
		a.sidebar.teamAsked = map[string]bool{}
	}
	if !a.sidebar.teamAsked[id] {
		a.sidebar.teamAsked[id] = true
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			c.EnsureTeam(ctx, id)
			a.Post(func() { a.Invalidate() })
		}()
	}
	return "", "", false
}

// sbOrgLabel draws an external DM partner's org name 8px after the name
// (whose baseline is base, from the row's top) in at most avail px, and
// returns the width it took (0: no label).
func (a *App) sbOrgLabel(gtx layout.Context, conv *convInfo, x, base, avail int, sel bool) int {
	if conv.PeerTeam == "" || conv.Self {
		return 0
	}
	name, _, ok := a.sbTeam(conv.PeerTeam, gtx.Dp(32))
	if !ok || name == "" {
		return 0
	}
	gap := gtx.Dp(8)
	if avail <= gap {
		return 0
	}
	col := theme.Over(theme.WithAlpha(a.th.C.SidebarText, 0xa3), a.sbBase) // theme-content-inv-ter
	if sel {
		col = a.th.C.SidebarActiveText // theme-content-hgl-1
	}
	m := op.Record(gtx.Ops)
	lg := gtx
	lg.Constraints = layout.Constraints{Max: image.Pt(avail-gap, gtx.Dp(40))}
	d := a.th.Label(lg, 13, theme.Regular, col, name)
	call := m.Stop()
	top := base - (d.Size.Y - d.Baseline) // the caption shares the name's baseline
	w := min(d.Size.X, avail-gap)
	st := op.Offset(image.Pt(x+gap, top)).Push(gtx.Ops)
	cl := clip.Rect{Max: image.Pt(w, d.Size.Y)}.Push(gtx.Ops)
	call.Add(gtx.Ops)
	cl.Pop()
	st.Pop()
	return gap + w
}

// sharedRowTooltip is BaseChannelName's tooltip over an externally shared
// channel's name (r: the name's bounds in the row; truncated: the name
// didn't fit, so the sentence names the channel).
func (a *App) sharedRowTooltip(gtx layout.Context, conv *convInfo, name string, r image.Rectangle, truncated bool) {
	if !conv.Shared || len(conv.Teams) == 0 || r.Empty() {
		return
	}
	if a.sidebar.sharedTips == nil {
		a.sidebar.sharedTips = map[string]*codeRowTip{}
	}
	st := a.sidebar.sharedTips[conv.ID]
	if st == nil {
		st = &codeRowTip{}
		st.tip.Delay = 600 * time.Millisecond // as the code channel's (BaseChannelName)
		st.tip.MaxWidth = 200                 // .c-tooltip__tip max-width
		a.sidebar.sharedTips[conv.ID] = st
	}
	t := op.Offset(r.Min).Push(gtx.Ops)
	defer t.Pop()
	pass := pointer.PassOp{}.Push(gtx.Ops)
	area := clip.Rect(image.Rectangle{Max: r.Size()}).Push(gtx.Ops)
	st.hover.Add(gtx.Ops)
	area.Pop()
	pass.Pop()
	hovered := st.hover.Update(gtx.Source)
	var teams []popups.SharedTeam
	count := len(conv.Teams)
	if hovered {
		// 6a40 y: the first three teams the client knows (a missing one
		// is asked for and counted once it's known)
		count = 0
		for _, id := range conv.Teams {
			n, icon, ok := a.sbTeam(id, gtx.Dp(40))
			if !ok {
				continue
			}
			count++
			if len(teams) < 3 {
				teams = append(teams, popups.SharedTeam{Name: n, Icon: icon})
			}
		}
	}
	popups.SharedChannelTip(gtx, a, a.th, &st.tip, hovered && len(teams) > 0, r.Size(), teams, count, name, truncated)
}
