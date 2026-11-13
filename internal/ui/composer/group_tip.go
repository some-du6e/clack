package composer

// The composer's @group tooltip, Texty's TextyTooltip for a user group
// slug: TextyTooltipUserGroup (boot-shared, build 32bb3cc8205b, read from
// the public CDN without a cookie):
//
//   - TextyInput's onMouseOver on a .c-member_slug calls showTooltip
//     (none while text is selected) and onMouseOut hides it; the
//     tooltip is a Popover opened from that handler (isOpen,
//     isOverlayInteractive false), not a BaseTooltip, so it has no hover
//     delay. showTextyTooltips is on for every Texty but search's.
//   - useUserGroupMemberIdsForTooltip(id, "texty-tooltip") (rZ8h) reads
//     the group's stored member ids; with none stored and none fetched, an
//     active group's are fetched when the tooltip mounts
//     (fetchAndUpsertUserGroupMembers: usergroups.users.list,
//     core.UsergroupTooltipIDs / UsergroupUsers), and until there are
//     members it renders nothing.
//   - Popover position top over the slug's client rect (allowance 0),
//     its TooltipTip position top: the c-tooltip__tip with its arrow, 8px
//     above the slug, centred on it.
//   - The body is TooltipMemberNames (popups.MemberNamesTip): up to 11
//     names, else 10 and "{count} others".
//
// A sent message's @group (UserGroup, fg3x: a.c-mrkdwn__user_group) has
// no tooltip in Slack: hovering it does nothing, and a click opens the
// User Group pane (messages/host.go OpenUsergroup). In the composer a
// click on the slug only places the caret, as before.

import (
	"context"
	"image"
	"time"

	"gioui.org/layout"
	"gioui.org/op"

	"github.com/some-du6e/clack/internal/ui/popups"
)

// groupTip is the composer's @group tooltip state.
type groupTip struct {
	tip  popups.TipState
	last string          // the group under the pointer last frame ("" none)
	busy map[string]bool // groups whose members are being fetched
}

// layoutGroupTip shows the tooltip over the @group slug under the
// pointer, the editor laid out at the origin.
func (c *Composer) layoutGroupTip(gtx layout.Context) {
	g := &c.gtip
	g.tip.Texty = true
	app := c.api.a
	at, r, ok := c.ed.HoverAtom()
	id := ""
	if ok && at.Kind == AtomUsergroup && app != nil {
		id = at.ID
	}
	if id != "" && id != g.last {
		// a hover mounts TextyTooltipUserGroup: its effect fetches
		if _, fetch := app.UsergroupTooltipIDs(id); fetch && !g.busy[id] {
			if g.busy == nil {
				g.busy = map[string]bool{}
			}
			g.busy[id] = true
			grp := app.StoredUsergroup(id)
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				_, _, _ = app.UsergroupUsers(ctx, grp, "") // a failure stays silent (catch {})
				c.later(func() { delete(g.busy, id) })
			}()
		}
	}
	g.last = id
	var ids []string
	if id != "" {
		ids, _ = app.UsergroupTooltipIDs(id)
	}
	th := c.h.Theme()
	if len(ids) == 0 {
		popups.TooltipBox(gtx, th, &g.tip, false, image.Point{}, popups.Above, nil)
		return
	}
	items := popups.TooltipMemberNameItems(ids, app.S.SelfID(), app.S.MemberName)
	defer op.Offset(r.Min).Push(gtx.Ops).Pop()
	popups.TooltipBox(gtx, th, &g.tip, true, r.Size(), popups.Above, func(gtx layout.Context) layout.Dimensions {
		return popups.MemberNamesTip(gtx, th, items)
	})
}
