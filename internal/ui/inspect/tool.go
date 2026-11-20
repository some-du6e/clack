// Package inspect is Clack's inspect tool: a developer shortcut, not a
// Slack feature, that makes a UI bug report one paste. The shortcut
// (off until the Konami code, CLACK_INSPECT=1 or the hidden ui.json pref
// inspect_tool turns it on) starts an inspect session, like a browser's
// inspect element: the Slack component under the pointer gets an outline
// and a label, ↑ and ↓ pick its parent or go back, Esc ends the session,
// and a click shows the element's shot for review first (review.go):
// rects dragged over it are blurred out of the image (Redact), Enter
// copies it and Esc drops it. A drag past dragSlop (which grabs the
// region dragged out instead, for what the picker can't reach) is
// reviewed the same way, and a Shift+click copies the report at once,
// with no review (Karim, 2026-11-17: "just flip it around", so every
// plain pick can be blurred). The copy is one image on the clipboard: the element as it was drawn, outlined,
// with a small blurred margin and nothing else of the window (or the
// region, as it was), over a panel with a report (which component and
// element, its classes, its rect, the colours drawn there, the theme,
// the experiments its code read, and tags for the ids it was drawn for:
// one-way hashes, salted per report, so the report is safe to paste in
// #clack). The image and the report's text are also saved in
// <config>/inspect-reports.
//
// While a session runs the window is laid out with a hover-only input
// source (Gio patch 21): hover states, scrolling, carets and animations
// stay, and no button, editor or shortcut reacts; the tool alone reads
// the clicks and keys. The freeze key (FreezeKey, freeze.go) holds the
// window on the frame on screen, for what only shows while the pointer is
// on it (a message's hover toolbar, a tooltip, a button's hover colour):
// the window draws that frame's ops again and lays nothing out, and the
// picker and the captures work on it. Where things are comes from slackc's inspect
// records (slackc/inspect.go): each component boundary gets a
// pass-through pointer area, and Gio's own hit test says which are under
// the pointer. Plugins' host trees go down to their boxes, and the
// chrome's elements (the channel header's title and topic, the tab bar's
// tabs, the tab rail's buttons; slackc ClassDef.Inspect) have areas of
// their own, as a browser's inspector reaches any element. Elsewhere
// (plain Gio widgets) the report falls back to the shell area.
package inspect

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/uiguard"
)

// Region is a named area of the window (px): the shell's columns, where
// the report falls back to when no component is under the pointer.
type Region struct {
	Name string
	Rect image.Rectangle
}

// Host is what the tool needs from the window (internal/ui).
type Host interface {
	// Theme draws the session's label and banner.
	Theme() *theme.Theme
	// Describer reads the window and the page as they are now, and
	// returns what fills a report's window and page fields with them
	// (id-bearing ones through red) later: a frozen capture's report
	// describes the frozen frame (UI goroutine).
	Describer() func(r *Report, red *Redactor)
	// ConvKind is "public", "private", "dm", "mpdm" or "" for a channel
	// id: what kind of conversation, never which.
	ConvKind(id string) string
	// Regions are the shell's areas this frame, outermost first.
	Regions() []Region
	// Toast shows a short note at the bottom of the window.
	Toast(text string)
	// Post runs f on the UI goroutine; Invalidate asks for a frame.
	Post(f func())
	Invalidate()
	// ConfigDir is where reports are saved ("": nowhere).
	ConfigDir() string
	// Frozen says a freeze began (true: the window draws the frame on
	// screen again, frame after frame, and holds its animated images) or
	// ended (false: at its next frame, before anything reads input, the
	// window starts every widget's pointer state over, Gio patch 49's
	// Source.ResetPointers, and lays out again).
	Frozen(on bool)
}

type state uint8

const (
	off state = iota
	inspecting
	// pressing: the primary button went down over an element, whose
	// report was built from the frame it was pressed in (pend). A release
	// before the pointer moves dragSlop picks it; past that it's a drag.
	pressing
	// dragging: a region is dragged out, from t.from to the pointer.
	dragging
	// picked: the release's frame (laid out hover-only, so no widget saw
	// the button) is shot in AfterFrame, which copies the report (a
	// Shift+click) or starts the review (a plain click, a region).
	picked
	// reviewing: the shot is shown for blurring (review.go).
	reviewing
)

// dragSlop is how far (dp) the pointer moves pressed before a click
// becomes a drag that grabs a region; minRegion is the smallest region
// (dp each way) a drag grabs.
const (
	dragSlop  = 6
	minRegion = 8
)

// Tool is one window's inspect tool. Everything runs on the UI goroutine.
type Tool struct {
	host  Host
	state state
	combo key.Filter // the shortcut, which also ends a session

	frames [2]slackc.InspectFrame
	cur    int // frames[cur] records this frame; the other one is the last
	rec    bool

	ptag  int // the window-wide pointer area (topmost, pass-through)
	ptr   f32.Point
	ptrIn bool
	hist  [32]ptrAt
	histN int
	from  f32.Point   // where the primary button went down (window px)
	win   image.Point // the window's size (px), at the last input read
	scale float32     // px per dp, at the last input read

	hover    *pick
	up       int           // ↑ presses: how many components out from the innermost
	pend     *capture      // pressing: the element's report, waiting for the release
	capture  *capture      // picked: waiting for the frame to be shot
	rev      *review       // reviewing
	lastDone chan struct{} // the last report's delivery (Wait)
	lastRep  *Report       // the last report delivered (tests, after Wait)
	texts    textCopies    // text copies on their way to the clipboard (textcopy.go)
	// observing: this tool's session observes experiments (observe)
	observing bool
	// held is a copy key (Return and Enter as one, or C) dropped because a
	// guarded capture's review wasn't armed, until its release is read:
	// its auto-repeats (presses with no release between them on Windows
	// and macOS) copy nothing; heldAt is its last press
	held   key.Name
	heldAt time.Time
	upNow  []key.Name // copy keys released in this frame (pickKeys holds no key already up)
	inAt   time.Time  // the frame whose input was read (Frame, Begin)
	recAt  time.Time  // the frame recorded into frames[cur]
	upKey  string     // the innermost element t.up counts from (innerKey)
	// the last press of FreezeKey and of Esc while it's down (repeat)
	freezeDown, escDown time.Time
	frameN              int // frames whose input was read (a capture's stage times count them)
	// afterPick are the frame's keys held for after its release (readKeys)
	afterPick []key.Event

	// content is what the last laid-out frame drew, the tool's own
	// drawing left out (End), and contentOK that it is the frame on
	// screen: no frame reset its ops since. A freeze draws it again.
	content    op.CallOp
	contentOK  bool
	contentAt  frameKey
	contentOps *op.Ops       // the window's ops End finished
	contentCP  op.Checkpoint // their end after content
	fz         *freeze       // the frozen frame (freeze.go), nil while none is
}

// frameKey is what a frame was laid out for: its size and scale.
type frameKey struct {
	size   image.Point
	metric unit.Metric
}

type ptrAt struct {
	t time.Duration
	p f32.Point
}

// pick is what the pointer is over: a node of the last frame's record.
type pick struct {
	frame  *slackc.InspectFrame
	node   int // the component (or tree) outlined
	inner  int // the innermost component hit (↑ walks out from it)
	tree   int // a plugin tree under the pointer, -1
	local  f32.Point
	origin image.Point
	rect   image.Rectangle // window px
	region string
	label  string
}

// New makes the tool for host. combo is its shortcut as a key filter (the
// session reads it to end itself; the window binds it to Toggle).
func New(host Host, combo key.Filter) *Tool {
	return &Tool{host: host, combo: combo}
}

// Active reports a running session (a pick's frame and its review
// included).
func (t *Tool) Active() bool { return t.state != off }

// Reviewing reports that a shot is shown for blurring (review.go).
func (t *Tool) Reviewing() bool { return t.state == reviewing }

// Dragging is the region being dragged out (window px), ok false when
// none is.
func (t *Tool) Dragging() (image.Rectangle, bool) {
	if t.state != dragging {
		return image.Rectangle{}, false
	}
	return t.dragRect(t.ptr), true
}

// Hovered is what the session outlines now: its label and rect (window
// px); ok false when nothing is.
func (t *Tool) Hovered() (label string, rect image.Rectangle, ok bool) {
	if t.state != inspecting || t.hover == nil {
		return "", image.Rectangle{}, false
	}
	return t.hover.label, t.hover.rect, true
}

// Close ends a session, a pick waiting for its frame included, before
// the window drops the tool.
func (t *Tool) Close() {
	// every capture logs its line, whatever ends it (sign-out, the tool
	// turned off, hide to tray)
	if r := t.rev; r != nil && r.c != nil {
		r.c.tm.outcome = "dropped: the tool closed"
		logTimes(r.c)
	}
	t.dropCapture("dropped: the tool closed")
	if t.state != off {
		t.stop()
	}
	t.capture, t.pend, t.rev = nil, nil, nil
	t.host.Invalidate()
}

// Toggle starts a session, or ends the one running.
func (t *Tool) Toggle() {
	if t.state == off {
		t.start()
	} else {
		t.stop()
	}
	t.host.Invalidate()
}

func (t *Tool) start() {
	t.state = inspecting
	t.hover, t.up, t.rec, t.pend, t.capture, t.rev = nil, 0, false, nil, nil, nil
	t.frames[0].Nodes, t.frames[1].Nodes = t.frames[0].Nodes[:0], t.frames[1].Nodes[:0]
	t.observe(true)
	if ownShoot() {
		shots.wantFor(t, true) // the shots' GPU, ready for the first pick (shooter.go)
	}
}

func (t *Tool) stop() {
	t.dropCapture("dropped: the session ended before its shot")
	t.state = off
	t.held = ""
	t.pend, t.rev = nil, nil
	t.contentOK = false
	t.unfreeze()
	t.release()
	if ownShoot() {
		shots.wantFor(t, false)
	}
}

// dropCapture lets go of a pick not shot yet, its stage times logged with
// outcome (unless it has one): no later frame's AfterFrame shoots it and
// opens a review after its session ended.
func (t *Tool) dropCapture(outcome string) {
	c := t.capture
	if c == nil {
		return
	}
	t.capture = nil
	if c.tm.outcome == "" {
		c.tm.outcome = outcome
	}
	logTimes(c)
}

// release stops recording frames: no outline, no experiments observed,
// and the last frames' nodes (widgets, props funcs, scopes) let go of.
func (t *Tool) release() {
	t.hover, t.rec = nil, false
	t.observe(false)
	for i := range t.frames {
		t.frames[i].Release()
	}
}

// backToInspecting ends a press or a drag that grabbed nothing: the
// session goes on, recording afresh (or, frozen, on the frozen frame).
func (t *Tool) backToInspecting() {
	if t.fz != nil {
		// the frozen frame's record stayed (release0): the next frame's
		// hits outline it again
		t.state, t.pend, t.hover = inspecting, nil, nil
		return
	}
	// no outline until a frame is recorded again: the one shown was the
	// press's, whose nodes are let go of
	t.state, t.pend, t.rec, t.hover = inspecting, nil, false, nil
	t.observe(true)
}

// observers counts the tools whose sessions observe experiments: each of
// Clack's windows runs a tool of its own (the main window's, huddleui's
// windows'), and the observer stays while any of their sessions wants it.
var observers struct {
	mu sync.Mutex
	n  int
}

// observe says t's session observes experiments (on) or no longer does.
func (t *Tool) observe(on bool) {
	if t.observing == on {
		return
	}
	t.observing = on
	observers.mu.Lock()
	defer observers.mu.Unlock()
	if on {
		if observers.n++; observers.n == 1 {
			core.ObserveExperiments(observeExperiment)
		}
	} else if observers.n--; observers.n == 0 {
		core.ObserveExperiments(nil)
	}
}

// recorder is the goroutine whose window's frame is being recorded (Begin
// to End, under slackc's frame lock), 0 between: each window runs its
// frames on its own goroutine, and only that one's experiment reads are
// the frame's.
var recorder atomic.Int64

// observeExperiment gives an experiment read in the frame being recorded,
// on the goroutine recording it, to the node drawing.
func observeExperiment(name, group string) {
	if g := recorder.Load(); g == 0 || g != uiguard.Goroutine() {
		return
	}
	if f := slackc.CurrentInspectFrame(); f != nil {
		f.NoteExperiment(name, group)
	}
}

const pointerKinds = pointer.Move | pointer.Enter | pointer.Leave | pointer.Press | pointer.Release | pointer.Drag | pointer.Cancel

// Begin starts a frame. Call it first in the window's frame, before
// anything reads input (after Frame, which reads the frame's input when
// a session runs); lay the window out with the context it returns (gtx
// itself while no session runs) and call End last.
func (t *Tool) Begin(gtx layout.Context) layout.Context {
	t.flushTexts(gtx) // a session or not: a delivery's text copy
	if t.state == off {
		return gtx
	}
	t.contentOK = false // the window's ops were reset for this frame
	if gtx.Now.IsZero() || !gtx.Now.Equal(t.inAt) {
		// no Frame read this frame's input (the session started in it)
		t.input(gtx)
		if t.state == off {
			return gtx
		}
	}
	hover := gtx
	hover.Source = gtx.Source.HoverOnly()
	if t.state == inspecting && t.fz == nil {
		if gtx.Now.IsZero() || !gtx.Now.Equal(t.recAt) {
			t.cur = 1 - t.cur // the record of the frame on screen stays for the outline
			t.recAt = gtx.Now
		}
		// recorded afresh when the frame is laid out again (a click
		// navigated mid-frame)
		slackc.InspectBegin(&t.frames[t.cur], gtx.Source)
		recorder.Store(uiguard.Goroutine())
		t.rec = true
	}
	return hover
}

// input reads a frame's input (Frame, or Begin in the frame a session
// starts): the pointer's events and the keys, what the pointer is over in
// the last frame's record (the frozen frame's, while frozen), and what a
// press, a drag or a release did.
func (t *Tool) input(gtx layout.Context) {
	src := gtx.Source
	t.frameN++
	t.inAt = gtx.Now
	t.win = gtx.Constraints.Max
	t.scale = gtx.Metric.PxPerDp
	if t.scale <= 0 {
		t.scale = 1
	}
	last := &t.frames[t.cur] // the frame before recorded into frames[cur]
	if r := t.rev; t.state == reviewing && r != nil && r.drawnFrames > 0 && r.shownAt.IsZero() {
		// a frame drew the review and went out before this one began: on
		// screen now, armed armDelay from here (armed)
		r.shownAt = t.inAt
	}
	in := t.readPointer(gtx)
	t.readKeys(gtx, in.released)
	defer t.pickKeys(gtx) // the keys after this frame's release, once it's known what it picked
	switch t.state {
	case off, reviewing:
		return
	case picked:
		// a new frame and the capture's wasn't shot (AfterFrame doesn't
		// run for a frame under the boot placeholder): give up, don't
		// leave the window taking no clicks; the capture goes with the
		// session (stop)
		t.dropCapture("dropped: its frame wasn't drawn")
		t.stop()
		t.host.Toast("Inspect: the capture's frame wasn't drawn, try again")
		return
	}
	var hits []slackc.InspectHit
	if t.rec && (t.state == inspecting || t.fz != nil) {
		// frozen, the frozen frame's areas are read whatever the state, so
		// what each one says the pointer is over stays current (Hold)
		hits = last.ReadHits(src, t.winAt)
		if t.fz != nil && !last.Held() {
			last.Hold(hits) // the freeze began this frame
		}
	}
	if t.state == inspecting && t.rec {
		t.hover = t.pickFrom(last, hits)
	}
	if t.state == inspecting && in.pressed {
		// the press: the report of what it's over now, while the frame
		// that drew it is at hand; whether it's a pick or a drag the
		// release says
		t.state, t.pend = pressing, nil
		if t.hover != nil {
			t.pend = t.newCapture(gtx, t.hover)
			t.pend.review = !in.shift // Shift copies at once
		}
		t.release0()
	}
	if t.state == pressing && in.far {
		t.state, t.pend = dragging, nil
	}
	if in.released {
		switch t.state {
		case pressing:
			if t.pend == nil {
				t.backToInspecting() // nothing under the pointer
				break
			}
			t.pend.review = t.pend.review && !in.shift // Shift at the press or the release
			t.capture, t.pend, t.state = t.pend, nil, picked
			if !t.capture.review {
				t.capture.tm.kind, t.capture.tm.atOnce = "shift-click", true
			}
			t.capture.tm.pick, t.capture.tm.pickN, t.capture.tm.win = time.Now(), t.frameN, t.win
		case dragging:
			r := t.dragRect(in.releaseAt)
			if min := int(minRegion*t.scale + .5); r.Dx() < min || r.Dy() < min {
				t.backToInspecting()
				t.say("drag a bigger region to grab it")
				break
			}
			at := image.Pt(int(in.releaseAt.X+.5), int(in.releaseAt.Y+.5))
			t.capture, t.state = t.newRegionCapture(gtx, r, at), picked
			t.capture.tm.pick, t.capture.tm.pickN, t.capture.tm.win = time.Now(), t.frameN, t.win
		}
	}
}

// release0 stops recording for a press (its report is built): the
// outline stays where it was until the release says what the press was.
func (t *Tool) release0() {
	if t.fz != nil {
		return // frozen: the frozen frame's record is all there is to pick from
	}
	h := t.hover
	t.release()
	t.hover = h
}

// dragRect is the region from the press to p, in the window (px).
func (t *Tool) dragRect(p f32.Point) image.Rectangle {
	r := image.Rect(int(t.from.X+.5), int(t.from.Y+.5), int(p.X+.5), int(p.Y+.5))
	return r.Intersect(image.Rectangle{Max: t.win})
}

// End finishes the frame Begin started (gtx: the frame's own context, at
// the window's origin, not Begin's): the outline and label, the banner,
// and the window-wide pointer area the tool reads, all deferred so they
// sit over every popup. What the window drew before them is the frame a
// freeze holds.
func (t *Tool) End(gtx layout.Context) {
	if slackc.Inspecting() {
		slackc.InspectEnd()
		recorder.Store(0)
	}
	if t.state == off {
		return
	}
	t.content, t.contentOK = gtx.Ops.Whole(), true
	t.contentOps, t.contentCP = gtx.Ops, gtx.Ops.Checkpoint()
	t.contentAt = frameKey{gtx.Constraints.Max, gtx.Metric}
	t.overlay(gtx)
}

// overlay is what the tool draws over a frame, the frame End finishes or
// the frozen one: this frame's filters (the session's keys and the
// area's pointer kinds, whose events come in the next frame's input), the
// outline, label and banner or the review, and the window-wide pointer
// area, deferred.
func (t *Tool) overlay(gtx layout.Context) {
	src := gtx.Source
	for {
		if _, ok := src.Event(t.keyFilters()...); !ok {
			break
		}
	}
	src.Event(pointer.Filter{Target: &t.ptag, Kinds: pointerKinds})
	m := op.Record(gtx.Ops)
	switch t.state {
	case inspecting, pressing, dragging:
		t.drawHover(gtx)
	case reviewing:
		t.drawReview(gtx)
	}
	pp := pointer.PassOp{}.Push(gtx.Ops)
	ar := clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops)
	pointer.CursorCrosshair.Add(gtx.Ops)
	event.Op(gtx.Ops, &t.ptag)
	ar.Pop()
	pp.Pop()
	op.Defer(gtx.Ops, m.Stop())
}

// keyFilters are the session's keys, the review's included (one set
// whatever the state: a frame's End declares what the next Begin reads,
// and AfterFrame starts a review in between).
func (t *Tool) keyFilters() []event.Filter {
	return []event.Filter{
		key.Filter{Name: key.NameEscape},
		key.Filter{Name: key.NameUpArrow},
		key.Filter{Name: key.NameDownArrow},
		key.Filter{Name: key.NameTab, Optional: key.ModShift}, // no focus moves underneath
		key.Filter{Name: key.NameReturn, Optional: key.ModShift},
		key.Filter{Name: key.NameEnter, Optional: key.ModShift},
		key.Filter{Name: key.NameDeleteBackward},
		key.Filter{Name: key.NameDeleteForward},
		key.Filter{Name: "Z", Required: key.ModShortcut},
		key.Filter{Name: "C", Required: key.ModShortcut},
		key.Filter{Name: FreezeKey},
		t.combo,
		// frozen, no widget asks for keys: every other press is the
		// tool's too, so none goes unhandled (macOS beeps for one)
		t.anyKey(),
	}
}

// anyKey is a filter for every key while frozen (a catch-all: Gio leaves
// out the keys it moves the focus with, which the Tab filter takes), and
// the freeze key's again otherwise, which adds nothing: in a session that
// isn't frozen the window's widgets still ask for theirs.
func (t *Tool) anyKey() key.Filter {
	if t.fz == nil {
		return key.Filter{Name: FreezeKey} // already asked for
	}
	return key.Filter{Optional: key.ModCtrl | key.ModCommand | key.ModShift | key.ModAlt | key.ModSuper}
}

// repeat reports a press of FreezeKey or Esc while that key is still down
// (no release read since its last press, which came less than a second
// ago): a held key's auto-repeat, which would toggle the freeze on every
// repeat, or unfreeze and then end the session. It notes those two keys'
// presses and releases.
func (t *Tool) repeat(ke key.Event) bool {
	var down *time.Time
	switch ke.Name {
	case FreezeKey:
		down = &t.freezeDown
	case key.NameEscape:
		down = &t.escDown
	default:
		return false
	}
	now := t.inAt
	if now.IsZero() {
		now = time.Now()
	}
	if ke.State != key.Press {
		*down = time.Time{}
		return false
	}
	held := !down.IsZero() && now.Sub(*down) < time.Second
	*down = now
	return held
}

// comboPress reports that e is the shortcut f: its key, its required
// modifiers and no others than its optional ones (the router's match).
func comboPress(f key.Filter, e key.Event) bool {
	return e.Name == f.Name && e.Modifiers&f.Required == f.Required && e.Modifiers&^(f.Required|f.Optional) == 0
}

// ptrRead is what a frame's pointer events did to a press: a primary
// press came (at t.from), the pointer went past dragSlop from it, the
// button came up (at releaseAt), and Shift was held at the press or the
// release (a click that copies at once, with no review).
type ptrRead struct {
	pressed, far, released bool
	shift                  bool
	releaseAt              f32.Point
}

// readPointer reads the window-wide area's events: in a review, each
// goes to reviewPointer; otherwise it reports what they did to a press.
func (t *Tool) readPointer(gtx layout.Context) ptrRead {
	var in ptrRead
	slop := float32(dragSlop) * t.scale
	held := t.state == pressing || t.state == dragging
	for {
		e, ok := gtx.Event(pointer.Filter{Target: &t.ptag, Kinds: pointerKinds})
		if !ok {
			break
		}
		pe, ok := e.(pointer.Event)
		if !ok {
			continue
		}
		if pe.Kind == pointer.Cancel {
			// the window lost the focus, or a freeze's end started the
			// pointer state over (Gio patch 49): a Cancel has no position,
			// so the pointer stays where it was, only not known to be over
			// anything until it moves (the outline goes, not to 0,0)
			t.ptrIn = false
			t.held = "" // the window lost the focus: the key's release goes elsewhere
			if t.state == reviewing {
				t.reviewPointer(pe)
			} else if held || in.pressed {
				t.backToInspecting()
				held, in = false, ptrRead{}
			}
			continue
		}
		t.hist[t.histN%len(t.hist)] = ptrAt{pe.Time, pe.Position}
		t.histN++
		t.ptr = pe.Position
		if t.state == reviewing {
			t.reviewPointer(pe)
			continue
		}
		switch pe.Kind {
		case pointer.Leave:
			t.ptrIn = false
		case pointer.Press:
			t.ptrIn = true
			if pe.Buttons&pointer.ButtonPrimary != 0 && t.state == inspecting && !in.pressed {
				in.pressed, t.from = true, pe.Position
				in.shift = in.shift || pe.Modifiers.Contain(key.ModShift)
			}
		case pointer.Drag:
			t.ptrIn = true
			if (held || in.pressed) && dist32(pe.Position, t.from) > slop {
				in.far = true
			}
		case pointer.Release:
			if (held || in.pressed) && pe.Buttons&pointer.ButtonPrimary == 0 {
				in.released, in.releaseAt = true, pe.Position
				in.far = in.far || dist32(pe.Position, t.from) > slop
				in.shift = in.shift || pe.Modifiers.Contain(key.ModShift)
				held = false
			}
		default:
			t.ptrIn = true
		}
	}
	return in
}

func dist32(a, b f32.Point) float32 {
	d := a.Sub(b)
	return float32(math.Hypot(float64(d.X), float64(d.Y)))
}

// winAt is the window position of the pointer event at time tm: the
// area saw every one; the last known position for an event from before
// the session (the Enter Gio sends new areas carries the last event's
// time).
func (t *Tool) winAt(tm time.Duration) (f32.Point, bool) {
	n := min(t.histN, len(t.hist))
	for i := 1; i <= n; i++ {
		h := t.hist[(t.histN-i)%len(t.hist)]
		if h.t == tm {
			return h.p, true
		}
	}
	return t.ptr, t.histN > 0 || t.ptr != (f32.Point{})
}

// SeedPointer tells a new session where the pointer is (window px), for
// the frames before it moves.
func (t *Tool) SeedPointer(p image.Point) {
	t.ptr = f32.Pt(float32(p.X), float32(p.Y))
	t.ptrIn = true
}

// readKeys reads the frame's key presses. In a frame whose pointer events
// released the button (released: a pick or a region, maybe), Enter and
// Mod+C wait for the release to be read (pickKeys): they came after it,
// and anyone fast enough to press one before the capture's frame was drawn
// means the capture (keys come after the pointer's events in a frame:
// Gio's order). Esc and the shortcut act at once, as they always have: Esc
// while pressing or dragging drops only that press (its release is then
// ignored, nothing is shot), and the shortcut ends the session.
func (t *Tool) readKeys(gtx layout.Context, released bool) {
	t.upNow = t.upNow[:0]
	for {
		e, ok := gtx.Event(t.keyFilters()...)
		if !ok {
			break
		}
		ke, ok := e.(key.Event)
		if !ok || t.repeat(ke) {
			continue
		}
		if t.held != "" && heldName(ke) == t.held {
			switch {
			case ke.State != key.Press:
				t.held = "" // up: its next press is a press
				continue
			case t.inAt.Sub(t.heldAt) < heldGap:
				t.heldAt = t.inAt // an auto-repeat
				continue
			}
			t.held = "" // a press long after the last (its release went elsewhere)
		}
		if ke.State != key.Press {
			t.upNow = append(t.upNow, heldName(ke))
			continue
		}
		if released && t.state != reviewing && enterKey(ke) {
			t.afterPick = append(t.afterPick, ke)
			continue
		}
		t.key(gtx, ke)
	}
}

// enterKey is Enter, Return or Mod+C: what copies in the review.
func enterKey(ke key.Event) bool {
	return ke.Name == key.NameReturn || ke.Name == key.NameEnter || ke.Modifiers.Contain(key.ModShortcut) && ke.Name == "C"
}

// heldName names a copy key for Tool.held: Return and Enter as one key.
func heldName(ke key.Event) key.Name {
	if ke.Name == key.NameEnter {
		return key.NameReturn
	}
	return ke.Name
}

// hold drops copy key ke for a guarded capture not armed yet: it and its
// auto-repeats copy nothing until it comes up.
func (t *Tool) hold(ke key.Event) {
	t.held, t.heldAt = heldName(ke), t.inAt
}

// heldGap is longer than any wait between a held key's auto-repeats (at
// the slowest settings macOS repeats after about 1.8 s and then as often,
// Windows after 1 s): a press of it later than that came after a release
// the window didn't get (it went to another window).
const heldGap = 2500 * time.Millisecond

// pickKeys acts on the keys readKeys held for after this frame's release
// (Enter, Mod+C): an element's pick waiting for its shot is copied as soon
// as it's shot, with no review (AfterFrame: the image the review's Enter
// copies with nothing marked; its margin is blurred anyway). A dragged
// region still opens its review, the only place it can be blurred: the
// Enter that came before it is dropped. With no capture they act as ever.
func (t *Tool) pickKeys(gtx layout.Context) {
	keys := t.afterPick
	t.afterPick = t.afterPick[:0]
	for _, ke := range keys {
		switch c := t.capture; {
		case t.state == picked && c != nil && !c.guarded():
			c.copyNow, c.tm.atOnce = true, true
			c.tm.kind += " (Enter in its frame)"
		case t.state == picked && c != nil:
			// a region, or a region's size: reviewed first, and the key
			// held until it comes up (its auto-repeats would copy once the
			// review armed), unless it came up in this frame already
			if !slices.Contains(t.upNow, heldName(ke)) {
				t.hold(ke)
			}
		default:
			t.key(gtx, ke)
		}
		t.host.Invalidate()
	}
}

// key acts on one key press.
func (t *Tool) key(gtx layout.Context, ke key.Event) {
	switch {
	case t.state == reviewing:
		t.reviewKey(ke)
	case t.state == picked:
		// the shot comes at this frame's end: the review takes keys
	case comboPress(t.combo, ke):
		t.stop()
	case ke.Name == key.NameEscape && (t.state == dragging || t.state == pressing):
		t.backToInspecting() // only the drag or the press; its release is ignored
	case ke.Name == key.NameEscape && t.fz != nil && t.state == inspecting:
		t.unfreeze() // the session goes on; Esc again ends it
	case ke.Name == key.NameEscape:
		t.stop()
	case t.state != inspecting:
	case ke.Name == FreezeKey: // its filter takes no modifier
		if t.fz != nil {
			t.unfreeze()
		} else if !t.freeze(gtx) {
			t.host.Toast("Inspect: nothing to freeze yet, try again")
		}
	case ke.Name == key.NameUpArrow:
		t.up++
	case ke.Name == key.NameDownArrow:
		t.up = max(t.up-1, 0)
	}
	t.host.Invalidate()
}

// pickFrom chooses what the pointer is over among the last frame's hits:
// the component drawn last (Gio's hit test already dropped what's covered),
// a native drawn inside a plugin's tree over the tree itself, then t.up
// components out.
func (t *Tool) pickFrom(f *slackc.InspectFrame, hits []slackc.InspectHit) *pick {
	ptr := image.Pt(int(t.ptr.X+.5), int(t.ptr.Y+.5))
	p := &pick{frame: f, node: -1, inner: -1, tree: -1}
	regions := t.regions()
	for _, r := range regions {
		if ptr.In(r.Rect) {
			p.region = r.Name
		}
	}
	if !t.ptrIn && len(hits) == 0 {
		return nil
	}
	origin := map[int]slackc.InspectHit{}
	best := -1
	bestOver := false // best is in a layer drawn over everything (a tooltip)
	var tb treeBox
	for _, h := range hits {
		origin[h.Node] = h
		n := &f.Nodes[h.Node]
		if n.Kind == slackc.InspectTree {
			if b := innermostBox(f, h); p.tree < 0 || b.over(tb) {
				p.tree, p.local, tb = h.Node, h.Local, b
			}
			continue
		}
		// a component, an element with a size, or a popup's layer: the
		// one drawn last, and before all one in a tooltip, drawn over
		// everything whatever was recorded after it
		switch o := drawnOver(f, h.Node); {
		case o && !bestOver:
			best, bestOver = h.Node, true
		case o == bestOver:
			best = max(best, h.Node)
		}
	}
	if p.tree >= 0 && best < p.tree && !bestOver {
		// the tree was drawn after the natives inside it: one of them
		// under the pointer still wins; the component the plugin patches
		// (the tree's parent) and a native elsewhere don't
		if parent := f.Nodes[p.tree].Parent; best < 0 || best == parent || !descends(f, best, parent) {
			best = p.tree
		}
	}
	if best < 0 {
		// no component: the shell's area
		for _, r := range regions {
			if ptr.In(r.Rect) {
				p.rect = r.Rect
			}
		}
		if p.rect.Empty() {
			return nil
		}
		p.label = p.region
		return p
	}
	p.inner = best
	// ↑ counts from the innermost element: another one starts over
	if k := innerKey(f, best, origin[best].Origin); k != t.upKey {
		t.up, t.upKey = 0, k
	}
	// ↑: the enclosing components that were hit too
	node := best
	for k := 0; k < t.up; k++ {
		next := -1
		for i := f.Nodes[node].Parent; i >= 0; i = f.Nodes[i].Parent {
			if _, ok := origin[i]; ok { // hit: it has an area (a component, a sized element)
				next = i
				break
			}
		}
		if next < 0 {
			t.up = k
			break
		}
		node = next
	}
	p.node = node
	h := origin[node]
	p.origin = h.Origin
	p.rect = image.Rectangle{Min: h.Origin, Max: h.Origin.Add(f.Nodes[node].Size)}
	if f.Nodes[node].Kind == slackc.InspectTree {
		p.label = "plugin tree"
		if bs := f.Boxes(node, p.local); len(bs) > 0 {
			p.rect = bs[0].Rect.Add(h.Origin)
			p.label = boxSelector(bs[0])
		}
	}
	if p.label == "" {
		p.label = t.labelFor(f, node)
	}
	if l := layerOf(f, node); l != "" {
		p.region = l // a popup's (slackc.InspectOverlay): not the window's area under it
	}
	return p
}

// RecordedLayers lists the popup layers of the record the picker reads
// (the last frame's, the frozen frame's while frozen): each one's name,
// with its box when it has one (tests).
func (t *Tool) RecordedLayers() []string {
	f := &t.frames[t.cur]
	var out []string
	for i := range f.Nodes {
		if n := &f.Nodes[i]; n.Kind == slackc.InspectLayer {
			parent := "none"
			if n.Parent >= 0 {
				pn := &f.Nodes[n.Parent]
				parent = pn.Name
				if pn.Kind == slackc.InspectElement {
					parent = selector(pn, nil, 2)
				}
			}
			out = append(out, fmt.Sprintf("%s %v sized %v (node %d of %d, in %s)", n.Name, n.Size, n.Sized(), i, len(f.Nodes), parent))
		}
	}
	return out
}

// drawnOver reports that node i is drawn in a layer drawn over
// everything else (slackc.InspectNode.Over: a tooltip).
func drawnOver(f *slackc.InspectFrame, i int) bool {
	for _, j := range f.Chain(i) {
		if n := &f.Nodes[j]; n.Kind == slackc.InspectLayer {
			return n.Over
		}
	}
	return false
}

// layerOf is the name of the innermost popup layer node i is drawn in
// (slackc.InspectLayer: an overlay, a submenu, a tooltip), "" for the
// window's own.
func layerOf(f *slackc.InspectFrame, i int) string {
	for _, j := range f.Chain(i) {
		if n := &f.Nodes[j]; n.Kind == slackc.InspectLayer {
			return n.Name
		}
	}
	return ""
}

// treeBox is the innermost box of a plugin tree under the pointer, to
// choose between trees that are both hit.
type treeBox struct {
	node   int
	native bool // a "@" box (a native the tree draws), or no box at all
	area   int
}

func innermostBox(f *slackc.InspectFrame, h slackc.InspectHit) treeBox {
	bs := f.Boxes(h.Node, h.Local)
	if len(bs) == 0 {
		return treeBox{node: h.Node, native: true, area: 1 << 62}
	}
	r := bs[0].Rect
	return treeBox{node: h.Node, native: bs[0].Component != "", area: r.Dx() * r.Dy()}
}

// over reports that b is more specific than o: a tree's own element over
// a native's box, then the smaller box, then the tree drawn later. A
// native's children are a tree of their own (LayoutElement: the
// UserPronouns span among BaseBroadcastPreamble's children), painted
// before the tree around the native, whose innermost box there is only
// the native's "@" box.
func (b treeBox) over(o treeBox) bool {
	if b.native != o.native {
		return !b.native
	}
	if b.area != o.area {
		return b.area < o.area
	}
	return b.node > o.node
}

// innerKey names what the pointer is innermost over across frames (node
// indices aren't stable): its name or selector and where it is.
func innerKey(f *slackc.InspectFrame, i int, origin image.Point) string {
	n := &f.Nodes[i]
	name := n.Name
	if n.Kind == slackc.InspectElement {
		name = selector(n, nil, 0)
	}
	return fmt.Sprintf("%d %s %v %v", n.Kind, name, origin, n.Size)
}

// descends reports that node i is anc or inside it.
func descends(f *slackc.InspectFrame, i, anc int) bool {
	for ; i >= 0; i = f.Nodes[i].Parent {
		if i == anc {
			return true
		}
	}
	return false
}

// labelFor is the hover label: the component and its root element, or
// the element.
func (t *Tool) labelFor(f *slackc.InspectFrame, i int) string {
	n := &f.Nodes[i]
	switch n.Kind {
	case slackc.InspectElement:
		return selector(n, nil, 3)
	case slackc.InspectLayer:
		return n.Name
	}
	s := n.Name
	if el := rootElement(f, i); el >= 0 {
		s += " · " + selector(&f.Nodes[el], nil, 3)
	}
	return s
}

// rootElement is the first element component i entered, -1 if none.
func rootElement(f *slackc.InspectFrame, i int) int {
	for j := i + 1; j < len(f.Nodes); j++ {
		if f.Nodes[j].Parent == i && f.Nodes[j].Kind == slackc.InspectElement {
			return j
		}
		if !descends(f, j, i) {
			break
		}
	}
	return -1
}

// selector is an element as CSS: tag.class.class[attr=value]; at most
// maxClasses classes (0: all). Run-time attribute values go through red
// (nil: left out).
func selector(n *slackc.InspectNode, red *Redactor, maxClasses int) string {
	var b strings.Builder
	b.WriteString(n.Tag())
	cs := n.Classes()
	if maxClasses > 0 && len(cs) > maxClasses {
		cs = cs[:maxClasses]
	}
	for _, c := range cs {
		b.WriteByte('.')
		b.WriteString(c)
	}
	attrs := n.Attrs()
	for i := 0; i+1 < len(attrs); i += 2 {
		if name := attrs[i]; name == "data-qa" || name == "role" {
			fmt.Fprintf(&b, "[%s=%s]", name, attrs[i+1]) // fixed in Clack's source
		}
	}
	if k, v := n.RuntimeAttr(); k != "" && red != nil {
		fmt.Fprintf(&b, "[%s=%s]", k, red.Attr(k, v)) // from data: a tag unless an enumeration
	}
	return b.String()
}

func boxSelector(b slackc.InspectBox) string {
	var s strings.Builder
	if b.Component != "" {
		s.WriteString("<" + b.Component + ">")
	} else {
		s.WriteString(b.Tag)
	}
	for _, c := range b.Classes {
		s.WriteByte('.')
		s.WriteString(c)
	}
	for i := 0; i+1 < len(b.Attrs); i += 2 {
		if safeValue(b.Attrs[i+1]) {
			fmt.Fprintf(&s, "[%s=%s]", b.Attrs[i], b.Attrs[i+1])
		}
	}
	return s.String()
}

// drawHover outlines what the pointer is over, with its label, and the
// session's banner.
func (t *Tool) drawHover(gtx layout.Context) {
	th := t.host.Theme()
	sz := gtx.Constraints.Max
	if t.state == dragging {
		t.drawDrag(gtx, th)
		return
	}
	if p := t.hover; p != nil && !p.rect.Empty() {
		r := p.rect
		w := max(gtx.Dp(2), 2)
		fill(gtx, r, color.NRGBA{R: outlineCol.R, G: outlineCol.G, B: outlineCol.B, A: 0x24})
		frame(gtx, r.Inset(-w), w, outlineCol)
		if th != nil && th.Shaper != nil {
			text := fmt.Sprintf("%s  %d×%d", p.label, int(float32(r.Dx())/gtx.Metric.PxPerDp+.5), int(float32(r.Dy())/gtx.Metric.PxPerDp+.5))
			t.drawLabel(gtx, th, text, r, sz)
		}
	}
	if th != nil && th.Shaper != nil {
		t.banner(gtx, th, t.sessionHint(gtx))
	}
}

// The banner's hints: a session's, a frozen session's (freeze.go), a
// drag's.
const (
	HintInspect = "Inspect: click or drag to blur, then copy · shift-click copies at once · ↑ ↓ parent · F freezes · Esc to stop"
	HintFrozen  = "Frozen: click or drag to blur, then copy · shift-click copies at once · ↑ ↓ parent · F or Esc unfreezes"
	HintDrag    = "Inspect: release to grab this region · Esc cancels the drag"
)

// drawDrag draws the region being dragged out: the window dimmed around
// it, its frame, its size, and the banner.
func (t *Tool) drawDrag(gtx layout.Context, th *theme.Theme) {
	sz := gtx.Constraints.Max
	r := t.dragRect(t.ptr)
	dim := color.NRGBA{R: 0x0b, G: 0x0c, B: 0x0e, A: 0x66}
	fill(gtx, image.Rect(0, 0, sz.X, r.Min.Y), dim)
	fill(gtx, image.Rect(0, r.Max.Y, sz.X, sz.Y), dim)
	fill(gtx, image.Rect(0, r.Min.Y, r.Min.X, r.Max.Y), dim)
	fill(gtx, image.Rect(r.Max.X, r.Min.Y, sz.X, r.Max.Y), dim)
	if !r.Empty() {
		w := max(gtx.Dp(2), 2)
		frame(gtx, r.Inset(-w), w, outlineCol)
	}
	if th == nil || th.Shaper == nil {
		return
	}
	if !r.Empty() {
		t.drawLabel(gtx, th, fmt.Sprintf("region  %d×%d", int(float32(r.Dx())/gtx.Metric.PxPerDp+.5), int(float32(r.Dy())/gtx.Metric.PxPerDp+.5)), r, sz)
	}
	t.banner(gtx, th, HintDrag)
}

// banner is the session's pill at the top of the window.
func (t *Tool) banner(gtx layout.Context, th *theme.Theme, msg string) {
	sz := gtx.Constraints.Max
	tw := th.Measure(gtx, 13, theme.Bold, msg)
	h := gtx.Dp(28)
	r := image.Rect((sz.X-tw)/2-gtx.Dp(14), gtx.Dp(8), (sz.X+tw)/2+gtx.Dp(14), gtx.Dp(8)+h)
	theme.RRect(gtx, r, h/2, color.NRGBA{R: 0x16, G: 0x17, B: 0x1a, A: 0xf0})
	theme.Border(gtx, r, h/2, max(gtx.Dp(1), 1), outlineCol)
	t.text(gtx, th, msg, image.Pt(r.Min.X+gtx.Dp(14), r.Min.Y), h, tw)
}

func (t *Tool) drawLabel(gtx layout.Context, th *theme.Theme, text string, r image.Rectangle, win image.Point) {
	tw := th.Measure(gtx, 12, theme.Bold, text)
	h := gtx.Dp(22)
	pad := gtx.Dp(8)
	w := tw + 2*pad
	x := min(max(r.Min.X, 0), max(win.X-w, 0))
	y := r.Min.Y - h - gtx.Dp(4)
	if y < 0 {
		y = min(r.Max.Y+gtx.Dp(4), win.Y-h)
	}
	lr := image.Rect(x, y, x+w, y+h)
	theme.RRect(gtx, lr, gtx.Dp(4), outlineCol)
	t.text(gtx, th, text, image.Pt(x+pad, y), h, tw)
}

func (t *Tool) text(gtx layout.Context, th *theme.Theme, s string, at image.Point, h, w int) {
	defer op.Offset(at).Push(gtx.Ops).Pop()
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(w+1, h)}
	m := op.Record(gtx.Ops)
	d := th.Text(g, theme.LabelOpts{Font: theme.Font(theme.Bold), Size: unit.Sp(12), MaxLines: 1, Color: color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}}, s)
	c := m.Stop()
	defer op.Offset(image.Pt(0, (h-d.Size.Y)/2)).Push(gtx.Ops).Pop()
	c.Add(gtx.Ops)
}

func fill(gtx layout.Context, r image.Rectangle, c color.NRGBA) {
	paint.FillShape(gtx.Ops, c, clip.Rect(r).Op())
}

// frame strokes w px inside r.
func frame(gtx layout.Context, r image.Rectangle, w int, c color.NRGBA) {
	fill(gtx, image.Rect(r.Min.X, r.Min.Y, r.Max.X, r.Min.Y+w), c)
	fill(gtx, image.Rect(r.Min.X, r.Max.Y-w, r.Max.X, r.Max.Y), c)
	fill(gtx, image.Rect(r.Min.X, r.Min.Y+w, r.Min.X+w, r.Max.Y-w), c)
	fill(gtx, image.Rect(r.Max.X-w, r.Min.Y+w, r.Max.X, r.Max.Y-w), c)
}

func goos() string { return runtime.GOOS + "/" + runtime.GOARCH }
