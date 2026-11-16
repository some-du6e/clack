package slackc

import (
	"image"
	"sync"

	"gioui.org/io/event"
	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
)

// Overlay layers: what is drawn over a window's page that Escape closes
// and that gives the focus back when it goes, one stack per window, in
// the order the layers opened, the way the DOM stacks modals (each a
// portal appended to the body as it opens):
//
//   - the shell's overlays (menus, pickers, Slack's modals, the profile,
//     Block Kit views) and the huddle window's: DrawLayer around each;
//   - plugin overlay roots (api.modal, api.menu, a <dialog> showModal()
//     put in the top layer): LayoutOverlays, painter.fixed;
//   - kit's Popover and ConfirmationModal.
//
// The invisible ones (the popups pump) aren't layers.
//
// Escape goes the way a DOM keydown does:
//
//  1. to the focused element first. A plugin's input or focusable
//     element sends it to JS (dispatchKey), where its listeners can stop
//     it; a Clack widget that owns Escape takes it;
//  2. if nothing stopped it, to the innermost layer whose content holds
//     the focus, else to the topmost (RouteEscape, last in the frame). A
//     plugin layer's is JS's: the keydown's default action cancels the
//     topmost modal <dialog>;
//  3. the shell's Escape bindings ("Close / mark as read") only while no
//     layer is open (LayerOpen).
//
// The focus: a layer records what had it when it opened
// (document.activeElement). When layers close with the focus gone, it
// goes back there if that can still take it, else to the layer now on
// top, else to the window's own: the main window's channel composer
// (FocusFallback). With the focus gone while a layer stays (its focused
// element went), the topmost takes it back (react-modal's
// onFocusedElementRemoved).

// A Layer is one overlay in a window's stack.
type Layer interface {
	// LayerEscape is an Escape that bubbled up to the layer: it closes it
	// (react-modal's onRequestClose, the shell's Dismiss).
	LayerEscape()
}

// LayerSpec says how a layer holds the focus.
type LayerSpec struct {
	// Area is the tag of the layer's content area: the focus is in the
	// layer when the focused tag was drawn inside it. DrawLayer draws one
	// around a func; a layer whose content is deferred (op.Defer runs from
	// the root) draws its own inside the deferred macro.
	Area event.Tag
	// Home is where the focus comes back to in the layer (nil: nowhere).
	// It must take the focus: drawn with a key.FocusFilter read.
	Home event.Tag
	// JS marks a plugin overlay root or <dialog>: its Escape is the
	// keydown's in JS, so one that reaches it goes on to the document.
	JS bool
}

type layerEntry struct {
	l      Layer
	spec   LayerSpec
	opener event.Tag // the focus when it opened (its first live pass)
	live   bool      // opener read
	seen   bool      // laid out this frame
}

// layerStack is one window's layers, in the order they opened.
type layerStack struct {
	entries []*layerEntry
	// routed is the Source of the window's RouteEscape: the stack answers
	// for that window's router alone (a test's own router, or a window
	// that runs no RouteEscape, gets the overlays' old Escape)
	routed input.Source

	mu     sync.Mutex
	fromJS int // Escape keydowns JS passed on (no listener stopped them) since the last RouteEscape
	fromGo int // Escapes a Clack widget passed on (PassEscape): JS hasn't had them
}

// escAny is Escape with any modifiers: react-modal's handleKeyDown closes
// on keyCode 27 whatever else is held.
var escAny = key.Filter{Name: key.NameEscape, Optional: key.ModShift | key.ModCtrl | key.ModAlt | key.ModSuper | key.ModCommand}

// FocusFallback, set by the ui root, focuses the main window's channel
// composer: where the focus goes when the last layer closes and what had
// it before can't take it back.
var FocusFallback func()

// OpenLayer says l is open in the frame the current window lays out. Call
// it on every pass that lays l out, before l takes the focus or lays out
// its content: its first live pass records what has the focus.
func OpenLayer(gtx layout.Context, l Layer, spec LayerSpec) {
	ls := &curClock.layers
	var e *layerEntry
	for _, x := range ls.entries {
		if x.l == l {
			e = x
			break
		}
	}
	if e == nil {
		e = &layerEntry{l: l}
		ls.entries = append(ls.entries, e)
	}
	e.spec, e.seen = spec, true
	if !e.live && gtx.Source.Enabled() {
		e.live, e.opener = true, focusTag(gtx.Source)
	}
}

// DrawLayer lays out draw as the layer l (OpenLayer) inside its area: an
// unbounded pass-through clip area holding spec.Area's op (it nests what
// draw draws and clips none of its paint), so the focus is in l when it's
// in what draw draws (but not in its deferred ops).
func DrawLayer(gtx layout.Context, l Layer, spec LayerSpec, draw func(gtx layout.Context)) {
	OpenLayer(gtx, l, spec)
	const far = 1 << 16
	defer clip.Rect{Min: image.Pt(-far, -far), Max: image.Pt(far, far)}.Push(gtx.Ops).Pop()
	pass := pointer.PassOp{}.Push(gtx.Ops)
	event.Op(gtx.Ops, spec.Area)
	pass.Pop()
	draw(gtx)
}

// LayerOpen reports whether a layer was open in the last frame of the
// window gtx draws: Escape is the layers' then, and the shell's Escape
// bindings stand down.
func LayerOpen(gtx layout.Context) bool {
	ls := &curClock.layers
	return len(ls.entries) > 0 && gtx.Source.Enabled() && gtx.Source == ls.routed
}

// EscapeRouted reports whether RouteEscape runs in the window gtx draws.
// An overlay reads Escape only on its own tags then (its focused buttons)
// and leaves the rest to the stack: popups.Modal read it whatever had the
// focus, before a Popover in its content could. Without it (a test's own
// router) the overlays keep their own Escape.
func EscapeRouted(gtx layout.Context) bool {
	return gtx.Source.Enabled() && gtx.Source == curClock.layers.routed
}

// RouteEscape runs the Escapes the window's layers get this frame: the
// keydowns JS passed on (a plugin element's, no listener stopped it) and
// the one no widget took. Each goes to the innermost open layer holding
// the focus, else the topmost; a plugin layer's goes on to JS. Call it
// last in the window's frame: the ui root before the plugins' window key
// listeners (they don't hear an Escape a layer took, as react-modal's
// stopPropagation keeps it from the document); a SubWindow's frame runs
// it as it restores.
func RouteEscape(gtx layout.Context) {
	ls := &curClock.layers
	ls.routed = gtx.Source
	ls.mu.Lock()
	js, passed := ls.fromJS, ls.fromGo
	ls.fromJS, ls.fromGo = 0, 0
	ls.mu.Unlock()
	if !ls.open() {
		return
	}
	for ; js > 0; js-- {
		if to := ls.target(gtx); to != nil && !to.spec.JS { // a plugin layer's ran in JS (the keydown's default action)
			to.l.LayerEscape()
		}
	}
	for ; passed > 0; passed-- {
		switch to := ls.target(gtx); {
		case to == nil:
		case to.spec.JS:
			GlobalKey("keydown", key.Event{Name: key.NameEscape, State: key.Press})
		default:
			to.l.LayerEscape()
		}
	}
	for {
		e, ok := gtx.Event(escAny)
		if !ok {
			break
		}
		ke, ok := e.(key.Event)
		if !ok {
			continue
		}
		to := ls.target(gtx)
		switch {
		case to == nil:
		case to.spec.JS:
			typ := "keydown"
			if ke.State == key.Release {
				typ = "keyup"
			}
			GlobalKey(typ, ke) // at the document: the default action cancels the topmost modal dialog
		case ke.State == key.Press:
			to.l.LayerEscape()
		}
	}
}

// open reports whether a layer was laid out this frame.
func (ls *layerStack) open() bool {
	for _, e := range ls.entries {
		if e.seen {
			return true
		}
	}
	return false
}

// target is the layer an Escape goes to: the innermost (last opened)
// layer laid out this frame whose area holds the focus, else the topmost.
func (ls *layerStack) target(gtx layout.Context) *layerEntry {
	f := focusTag(gtx.Source)
	var top *layerEntry
	for i := len(ls.entries) - 1; i >= 0; i-- {
		e := ls.entries[i]
		if !e.seen {
			continue
		}
		if top == nil {
			top = e
		}
		if f != nil && e.spec.Area != nil && within(gtx.Source, f, e.spec.Area) {
			return e
		}
	}
	return top
}

// begin starts the window's frame for its layers (with the frame's
// context, before anything is laid out). The layers the last frame didn't
// lay out closed. With the focus nowhere (a focus in a closed layer went
// with its tags; one anywhere else was put there since, and stays) it
// comes back to what had it when the lowest of them opened, if that can
// still take it, else to the topmost layer's home, else, with no layer
// left, to the window's own (main). No layer closing, the topmost takes a
// lost focus back.
func (ls *layerStack) begin(gtx layout.Context, main bool) {
	var lowest *layerEntry
	keep := ls.entries[:0]
	for _, e := range ls.entries {
		switch {
		case e.seen:
			e.seen = false
			keep = append(keep, e)
		case lowest == nil:
			lowest = e
		}
	}
	clear(ls.entries[len(keep):])
	ls.entries = keep
	s := gtx.Source
	if !s.Enabled() || s != ls.routed || !s.Focused(nil) {
		return
	}
	if lowest != nil && lowest.opener != nil && canFocus(s, lowest.opener) {
		gtx.Execute(key.FocusCmd{Tag: lowest.opener})
		return
	}
	if n := len(ls.entries); n > 0 {
		if h := ls.entries[n-1].spec.Home; h != nil && canFocus(s, h) {
			gtx.Execute(key.FocusCmd{Tag: h})
		}
		return
	}
	if lowest != nil && main && FocusFallback != nil {
		FocusFallback()
	}
}

// PassEscape passes on to the window's layers an Escape a focused Clack
// widget read and had no use for (an editor with nothing to cancel), as a
// DOM keydown bubbles on past an element whose handler left it alone: a
// Popover's RichTextInput, a modal's field, closes its layer. Call it
// while the window's frame runs.
func PassEscape() { curClock.bubble(false) }

// bubble passes an Escape on to the window's layers, at its next
// RouteEscape, and asks for that frame: a keydown JS had and didn't stop
// (js: a plugin element's, dispatchKey) or one a Clack widget passed on.
// Any goroutine.
func (c *frameClock) bubble(js bool) {
	ls := &c.layers
	ls.mu.Lock()
	if js {
		ls.fromJS++
	} else {
		ls.fromGo++
	}
	ls.mu.Unlock()
	if f := c.wake; f != nil {
		f()
	} else {
		invalidate()
	}
}

// jsLayer is a plugin overlay root or a modal <dialog> as a window layer:
// JS closes it (LayerSpec.JS), so its own LayerEscape does nothing.
type jsLayer struct{ area int }

func (*jsLayer) LayerEscape() {}

// spec is the layer's: its area, JS's Escape.
func (l *jsLayer) spec() LayerSpec { return LayerSpec{Area: &l.area, JS: true} }
