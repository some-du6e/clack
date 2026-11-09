package ui

import (
	"image"

	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/ctxclick"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/popups"
)

// Where Slack's web client (build a84662d2d135, 2026-11-08) opens its
// "Create a channel" modal (boot-shared Gznj openChannelCreateModal), and
// Clack's copy of each:
//
//   - The Channels section's footer row "Add channels" (boot-render, the
//     sidebar's add-more link): a menu with "Create a new channel" and
//     "Browse channels" (entryPoint channel_sidebar_add_more).
//   - A section heading's ⋯ and right-click menus: "Create a channel" for
//     Channels, custom and Starred sections (the new channel stays in that
//     section), and "Browse channels" for Channels (section_menu.go has
//     the whole menus).
//   - The Channels heading's hover "+" (SectionMenuPlusButton, tooltip
//     "Create a channel"): straight to the modal.
//   - The rail's "+" (the IA4 Create menu): "Channel" (openCreateMenu has
//     the whole menu).
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

// openCreateMenu is the rail "+" Create menu (boot-render CreateMenu +
// nx8t useCreateMenuActions; 360px, header "Create"). Rows and gates:
//
//   - Message (compose; "Start a conversation in a DM or channel", Ctrl+N).
//   - Channel (channel; "Start a group conversation by topic"), for users
//     who may create channels.
//   - Huddle (headphones; "Start a video or audio chat"; huddles enabled,
//     not frontline): Slack opens HuddleInviteStartModal, "Start a
//     Huddle", and starts the huddle in the picked conversation.
//   - Canvas (add-channel-canvas; "Create and share content", Ctrl+Shift+N
//     on paid teams): createAndOpenDocInNewWindow, a standalone canvas.
//   - List (lists; "Track and manage projects"; lists enabled).
//   - Workflow (play; "Automate everyday tasks"; who may build workflows):
//     Workflow Builder.
//   - Notes (microphone; "Record meeting notes"; meeting_assistant, off for
//     the test account).
//   - a separator, then "Invite people" (user-add) for users who may
//     invite; "Workspace" only on SPF non-work workspaces.
//
// Clack draws every row the account has. Lists, Workflow Builder, meeting
// notes and invitations have no Clack screens yet, so those rows say so in
// a toast, like Clack's other unavailable features, instead of doing
// nothing.
func (a *App) openCreateMenu(anchor image.Rectangle) {
	unavailable := func(what string) func() {
		return func() { a.Toast(what + " in " + AppName + " yet") }
	}
	items := []MenuItem{
		{Label: "Create", Header: true},
		{Key: "compose", Label: "Message", Icon: "compose", Subtitle: "Start a conversation in a DM or channel", Hint: "Ctrl+N", Do: a.compose},
		{Key: "create-channel", Label: "Channel", Icon: "channel", Subtitle: "Start a group conversation by topic", Do: a.OpenCreateChannel},
		{Key: "start_huddle", Label: "Huddle", Icon: "headphones", Subtitle: "Start a video or audio chat", Do: a.openHuddleStart},
		{Key: "create_canvas", Label: "Canvas", Icon: "add-channel-canvas", Subtitle: "Create and share content", Hint: "Ctrl+Shift+N", Do: a.CreateCanvas},
		{Key: "create_list", Label: "List", Icon: "lists", Subtitle: "Track and manage projects", Do: unavailable("Lists aren’t available")},
		{Key: "create-workflow", Label: "Workflow", Icon: "play", Subtitle: "Automate everyday tasks", Do: unavailable("Workflow Builder isn’t available")},
	}
	if a.exp("meeting_assistant") == "on" {
		items = append(items, MenuItem{Key: "create-notes", Label: "Notes", Icon: "microphone", Subtitle: "Record meeting notes",
			Do: unavailable("Meeting notes aren’t available")})
	}
	items = append(items, MenuItem{Divider: true},
		MenuItem{Key: "invite", Label: "Invite people", Icon: "user-add", Do: unavailable("Inviting people isn’t available")})
	a.menuAt(anchor, 2, 360, items)
}

// openHuddleStart is the Create menu's Huddle: "Start a Huddle" picks the
// conversation, then the huddle starts there (StartHuddle, the same path
// as a channel header's huddle button).
func (a *App) openHuddleStart() {
	if a.Core() == nil {
		a.Toast("Sign in to start a huddle")
		return
	}
	a.ShowOverlay(popups.NewHuddleStartModal(a, a.sidebarConvs(), a.StartHuddle))
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
			a.openSectionContextMenu(r.key, a.ptrWin)
		}
	}
	func() {
		defer clip.Rect{Max: sz}.Push(gtx.Ops).Pop()
		defer pointer.PassOp{}.Push(gtx.Ops).Pop()
		event.Op(gtx.Ops, tag)
	}()
	sec := a.sectionByID(r.key)
	if sec == nil { // every section has a ⋯ menu (section_menu.go)
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
