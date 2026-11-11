package ui

import (
	"image"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/ui/huddleui"
	"github.com/some-du6e/clack/internal/ui/popups"
)

// huddles is the native huddle controller (internal/ui/huddleui): the
// sidebar huddle bar, the header's huddle button and the huddle window.
func (a *App) huddles() *huddleui.Controller {
	if a.hud == nil {
		huddleui.AppName = AppName
		a.hud = huddleui.New(a)
	}
	return a.hud
}

// layoutHuddleDock draws Slack's docked huddle bar (HuddleBottomBar) in
// the window's bottom row while you're in a huddle: under the rail and
// the workspace, which layoutShell and layoutBBM end above it.
func (a *App) layoutHuddleDock(gtx layout.Context) {
	hud := a.huddles()
	h := hud.DockHeight(gtx)
	if h == 0 {
		return
	}
	sz := gtx.Constraints.Max
	r := image.Rect(0, sz.Y-h, sz.X, sz.Y)
	hud.SetDockOrigin(r.Min)
	a.th.Chrome = true // the client's chrome scope, like the sidebar
	atExact(gtx, r.Min, r.Size(), func(gtx layout.Context) layout.Dimensions { return hud.LayoutDock(gtx, a.th) })
	a.th.Chrome = false
}

// StartHuddle starts or joins the huddle in a channel or DM (views'
// Huddles page calls it through its Host).
func (a *App) StartHuddle(ch string) { a.huddles().Start(ch) }

// HuddleJoinFailed is Slack's HuddleErrorModal for a start or join that
// can't happen (views' Huddles page).
func (a *App) HuddleJoinFailed() { a.huddles().JoinFailed(nil) }

// SlashHuddle is the composer's /huddle: start or join the huddle in ch
// (through Slack's startHuddle thunk, like the header button).
func (a *App) SlashHuddle(ch string) { a.huddles().Begin(ch, "") }

// OpenHuddleStart opens Slack's HuddleInviteStartModal ("Start a Huddle":
// pick who to huddle with); entry is startHuddle's entry point (the
// Create menu, the Huddles page's New huddle).
func (a *App) OpenHuddleStart(entry string) {
	if a.Core() == nil {
		a.Toast("Sign in to start a huddle")
		return
	}
	a.ShowOverlay(popups.NewHuddleStartModal(a, a.sidebarConvs(), func(ch string) { a.huddles().Begin(ch, entry) }))
}

// toggleHuddle is Cmd/Ctrl+Shift+H: leave the huddle you're in, else start
// or join the one in the conversation on screen.
func (a *App) toggleHuddle() { a.huddles().ShortcutToggle(a.router.Current().Channel()) }

// CopyText puts text on the clipboard at the next frame (huddleui's copy
// huddle link answers off the UI goroutine).
func (a *App) CopyText(s string) {
	a.Post(func() { a.copyText(s) })
}

// ScheduleHuddle opens Slack's Schedule Huddle modal for a channel or DM.
func (a *App) ScheduleHuddle(ch string) {
	a.ShowOverlay(popups.NewHuddleScheduleModal(a, ch))
}

// HuddleMemberMenu is the profile card's Huddle call menu for a person
// (popups.huddleHost).
func (a *App) HuddleMemberMenu(user string) []popups.MenuItem {
	return a.huddles().MemberMenuItems(user)
}

// openHuddleMenu opens the header caret's menu under caret (window
// coordinates), where Slack's MenuTrigger puts it (huddleui.MenuAnchor).
func (a *App) openHuddleMenu(gtx layout.Context, ch string, caret image.Rectangle) {
	items := a.huddles().MenuItems(ch)
	w := huddleui.MenuWidth(gtx, a.th, items)
	a.ShowOverlay(popups.NewMenuOpts(a, items, popups.MenuOpts{Width: w, Anchor: huddleui.MenuAnchor(gtx, caret, w), Side: popups.Below}))
}
