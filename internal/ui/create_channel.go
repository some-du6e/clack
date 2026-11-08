package ui

import (
	"image"

	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/ctxclick"
)

// Where Slack's web client (build a84662d2d135, 2026-11-08) opens its
// "Create a channel" modal (boot-shared Gznj openChannelCreateModal), and
// Clack's copy of each:
//
//   - The Channels section's footer row "Add channels" (boot-render, the
//     sidebar's add-more link): a menu with "Create a new channel" and
//     "Browse channels" (entryPoint channel_sidebar_add_more).
//   - A section heading's ⋯ and right-click menus
//     (getSectionHeadingEllipsisMenuTemplate / …ContextMenuTemplate): the
//     section's name, then "Create a channel" for Channels, custom and
//     Starred sections (the new channel stays in that section), and
//     "Browse channels" for Channels.
//   - The Channels heading's hover "+" (SectionMenuPlusButton, tooltip
//     "Create a channel"): straight to the modal.
//   - The rail's "+" (the IA4 Create menu, 360px, header "Create"):
//     "Message" (Start a conversation in a DM or channel, Ctrl+N) and
//     "Channel" (Start a group conversation by topic). Huddle, Canvas, List
//     and Workflow rows are left out: Clack can't create those.
//   - The channel browser's "Create Channel" (Directories → Channels).

var (
	mkCreateChannel func(a *App, section string) Overlay
	// mkMenuAt opens a menu width dp wide next to anchor (window px) on
	// side (popups.Side: 0 below, 2 right).
	mkMenuAt func(a *App, anchor image.Rectangle, side int, width float32, items []MenuItem) Overlay
)

// OpenCreateChannel opens the create-channel modal (views' Directories
// button finds it by this name).
func (a *App) OpenCreateChannel() { a.openCreateChannel("") }

// openCreateChannel opens the modal for a channel that should land in
// section ("" = Channels).
func (a *App) openCreateChannel(section string) {
	if a.data == nil || mkCreateChannel == nil {
		a.Toast("Sign in to create a channel")
		return
	}
	a.ShowOverlay(mkCreateChannel(a, section))
}

func (a *App) menuAt(anchor image.Rectangle, side int, width float32, items []MenuItem) {
	if mkMenuAt != nil {
		a.ShowOverlay(mkMenuAt(a, anchor, side, width, items))
		return
	}
	a.menu(anchor.Min, items)
}

// openAddChannelsMenu is the Channels footer's "Add channels" menu.
func (a *App) openAddChannelsMenu(anchor image.Point) {
	a.menu(anchor, []MenuItem{
		{Label: "Create a new channel", Do: func() { a.openCreateChannel("") }},
		{Label: "Browse channels", Do: func() { a.Navigate("directories?tab=channels") }},
	})
}

// openCreateMenu is the rail "+" Create menu.
func (a *App) openCreateMenu(anchor image.Rectangle) {
	a.menuAt(anchor, 2, 360, []MenuItem{
		{Label: "Create", Disabled: true},
		{Label: "Message", Icon: "compose", Subtitle: "Start a conversation in a DM or channel", Hint: "Ctrl+N", Do: a.compose},
		{Label: "Channel", Icon: "channel", Subtitle: "Start a group conversation by topic", Do: a.OpenCreateChannel},
	})
}

// sectionMenuItems is what a section heading's menu offers here.
func (a *App) sectionMenuItems(sec *sbSection) []MenuItem {
	var items []MenuItem
	switch sec.Type {
	case "channels", "standard", "stars":
		id := sec.ID
		if sec.Type == "channels" {
			id = ""
		}
		items = append(items, MenuItem{Label: "Create a channel", Icon: "channel", Do: func() { a.openCreateChannel(id) }})
	}
	if sec.Type == "channels" {
		items = append(items, MenuItem{Label: "Browse channels", Icon: "search", Do: func() { a.Navigate("directories?tab=channels") }})
	}
	if len(items) == 0 {
		return nil
	}
	return append([]MenuItem{{Label: sec.Name, Disabled: true}}, items...)
}

func (a *App) sectionByID(id string) *sbSection {
	m := a.model()
	for i := range m.Sections {
		if m.Sections[i].ID == id {
			return &m.Sections[i]
		}
	}
	return nil
}

// openSectionMenu opens a heading's menu at anchor.
func (a *App) openSectionMenu(id string, anchor image.Point) {
	sec := a.sectionByID(id)
	if sec == nil {
		return
	}
	if items := a.sectionMenuItems(sec); items != nil {
		a.menu(anchor, items)
	}
}

// sbHeadingTag is a heading's right-click tag.
func (s *sidebarState) headingTag(id string) *int {
	if s.headTags == nil {
		s.headTags = map[string]*int{}
	}
	t := s.headTags[id]
	if t == nil {
		t = new(int)
		s.headTags[id] = t
	}
	return t
}

// sbHeadingControls handles a section heading's right-click and, while it
// is hovered, draws its ⋯ (and, on Channels, +) buttons at the row's right
// (sz: the row, px).
func (a *App) sbHeadingControls(gtx layout.Context, r *sbRow, hovered bool, sz image.Point) {
	tag := a.sidebar.headingTag(r.key)
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: tag, Kinds: pointer.Press})
		if !ok {
			break
		}
		if e, ok := ev.(pointer.Event); ok && ctxclick.Press(e) {
			a.openSectionMenu(r.key, a.ptrWin)
		}
	}
	func() {
		defer clip.Rect{Max: sz}.Push(gtx.Ops).Pop()
		defer pointer.PassOp{}.Push(gtx.Ops).Pop()
		event.Op(gtx.Ops, tag)
	}()
	sec := a.sectionByID(r.key)
	if sec == nil || a.sectionMenuItems(sec) == nil {
		return
	}
	more, plus := a.clicks.get("sbh.more."+r.key), a.clicks.get("sbh.plus."+r.key)
	if !hovered && !more.Hovered() && !plus.Hovered() {
		return
	}
	c := &a.sbPal
	bs := gtx.Dp(24)
	x := sz.X - gtx.Dp(8) - bs
	y := (sz.Y - bs) / 2
	hover := c.SidebarHover
	if sec.Type == "channels" {
		at(gtx, image.Pt(x, y), image.Pt(bs, bs), func(gtx layout.Context) layout.Dimensions {
			return iconButton(gtx, plus, "plus", 24, 16, c.SidebarText, hover, 6)
		})
		x -= bs + gtx.Dp(2)
	}
	at(gtx, image.Pt(x, y), image.Pt(bs, bs), func(gtx layout.Context) layout.Dimensions {
		return iconButton(gtx, more, "more", 24, 16, c.SidebarText, hover, 6)
	})
	if clicked(plus) {
		a.openCreateChannel("")
	}
	if clicked(more) {
		a.openSectionMenu(r.key, a.ptrWin)
	}
}
