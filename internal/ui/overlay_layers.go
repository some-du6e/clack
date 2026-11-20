package ui

import (
	"image"
	"log"
	"reflect"

	"gioui.org/io/event"
	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/ui/refocus"
	"github.com/some-du6e/clack/internal/ui/slackc"
)

// The shell's overlays (menus, pickers, Slack's modals, the profile, Block
// Kit views) are layers in the main window's overlay stack (slackc
// layers.go), with the plugins' Popovers, dialogs and overlay roots. Their
// Escape stays the shell's (its binding closes the topmost, dismissTop);
// a plugin's layer opened over them, or from inside one, gets it first.
// A layer that closes gives the focus back to what had it when it opened.

// shellLayer is one shell overlay as a layer.
type shellLayer struct {
	a    *App
	o    Overlay
	area int
}

// LayerEscape (slackc.Layer) is an Escape that reached the overlay (one
// a plugin's layer it opened passed on): it closes it as the shell's
// Escape binding closes the topmost (dismissTop).
func (l *shellLayer) LayerEscape() { l.a.dismissOverlay(l.o) }

// invisible reports an overlay that draws nothing (the popups pump): no
// layer, and no Escape of its own.
func invisible(o Overlay) bool {
	inv, ok := o.(interface{ Invisible() bool })
	return ok && inv.Invisible()
}

// peeking reports a hover peek (a rail tab's, the Focus button's, a peek
// menu until it's pinned): no layer, as it never takes the focus and has
// none to give back.
func peeking(o Overlay) bool {
	p, ok := o.(interface{ Peeking() bool })
	return ok && p.Peeking()
}

// drawOverlay lays out overlay o as a layer: inside its area, so a focus
// in it is in the layer, with its focus home when it has one.
func (a *App) drawOverlay(gtx layout.Context, o Overlay) {
	// a layer of the inspect tool's record too: what o draws reports o as
	// its region, and its card is its box (slackc.InspectCard)
	if slackc.Inspecting() && !invisible(o) {
		lm := slackc.InspectOverlay(overlayLayerName(o))
		// a popup that gave its layer no box (no InspectCard): its own
		// Rect, so a pick on its card where nothing in it was recorded
		// outlines the card under the popup's region. One with neither
		// is logged once (TestOverlayKindsBoxTheirLayer finds them
		// statically; this is for what it can't see)
		defer func() {
			if !lm.EndCardOr(gtx, overlayRect(o)) {
				unboxedOverlay(o)
			}
		}()
	}
	if invisible(o) || peeking(o) || !reflect.TypeOf(o).Comparable() {
		o.Layout(gtx)
		return
	}
	l := a.overlayLayers[o]
	if l == nil {
		if a.overlayLayers == nil {
			a.overlayLayers = map[Overlay]*shellLayer{}
		}
		l = &shellLayer{a: a, o: o}
		a.overlayLayers[o] = l
	}
	spec := slackc.LayerSpec{Area: &l.area}
	if h, ok := o.(interface{ FocusHome() event.Tag }); ok {
		spec.Home = h.FocusHome()
	}
	slackc.DrawLayer(gtx, l, spec, func(gtx layout.Context) { o.Layout(gtx) })
}

// overlayRect is o's card as it says (Rect, window coords), empty for an
// overlay that doesn't.
func overlayRect(o Overlay) image.Rectangle {
	if r, ok := o.(interface{ Rect() image.Rectangle }); ok {
		return r.Rect()
	}
	return image.Rectangle{}
}

// unboxedSeen are the overlay layers (overlayLayerName: a wrapper's by
// what it wraps) that ended a session's frame with no box (UI goroutine):
// each is logged once.
var unboxedSeen = map[string]bool{}

func unboxedOverlay(o Overlay) {
	if _, ok := unwrapOverlay(o).(interface{ InspectNoCard() }); ok {
		return // no card by design (the profile preview's dim)
	}
	if name := overlayLayerName(o); !unboxedSeen[name] {
		unboxedSeen[name] = true
		log.Printf("inspect: overlay %s drew no card box (no InspectCard, no Rect): picks on it report the window under it", name)
	}
}

// pruneOverlayLayers forgets the layers of overlays no longer shown.
func (a *App) pruneOverlayLayers() {
	for o := range a.overlayLayers {
		shown := false
		for _, x := range a.overlays {
			if x == o {
				shown = true
				break
			}
		}
		if !shown {
			delete(a.overlayLayers, o)
		}
	}
}

// dismissOverlay closes overlay o, as dismissTop closes the topmost.
func (a *App) dismissOverlay(o Overlay) {
	for i := len(a.overlays) - 1; i >= 0; i-- {
		if x := a.overlays[i]; x == o && !x.Dismissed() {
			a.dismissAt(i)
			return
		}
	}
}

// focusFallback is the main window's focus of last resort when the last
// of its overlay layers that held the focus closes and what had it can't
// take it back (slackc.FocusFallback): the composer that had it last if
// it's on screen now (refocus, as a modal gives it back: a thread's,
// Activity's), else the current conversation's. One that went off the
// screen isn't asked: the request would wait for it, and take the focus
// when it came back (a permalink to its channel).
func (a *App) focusFallback(gtx layout.Context) {
	if t := refocus.Last(gtx.Now); t != nil && slackc.TakesFocus(gtx, t) {
		refocus.Restore(t)
		a.Invalidate()
		return
	}
	a.focusComposer()
}

// focusComposer focuses the current conversation's composer (the edit in
// progress first), as a navigation does.
func (a *App) focusComposer() {
	ch := a.router.Current().Channel()
	if ch == "" || a.data == nil || a.data.core == nil {
		return
	}
	c := a.convos[ch+"/"]
	switch {
	case c == nil:
	case c.edit != nil && !c.edit.Done():
		c.edit.Focus()
	case c.comp != nil:
		c.comp.Focus()
	}
}
