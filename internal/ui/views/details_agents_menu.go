package views

// The channel details rosters' member actions (ia-details-popover-
// members chunk, build 846030e1ce97; core/exp_wave10.go and
// core/channel_managers_edit.go have the checks):
//
//   - A row of the channel (IADetailsPopoverMemberItemComponent with
//     isInChannel, not Slackbot: the Members tab's people, the Agents &
//     apps roster's agents and apps) gets its secondary content
//     (…__members_list_item_secondary_content: flex, centred, margin-right
//     24px, 28px tall): IaDetailsPopoverChannelManagerIndicator, the
//     "Channel Manager" pill for a manager (details.go managerPill), then
//     IaDetailsPopoverMemberItemActionsMenu, a 28px outline IconButton
//     (c-icon_button--size_small, --type_outline: base-pry, a 1px otl-sec
//     border, base-sec on hover; the SvgIcon ellipsis-vertical-filled at
//     16px; aria-label "More options") at the row's right edge less 16px
//     (…__members_list_item_action_menu: position absolute, right 16px),
//     shown while the row is hovered or its menu is open (visibility
//     hidden otherwise). Positioned, it paints over the pill.
//   - Its menu (MenuTrigger position "bottom-right", the 300px c-menu) has,
//     in this order, Remove from Channel Managers (the viewer may edit the
//     channel's managers and the member is one), Make Channel Manager (the
//     viewer may, and the member isn't one, a guest, external or a channel
//     visitor), Edit agent settings and the danger "Remove from channel"
//     (core.App.CanRemoveMemberFromChannel, tu's canRemoveMember); with
//     none of them the button isn't drawn. agent_channel_header (M/qt Zc)
//     hides the two Channel Manager items for an app, a bot or the native
//     channel expert, and offers Edit agent settings only for the native
//     and the Agentforce channel experts (canEditAgent: Zc, in the
//     channel, the viewer can manage the channel, the configuration not
//     locked), whose rows Clack doesn't draw.
//   - Make Channel Manager opens MakeChannelManagerModal and Remove from
//     Channel Managers RemoveManagerModal (popups/channel_managers.go);
//     their writes change the managers store, which the pills read.
//   - Remove from channel is openRemoveMemberDialog (popups/remove_member.go);
//     once the kick is done (onMemberRemoved) the row leaves the roster and
//     the counts load again.
//   - The rows have no context menu: the members list passes the dynamic
//     list no onItemContextMenu, so a right-click opens nothing.

import (
	"context"
	"image"
	"slices"

	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/popups"
)

// cdRowMenuBtn is the menu button's size (c-button--icon.c-button--medium)
// and cdRowMenuRight its inset from the row's right edge.
const (
	cdRowMenuBtn   = 28
	cdRowMenuRight = 16
	// cdRowSecondaryMargin is the secondary content's margin-right.
	cdRowSecondaryMargin = 24
)

// cdRowMenu is one row's "…" and its menu.
type cdRowMenu struct {
	btn    widget.Clickable
	press  int         // the button's press tracker tag
	local  image.Point // the last press in the button's coordinates
	size   image.Point // the button as last drawn
	open   bool        // the menu is showing (…_action_menu--visible)
	menu   *popups.Menu
	anchor image.Rectangle // the button in window coordinates when it opened
	// what the row drew last (tests): the button, the pill (row
	// coordinates) and the row's size
	shown bool
	pill  image.Rectangle
	row   image.Point
}

// takePress notes where the button was last pressed, in its own
// coordinates. The row reads it before the button's click, so a press and
// its release in one frame (a quick tap, a test's click) anchor the menu
// to this press, not the one before.
func (m *cdRowMenu) takePress(gtx C) {
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &m.press, Kinds: pointer.Press})
		if !ok {
			break
		}
		if e, ok := ev.(pointer.Event); ok {
			m.local = e.Position.Round()
		}
	}
}

func (d *ChannelDetails) rowMenu(id string) *cdRowMenu {
	ag := d.ag()
	if ag.rowMenus == nil {
		ag.rowMenus = map[string]*cdRowMenu{}
	}
	m := ag.rowMenus[id]
	if m == nil {
		m = &cdRowMenu{}
		ag.rowMenus[id] = m
	}
	return m
}

// memberRowMenuItems are the row's menu items for member u (the actions
// menu's renderMenu); removed runs after a kick (onMemberRemoved).
func (d *ChannelDetails) memberRowMenuItems(u core.User, removed func(id string)) []popups.MenuItem {
	c := d.h.Core()
	if c == nil {
		return nil
	}
	id := u.ID
	var items []popups.MenuItem
	if c.CanRemoveFromChannelManagers(d.id, id) {
		items = append(items, popups.MenuItem{Label: nsChannelDetails.T("Remove from Channel Managers"), Do: func() { d.openManagerModal(id, false) }})
	}
	if c.CanMakeChannelManager(d.id, id) {
		items = append(items, popups.MenuItem{Label: nsChannelDetails.T("Make Channel Manager"), Do: func() { d.openManagerModal(id, true) }})
	}
	if c.CanRemoveMemberFromChannel(d.id, id) {
		items = append(items, popups.MenuItem{Label: nsChannelDetails.T("Remove from channel"), Danger: true, Do: func() { d.openRemoveMember(id, removed) }})
	}
	return items
}

// agentRowMenuItems are an agent or app row's items.
func (d *ChannelDetails) agentRowMenuItems(u core.User) []popups.MenuItem {
	return d.memberRowMenuItems(u, d.agentRemoved)
}

// openManagerModal is the Channel Manager items' onSelected: the openModal
// of MakeChannelManagerModal (promote) or RemoveManagerModal, stacked.
func (d *ChannelDetails) openManagerModal(id string, promote bool) {
	ph, ok := d.h.(popups.Host)
	if !ok {
		return
	}
	if promote {
		popups.OpenMakeChannelManagerModal(ph, d.id, id)
		return
	}
	popups.OpenRemoveManagerModal(ph, d.id, id)
}

// openRemoveMember is the item's onSelected (k: openRemoveMemberDialog).
func (d *ChannelDetails) openRemoveMember(id string, removed func(id string)) {
	ph, ok := d.h.(popups.Host)
	if !ok {
		return
	}
	popups.OpenRemoveMemberDialog(ph, d.id, id, func() { removed(id) })
}

// memberRemoved is the Members tab's onMemberRemoved (a8): the row leaves
// the roster, and the counts load again.
func (d *ChannelDetails) memberRemoved(id string) {
	d.members.mutate(func(p *cdMembersPage) {
		p.users = slices.DeleteFunc(slices.Clone(p.users), func(u core.User) bool { return u.ID == id })
	})
	d.filtKey = "" // the filtered rows are rebuilt (their key counts the members in humanCount's steps)
	c, ch := d.h.Core(), d.id
	d.counts.load(d.h, true, func(ctx context.Context) (core.MemberCounts, error) { return c.MemberCounts(ctx, ch) })
	d.h.Invalidate()
}

// agentRemoved is onMemberRemoved: the row goes and the counts load again.
func (d *ChannelDetails) agentRemoved(id string) {
	ag := d.ag()
	for i, u := range ag.added {
		if u.ID == id {
			ag.added = append(ag.added[:i:i], ag.added[i+1:]...)
			break
		}
	}
	c, ch := d.h.Core(), d.id
	d.loadAgentRoster("")
	d.counts.load(d.h, true, func(ctx context.Context) (core.MemberCounts, error) { return c.MemberCounts(ctx, ch) })
	d.h.Invalidate()
}

// openRowMenu opens the row's menu under its button (bottom-right).
func (d *ChannelDetails) openRowMenu(m *cdRowMenu, items []popups.MenuItem) {
	ph, ok := d.h.(popups.Host)
	if !ok || len(items) == 0 {
		return
	}
	at := d.press.at.Sub(m.local) // the button's window origin
	anchor := image.Rectangle{Min: at, Max: at.Add(m.size)}
	m.open, m.anchor = true, anchor
	m.menu = popups.NewMenuOpts(ph, items, popups.MenuOpts{Anchor: anchor, Popover: "bottom-right",
		OnClose: func() { m.open = false; d.h.Invalidate() }})
	ph.ShowOverlay(m.menu)
}

// rowSecondary places a channel member row's secondary content in a row
// w wide: the right inset of the entity's text and, for a manager, the
// pill drawn at its place (nil without one). The row (padding 0 16px,
// flex) holds the entity (width 100%, min-width 0, 12px of its own
// padding) and the secondary content (margin-right 24px; the actions
// menu is out of flow, so only the pill sizes it). Both shrink in
// proportion to their basis, the secondary no narrower than its
// min-content (the pill's widest word), and the pill (flex-shrink 0)
// keeps its width past the secondary's right edge: Chrome's layout of
// Slack's rules puts a 116px pill's right edge 15.4px from the row's
// (2026-11-17, client-boot-styles cf726c4f2eaecc74).
// The row's menu m notes where the pill went (tests).
func (d *ChannelDetails) rowSecondary(gtx C, w int, manager bool, m *cdRowMenu) (padR unit.Dp, pill func(gtx C, row image.Point)) {
	toDp := func(v int) unit.Dp { return unit.Dp(float32(v) / gtx.Metric.PxPerDp) }
	pad, ent, margin := gtx.Dp(16), gtx.Dp(12), gtx.Dp(cdRowSecondaryMargin)
	wc := max(0, w-2*pad) // the row's content box
	m.pill = image.Rectangle{}
	if !manager {
		return toDp(w - (pad + wc - margin) + ent), nil
	}
	g := gtx
	g.Constraints.Min = image.Point{} // its own size, as a flex item
	rec := op.Record(gtx.Ops)
	pd := d.managerPill(g)
	call := rec.Stop()
	p := pd.Size.X
	sec := 0
	if wc+p > 0 {
		sec = p * max(0, wc-margin) / (wc + p)
	}
	sec = max(sec, d.managerPillMinContent(gtx))
	entityW := max(0, wc-margin-sec)
	x := pad + entityW
	return toDp(w - x + ent), func(gtx C, row image.Point) {
		at := image.Pt(x, (row.Y-pd.Size.Y)/2)
		m.pill, m.row = image.Rectangle{Min: at, Max: at.Add(pd.Size)}, row
		defer op.Offset(at).Push(gtx.Ops).Pop()
		call.Add(gtx.Ops)
	}
}

// rowMenuButton draws the row's "…" at its place in a row size wide and
// tall, when the row is hovered (hover) or the menu is open.
func (d *ChannelDetails) rowMenuButton(gtx C, m *cdRowMenu, size image.Point, hover bool) {
	m.shown, m.row = hover || m.open, size
	if !m.shown {
		return
	}
	s := gtx.Dp(cdRowMenuBtn)
	pos := image.Pt(size.X-gtx.Dp(cdRowMenuRight)-s, (size.Y-s)/2)
	defer op.Offset(pos).Push(gtx.Ops).Pop()
	m.takePress(gtx)
	g := gtx
	g.Constraints = layout.Exact(image.Pt(s, s))
	m.btn.Layout(g, func(gtx C) D {
		pointerCursor(gtx)
		r := image.Rect(0, 0, s, s)
		rad := gtx.Dp(8)
		rrect(gtx, r, rad, d.p.Bg)
		if m.btn.Hovered() || m.open {
			rrect(gtx, r, rad, d.p.Hover)
		}
		border(gtx, r, rad, max(1, gtx.Dp(1)), d.p.BorderStr)
		layout.Center.Layout(gtx, func(gtx C) D { return d.icon(gtx, "ellipsis-vertical-filled", 16, d.p.Text) })
		return D{Size: image.Pt(s, s)}
	})
	area := clip.Rect{Max: image.Pt(s, s)}.Push(gtx.Ops)
	pass := pointer.PassOp{}.Push(gtx.Ops)
	event.Op(gtx.Ops, &m.press)
	pass.Pop()
	area.Pop()
	m.size = image.Pt(s, s)
}

// AgentRowMenuItems are the labels of the row menu for member id, as the
// row would offer them (tests; nil: no button).
func (d *ChannelDetails) AgentRowMenuItems(id string) []string {
	u, ok := d.h.Core().User(id)
	if !ok {
		return nil
	}
	var out []string
	for _, it := range d.agentRowMenuItems(u) {
		out = append(out, it.Label)
	}
	return out
}

// OpenAgentRowMenu opens the row menu for member id as a click on its
// button does and returns it (tests; nil when the row has none).
func (d *ChannelDetails) OpenAgentRowMenu(id string) *popups.Menu {
	u, ok := d.h.Core().User(id)
	if !ok {
		return nil
	}
	items := d.agentRowMenuItems(u)
	if len(items) == 0 {
		return nil
	}
	m := d.rowMenu(id)
	d.openRowMenu(m, items)
	return m.menu
}

// AgentRowClickables are member id's row and its "…" button (tests).
func (d *ChannelDetails) AgentRowClickables(id string) (row, menu *widget.Clickable) {
	return dirClick(d.memberClk, "ag:"+id), &d.rowMenu(id).btn
}

// AgentRowMenuAnchor is the "…" in window coordinates when its menu last
// opened (tests).
func (d *ChannelDetails) AgentRowMenuAnchor(id string) image.Rectangle { return d.rowMenu(id).anchor }

// MemberRowMenuItems are the labels of the Members tab row menu for
// member id, as the row would offer them (tests; nil: no button).
func (d *ChannelDetails) MemberRowMenuItems(id string) []string {
	u, ok := d.h.Core().User(id)
	if !ok || id == core.SlackbotUserID {
		return nil
	}
	var out []string
	for _, it := range d.memberRowMenuItems(u, d.memberRemoved) {
		out = append(out, it.Label)
	}
	return out
}

// MemberRowClickables are a Members tab row and its "…" button (tests).
func (d *ChannelDetails) MemberRowClickables(id string) (row, menu *widget.Clickable) {
	return dirClick(d.memberClk, id), &d.rowMenu(id).btn
}

// MemberRowMenuAnchor is the "…" in window coordinates when its menu last
// opened (tests).
func (d *ChannelDetails) MemberRowMenuAnchor(id string) image.Rectangle { return d.rowMenu(id).anchor }

// MemberRowMenuShown reports whether member id's "…" was drawn in the
// last frame its row was (tests).
func (d *ChannelDetails) MemberRowMenuShown(id string) bool { return d.rowMenu(id).shown }

// MemberRowPill is where member id's row drew its Channel Manager pill,
// in the row's coordinates, the last frame it was drawn (tests; empty: no
// pill), and the row's size then.
func (d *ChannelDetails) MemberRowPill(id string) (pill image.Rectangle, row image.Point) {
	m := d.rowMenu(id)
	return m.pill, m.row
}

// ManagersLoaded reports that the details' managers read answered
// (tests).
func (d *ChannelDetails) ManagersLoaded() bool {
	_, done, _ := d.managers.get()
	return done
}

// ScrollMembersTo scrolls the Members list to member id's row, as the
// wheel would (tests; false when the list doesn't show it).
func (d *ChannelDetails) ScrollMembersTo(id string) bool {
	i := slices.Index(d.MembersShown(), id)
	if i < 0 {
		return false
	}
	first := 1 + i // after "Add people"
	if d.inviting {
		first++ // and the invite panel
	}
	d.list(cdMembers).Position = layout.Position{First: first}
	d.h.Invalidate()
	return true
}
