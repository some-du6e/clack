package ui

import (
	"reflect"

	"gioui.org/io/event"
	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/ui/slackc"
)

// The shell's overlays (menus, pickers, Slack's modals, the profile, Block
// Kit views) are layers in the main window's overlay stack (slackc
// layers.go), with the plugins' Popovers, dialogs and overlay roots. An
// Escape no widget took goes to the innermost layer holding the focus,
// else the topmost, and to the shell's "Close / mark as read" only with
// none open; a layer that closes gives the focus back to what had it.

// shellLayer is one shell overlay as a layer.
type shellLayer struct {
	a    *App
	o    Overlay
	area int
}

// LayerEscape (slackc.Layer) is an Escape that reached the overlay: it
// closes it as the shell's Escape binding closed the topmost one
// (dismissTop).
func (l *shellLayer) LayerEscape() { l.a.dismissOverlay(l.o) }

// invisible reports an overlay that draws nothing (the popups pump): no
// layer, and no Escape of its own.
func invisible(o Overlay) bool {
	inv, ok := o.(interface{ Invisible() bool })
	return ok && inv.Invisible()
}

// drawOverlay lays out overlay o as a layer: inside its area, so a focus
// in it is in the layer, with its focus home when it has one.
func (a *App) drawOverlay(gtx layout.Context, o Overlay) {
	if invisible(o) || !reflect.TypeOf(o).Comparable() {
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

// focusComposer focuses the current conversation's composer (the edit in
// progress first), as a navigation does: the main window's focus of last
// resort when its overlay layers close and what had the focus can't take
// it back (slackc.FocusFallback).
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
