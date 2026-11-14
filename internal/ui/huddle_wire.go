package ui

import (
	"image"
	"slices"
	"strings"

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
		a.hudLive.Store(a.hud)
	}
	return a.hud
}

// sbHuddleSync asks core for new sections when the conversations with a
// huddle on change (huddleui HuddleChannels, which core's sidebar rules
// read through SetHuddleChannels): Slack's lJBI keeps a closed DM with
// one and keeps a row its section's filter would hide (huddleChannelIds).
// The controller's Gen says cheaply whether anything moved. UI goroutine,
// each sidebar frame.
func (a *App) sbHuddleSync() {
	if a.data == nil || a.hud == nil {
		return
	}
	s := &a.sidebar
	g := a.hud.Gen()
	if g == s.hudGen {
		return
	}
	s.hudGen = g
	ids := make([]string, 0, 4)
	for id := range a.hud.HuddleChannels() {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	if k := strings.Join(ids, ","); k != s.hudKey {
		s.hudKey = k
		a.data.resnapshot(a)
	}
}

// imagesReady is the image cache's repaint callback (any goroutine): the
// main window, and the huddle windows, which draw from the same cache.
func (a *App) imagesReady() {
	a.Invalidate()
	if h := a.hudLive.Load(); h != nil {
		h.ImagesChanged()
	}
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

// JoinHuddleFrom starts or joins the huddle in ch from one of Slack's
// entry points (a huddle message's Join Huddle: huddle_message_button).
func (a *App) JoinHuddleFrom(ch, entry string) { a.huddles().Begin(ch, entry) }

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

// OpenHuddleStartWith is HuddleInviteStartModal with one of the Huddles
// page's suggestions picked (Slack's channelSuggestion /
// memberSuggestionId): the conversation, or the person's DM.
func (a *App) OpenHuddleStartWith(entry, channel, member string) {
	if a.Core() == nil {
		a.Toast("Sign in to start a huddle")
		return
	}
	cands := a.sidebarConvs()
	pick := channel
	if member != "" {
		for _, c := range a.Core().Snapshot().Convs {
			if c.Kind == "im" && c.User == member {
				pick = c.ID
				break
			}
		}
		if pick != "" && !slices.ContainsFunc(cands, func(c popups.SectionConv) bool { return c.ID == pick }) {
			name := member
			if u, ok := a.Core().User(member); ok {
				name = u.Name
			}
			cands = append([]popups.SectionConv{{ID: pick, Name: name, Icon: "dms", DM: true}}, cands...)
		}
	}
	m := popups.NewHuddleStartModal(a, cands, func(ch string) { a.huddles().Begin(ch, entry) })
	if pick != "" {
		m.Pick(pick)
	}
	a.ShowOverlay(m)
}

// ActiveHuddleChannel is activeHuddleChannelId: the conversation of the
// huddle you're in ("" when none), for the Huddles page.
func (a *App) ActiveHuddleChannel() string {
	if s := a.huddles().Session(); s != nil {
		return s.Channel()
	}
	return ""
}

// LeaveHuddle is the Huddles page's Leave (stopHuddle).
func (a *App) LeaveHuddle() { a.huddles().Leave() }

// SupportsHuddles is doesChannelSupportHuddles for the Huddles page's
// channel suggestions.
func (a *App) SupportsHuddles(ch string) bool { return a.huddles().SupportsHuddles(ch) }

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

// HuddleCallMenu is CallMenu's huddle rows for a channel (channel
// details' Start a Call ▾, views/details_call.go).
func (a *App) HuddleCallMenu(ch string) []popups.MenuItem {
	return a.huddles().CallMenuItems(ch)
}

// openHuddleMenu opens the header caret's menu under caret (window
// coordinates), where Slack's MenuTrigger puts it (huddleui.MenuAnchor).
func (a *App) openHuddleMenu(gtx layout.Context, ch string, caret image.Rectangle) {
	items := a.huddles().MenuItems(ch)
	w := huddleui.MenuWidth(gtx, a.th, items)
	a.ShowOverlay(popups.NewMenuOpts(a, items, popups.MenuOpts{Width: w, Anchor: huddleui.MenuAnchor(gtx, caret, w), Side: popups.Below}))
}
