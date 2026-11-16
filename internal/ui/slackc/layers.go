package slackc

import (
	"image"
	"sync"
	"time"

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
// The invisible ones (the popups pump) and hover peeks aren't layers. A
// layer that opens while the focus is in another was opened from inside
// it (a menu from Channel details, the colour popover from Preferences):
// it is that one's child, on top of it.
//
// Escape. The shell's overlays keep the shell's Escape: its binding, read
// first in the frame, closes the topmost of them (dismissTop), as it
// always has, and their own readers stay as they were. A plugin's layer
// changes that only where it is involved, the way the DOM bubbles a
// keydown up to react-modal:
//
//  1. with the focus in a plugin's layer the focused element gets the
//     key first (EscapeToFocus: the shell's bindings, and the overlays
//     that read Escape whatever has the focus, stand down): a plugin's
//     input sends it to JS, whose listeners can stop it (PressEscape: an
//     answer that doesn't come within escAnswerWait counts as unstopped),
//     a kit field or a Clack widget in it has it;
//  2. one nothing stopped goes on to the layer it was aimed at as it was
//     pressed: the innermost layer holding the focus, or the topmost of
//     the layers opened from inside it, else the topmost layer
//     (RouteEscape, last in the frame, for the rest); one aimed at a layer
//     that has closed since goes nowhere. A plugin layer's Escape is JS's
//     (the keydown's default action cancels the topmost modal dialog) or
//     its own (a kit Popover's onClose). While it is open it takes every
//     Escape aimed at it, as react-modal's handleKeyDown stops them all:
//     onClose once per asyncGrace (a second press before JS closed it
//     calls nothing), and nothing goes past it to the layers under it or
//     the shell;
//  3. one the shell's binding read goes to that layer when it's a
//     plugin's (EscapeToLayer), else on to the binding: dismissTop closes
//     the topmost shell overlay, and with none open the shell's Escape
//     does what it always does.
//
// The focus: a layer records what had it when it opened
// (document.activeElement), and what it was opened from. In the frame a
// layer that held the focus stops drawing, with the focus left on its
// tags or gone, the focus goes back to that if it can take it, else to
// the layer it opened from, else to the topmost that held it, else, with
// none left, to the main window's own (FocusFallback); with nowhere to go
// it leaves the closed layer (nothing focused: an editor in it would
// keep what is typed). A layer opened from one that closed gives the
// focus back where that one would have (a modal opened from a menu item,
// to what had the focus before the menu). With the focus dropped from a
// layer still open (its focused element went) the layer takes it back
// (react-modal's onFocusedElementRemoved).

// A Layer is one overlay in a window's stack.
type Layer interface {
	// LayerEscape is an Escape that reached the layer: it closes it
	// (react-modal's onRequestClose, the shell's Dismiss).
	LayerEscape()
}

// LayerSpec says how a layer holds the focus and takes Escape.
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
	// keydown's in JS (its LayerEscape isn't called). It is a plugin's
	// layer.
	JS bool
	// Plugin marks a plugin's layer Go draws (kit's Popover and
	// ConfirmationModal): Escape is routed to it as the DOM would.
	Plugin bool
	// Async marks a layer whose LayerEscape asks JS to close it (a kit
	// Popover's onClose): until it stops drawing, or asyncGrace passes,
	// later Escapes aimed at it are its, and call nothing.
	Async bool
}

const (
	// escAnswerWait is how long an Escape keydown waits for JS to say
	// whether a listener stopped it (a stuck plugin task, a runtime
	// starting or gone): then it goes on as unstopped.
	escAnswerWait = 300 * time.Millisecond
	// asyncGrace is how long an Async layer an Escape reached counts as
	// closing: the Escapes it takes meanwhile call nothing; one after that
	// calls onClose again (a plugin that kept it open hears it once more).
	asyncGrace = 500 * time.Millisecond
)

type layerEntry struct {
	l      Layer
	spec   LayerSpec
	opener event.Tag   // the focus at its first live pass (document.activeElement when it opened)
	parent *layerEntry // the layer that held the opener then: it was opened from inside it
	live   bool        // opener read
	seen   bool        // laid out this frame
	last   int         // the stack's frame it was last laid out in
	held   bool        // the focus was in it
	escAt  time.Time   // an Escape reached it then (zero: none since it settled)
	escF   int         // in that frame
	gone   bool        // closed: out of the stack
}

func (e *layerEntry) plugin() bool { return e.spec.Plugin || e.spec.JS }

// holds reports whether focused tag f is in the layer (last frame's
// areas).
func (e *layerEntry) holds(s input.Source, f event.Tag) bool {
	return f != nil && (f == e.spec.Home || e.spec.Area != nil && within(s, f, e.spec.Area))
}

// escaped reports whether an Escape that reached the layer is still
// closing it. A sync one (closed at once) is passed over by later ones,
// as it goes; an Async one takes them, and calls nothing. A shell overlay
// that stays after one (a CloseGuard kept it, a Block Kit view backed
// out) settles once it is drawn after it (begin).
func (e *layerEntry) escaped(now time.Time) bool {
	if e.escAt.IsZero() {
		return false
	}
	return !e.spec.Async || now.Sub(e.escAt) < asyncGrace
}

// giveBack is where the layer gives the focus back to and what it opened
// from, past the layers it opened from that are closed (their focus went
// with them: theirs instead).
func (e *layerEntry) giveBack() (event.Tag, *layerEntry) {
	o, p := e.opener, e.parent
	for p != nil && p.gone {
		o, p = p.opener, p.parent
	}
	return o, p
}

// layerStack is one window's layers, in the order they opened.
type layerStack struct {
	entries []*layerEntry
	// routed is the Source of the window's RouteEscape: the stack answers
	// for that window's router alone (a test's own router, or a window
	// that runs no RouteEscape, gets the overlays' old Escape)
	routed input.Source
	src    input.Source // this frame's (begin)
	now    time.Time    // this frame's
	frame  int          // frames begun
	ended  bool         // the frame ran RouteEscape
	// pluginFocus: at the frame's start the focus was in a plugin's
	// layer, which gets this frame's Escape first (EscapeToFocus)
	pluginFocus bool
	focusIn     *layerEntry // the innermost layer holding the focus at the last frame's end
	lastFocus   event.Tag   // the focus then
	gaveTo      event.Tag   // where a closing layer gave the focus back this frame

	mu     sync.Mutex
	passed []*EscapePress // Escapes passed on since the last RouteEscape (any goroutine)
}

// escAny is Escape with any modifiers: react-modal's handleKeyDown closes
// on keyCode 27 whatever else is held.
var escAny = key.Filter{Name: key.NameEscape, Optional: key.ModShift | key.ModCtrl | key.ModAlt | key.ModSuper | key.ModCommand}

var (
	// FocusFallback, set by the ui root, gives the main window's focus to
	// its own (the last composer that had it, else the channel's): where
	// it goes when the last layer that held it closes and what had it
	// before can't take it back. Called at the end of the frame.
	FocusFallback func(gtx layout.Context)
	// FocusInert, set by the ui root, reports a window whose page takes no
	// input (a replayed introduction): closing layers give it no focus.
	FocusInert func() bool
	// ShellEscape, set by the ui root, runs the main window's own Escape
	// binding for ke (Close / mark as read): an Escape its shell left to
	// a plugin's layer that had closed by the end of the frame, or one
	// that went past the layers it was aimed at (they closed with an
	// earlier one).
	ShellEscape func(gtx layout.Context, ke key.Event)
)

func (ls *layerStack) main() bool { return ls == &mainClock.layers }

func (ls *layerStack) inert() bool { return ls.main() && FocusInert != nil && FocusInert() }

func (ls *layerStack) find(l Layer) *layerEntry {
	for _, e := range ls.entries {
		if e.l == l {
			return e
		}
	}
	return nil
}

// holder is the innermost layer (the last opened) holding focused tag f,
// other than not.
func (ls *layerStack) holder(s input.Source, f event.Tag, not *layerEntry) *layerEntry {
	if f == nil {
		return nil
	}
	for i := len(ls.entries) - 1; i >= 0; i-- {
		if e := ls.entries[i]; e != not && e.holds(s, f) {
			return e
		}
	}
	return nil
}

// holding is the innermost layer holding focused tag f: drawn inside it in
// the last frame or, for the tag that had the focus at the last frame's
// end, the layer that held it then (a hold, composer.KeepFocus, draws an
// editor that missed a frame at the window's root).
func (ls *layerStack) holding(s input.Source, f event.Tag, not *layerEntry) *layerEntry {
	if h := ls.holder(s, f, not); h != nil {
		return h
	}
	if in := ls.focusIn; f != nil && f == ls.lastFocus && in != nil && in != not && !in.gone {
		return in
	}
	return nil
}

// passing reports whether a later Escape goes past the layer: a sync one
// an Escape closed (it goes this frame). An Async one still open takes it.
func (e *layerEntry) passing(now time.Time) bool { return !e.spec.Async && e.escaped(now) }

// target is where an Escape with the focus on f goes on to: the innermost
// layer holding f, or the topmost layer opened from inside it (or from
// those), else the topmost layer. Sync layers an Escape closed are passed
// over, to what they opened from (past reports one was): past the last of
// those, to none (what had the focus before them, outside every layer,
// gets it: the shell's).
func (ls *layerStack) target(s input.Source, f event.Tag, now time.Time) (to *layerEntry, past bool) {
	if h := ls.holding(s, f, nil); h != nil {
		for h != nil && (h.gone || h.passing(now)) {
			h, past = h.parent, true
		}
		if h == nil {
			return nil, past
		}
		for i := len(ls.entries) - 1; i >= 0; i-- {
			e := ls.entries[i]
			if e.passing(now) {
				continue
			}
			for a := e; a != nil; a = a.parent {
				if a == h {
					return e, past
				}
			}
		}
		return h, past
	}
	// the focus in none: the topmost (one closing doesn't make the next
	// one's Escape a plugin's)
	for i := len(ls.entries) - 1; i >= 0; i-- {
		if e := ls.entries[i]; !e.passing(now) {
			return e, false
		}
	}
	return nil, false
}

// involved reports whether a plugin's layer is in an Escape aimed at to
// from focused f: the layer holding f or to is a plugin's. Elsewhere the
// shell's and its overlays' own Escape stays.
func (ls *layerStack) involved(s input.Source, f event.Tag, to *layerEntry) bool {
	if to != nil && to.plugin() {
		return true
	}
	h := ls.holding(s, f, nil)
	return h != nil && h.plugin()
}

// escape gives to the Escape: its LayerEscape (a plugin overlay root's is
// JS's), and it is closing. An Async layer an earlier Escape is still
// closing takes this one too, and calls nothing (react-modal stops every
// Escape; onClose once per round trip).
func (ls *layerStack) escape(to *layerEntry, now time.Time) {
	if to.spec.Async && to.escaped(now) {
		return
	}
	to.escAt, to.escF = now, ls.frame
	if !to.spec.JS {
		to.l.LayerEscape()
	}
}

// OpenLayer says l is open in the frame the current window lays out. Call
// it on every pass that lays l out, before l takes the focus or lays out
// its content: its first live pass records what has the focus and the
// layer holding that (what it was opened from).
func OpenLayer(gtx layout.Context, l Layer, spec LayerSpec) {
	ls := &curClock.layers
	e := ls.find(l)
	if e == nil {
		e = &layerEntry{l: l}
		ls.entries = append(ls.entries, e)
	}
	e.spec, e.seen, e.last = spec, true, ls.frame
	if !e.live && gtx.Source.Enabled() {
		e.live = true
		e.opener = focusTag(gtx.Source)
		e.parent = ls.holding(gtx.Source, e.opener, e)
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

// EscapeRouted reports whether the window gtx draws routes Escape through
// its layers (RouteEscape runs there). A plugin layer that reads Escape on
// its own tags leaves it to the stack there, so it is closed once; a test
// laying a widget out with its own router keeps the old reads.
func EscapeRouted(gtx layout.Context) bool {
	return gtx.Source.Enabled() && gtx.Source == curClock.layers.routed
}

// EscapeToFocus reports whether the focus is in a plugin's layer of the
// window gtx draws, at the start of this frame: its focused element gets
// the frame's Escape first (a plugin's input sends it to JS, whose
// listeners can stop it), so the shell's Escape bindings stand down, and
// an overlay that reads Escape whatever has the focus (a modal under the
// layer, Channel details) leaves it alone. What nothing stopped goes on
// to the layers at the end of the frame (RouteEscape).
func EscapeToFocus(gtx layout.Context) bool {
	ls := &curClock.layers
	return ls.pluginFocus && gtx.Source.Enabled() && gtx.Source == ls.routed
}

// FocusTarget is the tag the window's focus is on, or goes to as the frame
// ends: one a closing layer gave it back to this frame (Gio runs that
// FocusCmd at the frame's end when the tag has read its focus events
// already). The shell's Escape (ShellEscape) leaves alone an Escape that
// tag owns.
func FocusTarget(gtx layout.Context) event.Tag {
	ls := &curClock.layers
	if ls.gaveTo != nil && gtx.Source == ls.routed {
		return ls.gaveTo
	}
	return focusTag(gtx.Source)
}

// FocusInPluginLayer reports whether the focus is in a plugin's layer of
// the window gtx draws, at the start of this frame (EscapeToFocus): a
// modal that opens then leaves giving it back to the stack.
func FocusInPluginLayer(gtx layout.Context) bool { return EscapeToFocus(gtx) }

// TakesFocus reports whether tag, drawn in the frame in progress, asked
// for its focus events (would keep a key.FocusCmd past the frame): at the
// end of a frame, a widget on screen now.
func TakesFocus(gtx layout.Context, tag event.Tag) bool { return askedFocus(gtx.Source, tag) }

// EscapeToLayer takes an Escape press the shell's binding read where a
// plugin's layer is involved: the layer it goes on to (target) is a
// plugin's. It reports whether it took it; when it didn't, the binding
// goes on as always (dismissTop closes the topmost shell overlay).
// forwarded says the keydown went to JS's document already (the shell
// forwards its keys to plugins' window listeners): a plugin overlay
// root's Escape is that keydown's default action.
func EscapeToLayer(gtx layout.Context, ke key.Event, forwarded bool) bool {
	ls := &curClock.layers
	s := gtx.Source
	if ke.Name != key.NameEscape || ke.State != key.Press || !s.Enabled() || s != ls.routed {
		return false
	}
	to, _ := ls.target(s, focusTag(s), gtx.Now)
	if to == nil || !to.plugin() {
		return false
	}
	if to.spec.JS && !forwarded {
		GlobalKey("keydown", ke) // at the document: the default action cancels the topmost modal dialog
	}
	ls.escape(to, gtx.Now)
	return true
}

// An EscapePress is an Escape keydown a focused element has (a plugin's,
// on its way through JS; a Clack widget's that had no use for it), bound
// to where it goes on to unless something stops it: the layer it was
// aimed at as it was pressed, which the DOM's synchronous keydown would
// have reached then. Nil (nothing to go on to) is valid: its methods do
// nothing.
type EscapePress struct {
	ls    *layerStack
	c     *frameClock
	to    *layerEntry
	js    bool // JS had it: a plugin overlay root's own Escape ran there
	timer *time.Timer

	mu   sync.Mutex
	need int  // answers still to come
	done bool // answered (or timed out)
}

// pressEscape binds an Escape keydown at focused element f in the current
// window (its frame in progress) to its layer, if a plugin's is involved.
func pressEscape(ke key.Event, f event.Tag, js bool) *EscapePress {
	c := curClock
	ls := &c.layers
	s := ls.src
	if !s.Enabled() || s != ls.routed {
		return nil
	}
	to, _ := ls.target(s, f, ls.now)
	if to == nil || !ls.involved(s, f, to) {
		return nil
	}
	return &EscapePress{ls: ls, c: c, to: to, js: js, need: 1}
}

// PassEscape passes on to the window's layers an Escape a focused Clack
// widget read and had no use for (an editor with nothing to cancel), as a
// DOM keydown bubbles on past an element whose handler left it alone: a
// RichTextInput closes the Popover it's in. Only where a plugin's layer
// is involved: elsewhere the widget's Escape stays its own, as always.
// Call it while the window's frame runs.
func PassEscape(gtx layout.Context) {
	if p := pressEscape(key.Event{Name: key.NameEscape, State: key.Press}, focusTag(gtx.Source), false); p != nil {
		p.Answer(false)
	}
}

// PressEscape begins an Escape keydown a focused plugin element (kit's
// FormTextInput) sends to JS, answered by n listeners (its onKeyDown
// prop, the DOM keydown at its node): each Answers whether it stopped
// it. It goes on once all said they didn't, and with no answers within
// escAnswerWait, as unstopped.
func PressEscape(gtx layout.Context, ke key.Event, n int) *EscapePress {
	p := pressEscape(ke, focusTag(gtx.Source), true)
	if p != nil {
		p.need = n
	}
	p.wait()
	return p
}

// wait starts the answers' deadline.
func (p *EscapePress) wait() {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.timer = time.AfterFunc(escAnswerWait, func() { p.finish(false) })
	p.mu.Unlock()
}

// Answer says whether a listener stopped the Escape (its propagation):
// once every one said it didn't, it goes on to its layer at the window's
// next RouteEscape, which it asks for; one that did keeps it. Later
// answers count for nothing. Any goroutine.
func (p *EscapePress) Answer(stopped bool) {
	if p == nil {
		return
	}
	p.mu.Lock()
	if !stopped && p.need > 1 {
		p.need--
		p.mu.Unlock()
		return
	}
	p.mu.Unlock()
	p.finish(stopped)
}

// finish ends the press once: stopped, or on to its layer.
func (p *EscapePress) finish(stopped bool) {
	p.mu.Lock()
	done, t := p.done, p.timer
	p.done = true
	p.mu.Unlock()
	if done {
		return
	}
	if t != nil {
		t.Stop()
	}
	if stopped {
		return
	}
	ls := p.ls
	ls.mu.Lock()
	ls.passed = append(ls.passed, p)
	ls.mu.Unlock()
	p.c.wakeAfter()
}

// RouteEscape ends the window's frame for its layers. Call it last in the
// frame (the ui root before the plugins' window key listeners, which
// don't hear an Escape a layer took, as react-modal's stopPropagation
// keeps it from the document; a SubWindow's frame runs it as it
// restores):
//
//   - the layers this frame didn't lay out closed: the focus they held
//     goes back (see the top of the file);
//   - the Escapes passed on since the last frame (a plugin element's that
//     no listener stopped, a Clack widget's) go to the layers they were
//     aimed at;
//   - with a plugin's layer involved, an Escape no widget took goes to
//     the layer holding the focus (or what opened from it on top), else
//     the topmost; with none of its layers left, to the shell's binding
//     that stood down for it.
func RouteEscape(gtx layout.Context) {
	ls := &curClock.layers
	ls.routed = gtx.Source
	ls.ended = true
	s := gtx.Source
	if !s.Enabled() {
		return
	}
	ls.closeUnseen(gtx, true)
	ls.mu.Lock()
	passed := ls.passed
	ls.passed = nil
	ls.mu.Unlock()
	for _, p := range passed {
		ls.goOn(gtx, p)
	}
	ls.unclaimed(gtx)
	// where the focus is as the frame ends; a tag still focused that a
	// hold drew outside its layer is still that layer's
	f := focusTag(s)
	if h := ls.holder(s, f, nil); h != nil || f != ls.lastFocus {
		ls.focusIn = h
	}
	ls.lastFocus = f
}

// goOn gives a passed Escape to the layer it was aimed at as it was
// pressed. One that closed since (the Escape before it, a click) takes it
// with it: in Slack the keydown reached it while it was open, and went no
// further (react-modal stops it). One still open takes it (escape: an
// Async one closing calls nothing).
func (ls *layerStack) goOn(gtx layout.Context, p *EscapePress) {
	switch to := p.to; {
	case to == nil || to.gone:
	case to.spec.JS && p.js: // its default action ran in JS
	case to.spec.JS:
		GlobalKey("keydown", key.Event{Name: key.NameEscape, State: key.Press}) // a Clack widget's: at the document, where the default action cancels the topmost modal dialog
		ls.escape(to, gtx.Now)
	default:
		ls.escape(to, gtx.Now)
	}
}

// unclaimed takes the Escapes no widget took, where a plugin's layer is
// involved (else they stay for the window's key listeners, as always).
func (ls *layerStack) unclaimed(gtx layout.Context) {
	s := gtx.Source
	f := focusTag(s)
	to, past := ls.target(s, f, gtx.Now)
	if !ls.pluginFocus && !ls.involved(s, f, to) {
		return
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
		switch {
		case to == nil:
			// the shell's binding stood down for a plugin's layer that
			// closed by now (or a sync one an earlier Escape closed)
			if ke.State == key.Press && (ls.pluginFocus || past) && ls.main() && ShellEscape != nil {
				ShellEscape(gtx, ke)
			}
		case to.spec.JS:
			typ := "keydown"
			if ke.State == key.Release {
				typ = "keyup"
			}
			GlobalKey(typ, ke) // at the document: the default action cancels the topmost modal dialog
		case ke.State == key.Press:
			ls.escape(to, gtx.Now)
			to, past = ls.target(s, f, gtx.Now) // a second one in the frame: past a sync layer it closed, an Async one's again
		}
	}
}

// begin starts the window's frame for its layers (with the frame's
// context, before anything is laid out).
func (ls *layerStack) begin(gtx layout.Context) {
	ls.frame++
	ls.src, ls.now, ls.gaveTo = gtx.Source, gtx.Now, nil
	if !ls.ended {
		// the last frame ran no RouteEscape: what it didn't lay out
		// closed then
		ls.closeUnseen(gtx, false)
	}
	ls.ended = false
	for _, e := range ls.entries {
		e.seen = false
		if !e.escAt.IsZero() && (!e.spec.Async && e.last > e.escF || e.spec.Async && !e.escaped(gtx.Now)) {
			e.escAt = time.Time{} // it stayed open after its Escape: the next one is its own again
		}
	}
	ls.pluginFocus = false
	s := gtx.Source
	if !s.Enabled() || s != ls.routed {
		return
	}
	f := focusTag(s)
	if f == nil {
		// the focus dropped from a layer still open (its focused element
		// went): it takes it back
		if in := ls.focusIn; in != nil && !in.gone && !ls.inert() {
			for x := in; x != nil; x = x.parent {
				if h := x.spec.Home; !x.gone && h != nil && canFocus(s, h) {
					gtx.Execute(key.FocusCmd{Tag: h})
					break
				}
			}
		}
		return
	}
	h := ls.holding(s, f, nil)
	for _, e := range ls.entries {
		if !e.held && (e == h || e.holds(s, f)) {
			e.held = true
		}
	}
	ls.pluginFocus = h != nil && h.plugin()
}

// closeUnseen drops the layers this frame (atEnd: RouteEscape) or the
// last one (begin) didn't lay out, and gives back the focus the lowest of
// them that held it had.
func (ls *layerStack) closeUnseen(gtx layout.Context, atEnd bool) {
	var closed []*layerEntry
	keep := ls.entries[:0]
	for _, e := range ls.entries {
		if e.seen {
			keep = append(keep, e)
			continue
		}
		e.gone = true
		closed = append(closed, e)
	}
	clear(ls.entries[len(keep):])
	ls.entries = keep
	if len(closed) == 0 {
		return
	}
	// what was opened from them gives the focus back where they would have
	for _, e := range ls.entries {
		if e.parent != nil && e.parent.gone {
			e.opener, e.parent = e.giveBack()
		}
	}
	s := gtx.Source
	if !s.Enabled() || s != ls.routed {
		return
	}
	// a widget drawn now asked for its focus events; at the start of a
	// frame, none has yet: the last frame's
	can := func(t event.Tag) bool { return t != nil && (atEnd && askedFocus(s, t) || !atEnd && canFocus(s, t)) }
	f := focusTag(s)
	var from *layerEntry // the lowest closed layer whose focus is to come back
	for _, x := range closed {
		// the focus left on one of its tags that went with it (a tag that
		// is still drawn is somewhere else now), or gone
		in := x.holds(s, f) || f != nil && x == ls.focusIn && f == ls.lastFocus
		if f != nil && in && !(atEnd && askedFocus(s, f)) || f == nil && x.held {
			from = x
			break
		}
	}
	if from == nil {
		return
	}
	if ls.inert() {
		// the page takes no input (a replay moved the focus off it): the
		// focus goes from the closed layer, and nowhere else
		if f != nil {
			gtx.Execute(key.FocusCmd{})
		}
		return
	}
	focus := func(t event.Tag) {
		gtx.Execute(key.FocusCmd{Tag: t})
		ls.gaveTo = t
		curClock.wakeAfter()
	}
	o, p := from.giveBack()
	if can(o) {
		focus(o)
		return
	}
	for x := p; x != nil; x = x.parent {
		if h := x.spec.Home; !x.gone && can(h) {
			focus(h)
			return
		}
	}
	for i := len(ls.entries) - 1; i >= 0; i-- {
		if e := ls.entries[i]; e.held && can(e.spec.Home) {
			focus(e.spec.Home)
			return
		}
	}
	if len(ls.entries) == 0 && ls.main() && FocusFallback != nil {
		FocusFallback(gtx)
	}
	if f != nil && focusTag(s) == f {
		gtx.Execute(key.FocusCmd{}) // nowhere to go: off the closed layer, nothing focused
	}
}

// wakeAfter asks for the window's next frame (from any goroutine): after
// a focus given back (one given to a tag that went in this frame after
// all is dropped at its end, without an event, and the next frame's begin
// gives it to a layer's home), or an Escape passed on.
func (c *frameClock) wakeAfter() {
	if c.wake != nil {
		c.wake()
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
