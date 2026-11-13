package ui

import (
	"context"
	"time"

	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op/paint"

	"github.com/some-du6e/clack/internal/ui/blockkit"
	"github.com/some-du6e/clack/internal/ui/hovercard"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// profileHover is the profile hover card (Slack's ProfileHoverTrigger):
// triggers report through hovercard during layout, and the root runs the
// timers and draws the card between the content and the overlays.
type profileHover struct {
	ctl  hovercard.Controller
	card *popups.ProfileCard
	prev *popups.PreviewCard      // a link preview's hover card (Request.Preview)
	chc  *popups.ChannelHoverCard // a channel's (Request.ChannelID)
	gen  int
	over bool // drawn above the overlays (the top one hosts the trigger)
}

// hoverBegin runs at the start of the frame, before any widget reads
// keys: it makes this App's controller the one triggers feed, and takes
// Escape while a card is open (Slack closes it from a document-level
// capture listener, so the composer never sees that Escape).
func (a *App) hoverBegin(gtx layout.Context) {
	h := &a.hover
	if h.ctl.Pointer == nil {
		h.ctl.Pointer = a.PointerPos
		a.installAppCardHooks()
	}
	hovercard.SetDefault(&h.ctl)
	if _, _, open := h.ctl.Open(); !open {
		return
	}
	for {
		e, ok := gtx.Event(key.Filter{Name: key.NameEscape})
		if !ok {
			break
		}
		if ke, ok := e.(key.Event); ok && ke.State == key.Press {
			h.ctl.Close()
		}
	}
}

// hoverBlocked reports whether something other than the card's own menu
// covers the window: Slack's menus, modals and popovers put an overlay
// over the page, so no trigger under them gets mouseover. The top overlay
// may host triggers itself (hoverHost: the channel details dialog's
// member list): then it doesn't block, and the card draws above it.
func (a *App) hoverBlocked() bool {
	if a.login != nil || slackc.HasOverlays() {
		return true
	}
	top := a.hoverTop()
	if top == nil {
		return false
	}
	h, ok := top.(hoverHost)
	return !ok || !h.HoverCards()
}

// hoverHost is an overlay whose own content has profile triggers.
type hoverHost interface{ HoverCards() bool }

// HoverCards: the channel details dialog's Members tab has Slack's
// ProfileHoverTrigger on each name (MediumMemberEntity showHoverCard).
func (o detailsOverlay) HoverCards() bool { return true }

// hoverTop is the topmost overlay that draws something (nil: none).
func (a *App) hoverTop() Overlay {
	for i := len(a.overlays) - 1; i >= 0; i-- {
		o := a.overlays[i]
		if o == nil || o.Dismissed() {
			continue
		}
		if inv, ok := o.(interface{ Invisible() bool }); ok && inv.Invisible() {
			continue // popups' async pump draws nothing
		}
		return o
	}
	return nil
}

// hoverOverOverlay reports whether the card belongs above the overlays
// this frame (the top overlay hosts the triggers).
func (a *App) hoverOverOverlay() bool {
	if a.hover.card != nil && a.hover.card.Holding() {
		return a.hover.over // keep it where it was while its menu is open
	}
	top := a.hoverTop()
	if top == nil {
		return false
	}
	h, ok := top.(hoverHost)
	return ok && h.HoverCards()
}

// layoutHoverCard runs the controller after the content has laid out (so
// this frame's triggers have reported) and draws the open card, unless
// the top overlay hosts the triggers (layoutHoverCardOver draws it then).
func (a *App) layoutHoverCard(gtx layout.Context) {
	a.hover.over = a.hoverOverOverlay()
	if a.hover.over {
		return
	}
	a.hoverCardFrame(gtx)
}

// layoutHoverCardOver draws the card above the overlays when one of them
// hosts its trigger (after layoutOverlays, so their triggers reported).
func (a *App) layoutHoverCardOver(gtx layout.Context) {
	if a.hover.over {
		a.hoverCardFrame(gtx)
	}
}

func (a *App) hoverCardFrame(gtx layout.Context) {
	h := &a.hover
	if h.card != nil && h.card.Dismissed() { // a button on it closed it
		h.ctl.Close()
		h.card = nil
	}
	hold := h.card != nil && h.card.Holding()
	next := h.ctl.Frame(gtx.Now, a.hoverBlocked(), hold)
	req, gen, open := h.ctl.Open()
	if !open {
		h.card, h.prev, h.chc = nil, nil, nil
		hovercard.Invalidate(gtx, next)
		return
	}
	if req.ChannelID != "" {
		// ChannelHoverCard: a message's #channel (HoverCardTrigger,
		// position "top") or a composer slug (TextyTooltip)
		h.card, h.prev = nil, nil
		if h.chc == nil || h.gen != gen {
			h.chc = popups.NewChannelHoverCard(a, req.Anchor, req.ChannelID, req.Buttons, req.Texty)
			h.gen = gen
		}
		g := gtx
		g.Constraints = layout.Constraints{Max: gtx.Constraints.Max}
		func() {
			if o := h.ctl.Opacity(gtx.Now); o < 1 {
				defer paint.PushOpacity(g.Ops, o).Pop()
			}
			h.chc.Layout(g)
		}()
		if h.chc.Dismissed() { // View Channel: closeCard
			h.ctl.Close()
			h.chc = nil
			return
		}
		h.ctl.CardArea(g, h.chc.Rect())
		if !next.IsZero() && !next.After(gtx.Now.Add(time.Minute)) {
			hovercard.Invalidate(gtx, next)
		}
		return
	}
	h.chc = nil
	if pc, ok := req.Preview.(popups.PreviewContent); ok {
		// MessageAttachmentDefaultV3's HoverCardTrigger: the preview's
		// card in popups' frame, position "top"
		h.card = nil
		if h.prev == nil || h.gen != gen {
			h.prev = popups.NewPreviewCard(a, req.Anchor, pc)
			h.gen = gen
		}
		g := gtx
		g.Constraints = layout.Constraints{Max: gtx.Constraints.Max}
		func() {
			if o := h.ctl.Opacity(gtx.Now); o < 1 {
				defer paint.PushOpacity(g.Ops, o).Pop()
			}
			h.prev.Layout(g)
		}()
		h.ctl.CardArea(g, h.prev.Rect())
		if !next.IsZero() && !next.After(gtx.Now.Add(time.Minute)) {
			hovercard.Invalidate(gtx, next)
		}
		return
	}
	h.prev = nil
	if h.card == nil || h.gen != gen {
		if req.Texty {
			h.card = popups.NewTextyHoverCard(a, req.Anchor, req.UserID)
		} else {
			h.card = popups.NewHoverCard(a, req.Anchor, req.UserID, req.BotID)
		}
		h.gen = gen
	}
	g := gtx
	g.Constraints = layout.Constraints{Max: gtx.Constraints.Max}
	func() {
		if o := h.ctl.Opacity(gtx.Now); o < 1 {
			defer paint.PushOpacity(g.Ops, o).Pop()
		}
		h.card.Layout(g)
	}()
	if h.card.Dismissed() {
		h.ctl.Close()
		h.card = nil
		return
	}
	h.ctl.CardArea(g, h.card.Rect())
	if next.IsZero() || next.After(gtx.Now.Add(time.Minute)) {
		return
	}
	hovercard.Invalidate(gtx, next)
}

// installAppCardHooks wires the hover cards' actions: About this app
// opens the app's DM on its About tab, a slash command row types the
// command into the open conversation's composer; the channel card's
// huddle badge and the channel-tab slugs' card rule.
func (a *App) installAppCardHooks() {
	popups.OpenAppAbout = func(uid string) { a.openAppTab(uid, "about") }
	popups.OpenAppTab = a.openAppTab
	// ChannelHoverCardHuddleBadge is the sidebar suffix's badge (huddleui
	// Badge), and a channel-tab slug opens the channel card where Slack's
	// does (core.ChannelTabSlugCard)
	popups.ChannelHuddleBadge = func(gtx layout.Context, th *theme.Theme, ch string, right, rowH int) int {
		return a.huddles().CardBadge(gtx, th, ch, right, rowH)
	}
	richview.TabSlugCard = func(ch, tab string) bool {
		c := a.Core()
		return c != nil && c.ChannelTabSlugCard(ch, tab)
	}
	popups.ComposerInsert = func(s string) bool {
		ch := a.router.Current().Channel()
		if ch == "" {
			return false
		}
		cv := a.convos[ch+"/"]
		if cv == nil || cv.comp == nil {
			return false
		}
		ins, ok := cv.comp.(interface{ InsertText(string) })
		if !ok {
			return false
		}
		ins.InsertText(s)
		return true
	}
}

// openAppTab opens the app's DM (its bot user uid) on a tab: "about"
// (the app card's About this app), "home" or "messages" (App details'
// buttons; Slack's openAppSpace with tabId).
func (a *App) openAppTab(uid, tab string) {
	if a.data == nil || a.data.core == nil {
		return
	}
	c := a.data.core
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		cv, err := c.OpenDM(ctx, uid)
		a.Post(func() {
			if err != nil || cv.ID == "" {
				a.Toast("Couldn't open the app")
				return
			}
			a.Navigate("c/" + cv.ID)
			if dm := blockkit.AppDMFor(a, cv.ID); dm != nil {
				dm.SetTab(tab)
			}
		})
	}()
}
