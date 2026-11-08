package ui

import (
	"time"

	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op/paint"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/hovercard"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/popups"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc"
)

// profileHover is the profile hover card (Slack's ProfileHoverTrigger):
// triggers report through hovercard during layout, and the root runs the
// timers and draws the card between the content and the overlays.
type profileHover struct {
	ctl  hovercard.Controller
	card *popups.ProfileCard
	gen  int
}

// hoverBegin runs at the start of the frame, before any widget reads
// keys: it makes this App's controller the one triggers feed, and takes
// Escape while a card is open (Slack closes it from a document-level
// capture listener, so the composer never sees that Escape).
func (a *App) hoverBegin(gtx layout.Context) {
	h := &a.hover
	if h.ctl.Pointer == nil {
		h.ctl.Pointer = a.PointerPos
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
// over the page, so no trigger under them gets mouseover.
func (a *App) hoverBlocked() bool {
	if a.login != nil || slackc.HasOverlays() {
		return true
	}
	for _, o := range a.overlays {
		if o.Dismissed() {
			continue
		}
		if inv, ok := o.(interface{ Invisible() bool }); ok && inv.Invisible() {
			continue // popups' async pump draws nothing
		}
		return true
	}
	return false
}

// layoutHoverCard runs the controller after the content has laid out (so
// this frame's triggers have reported) and draws the open card.
func (a *App) layoutHoverCard(gtx layout.Context) {
	h := &a.hover
	if h.card != nil && h.card.Dismissed() { // a button on it closed it
		h.ctl.Close()
		h.card = nil
	}
	hold := h.card != nil && h.card.Holding()
	next := h.ctl.Frame(gtx.Now, a.hoverBlocked(), hold)
	req, gen, open := h.ctl.Open()
	if !open {
		h.card = nil
		hovercard.Invalidate(gtx, next)
		return
	}
	if h.card == nil || h.gen != gen {
		h.card = popups.NewHoverProfileCard(a, req.Anchor, req.UserID)
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
