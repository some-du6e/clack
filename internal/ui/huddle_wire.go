package ui

import (
	"image"

	"gioui.org/layout"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/huddleui"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/popups"
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

// StartHuddle starts or joins the huddle in a channel or DM (views'
// Huddles page calls it through its Host).
func (a *App) StartHuddle(ch string) { a.huddles().Start(ch) }

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
