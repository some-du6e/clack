// Package inspect is Clack's inspect tool: a developer shortcut, not a
// Slack feature, that makes a UI bug report one paste. The shortcut
// (off until the Konami code, CLACK_INSPECT=1 or the hidden ui.json pref
// inspect_tool turns it on) starts an inspect session, like a browser's
// inspect element: the Slack component under the pointer gets an outline
// and a label, ↑ and ↓ pick its parent or go back, Esc ends the session,
// and a click picks the element; a drag past dragSlop grabs the region
// dragged out instead, for what the picker can't reach. The shot is
// then shown for review (review.go): rects dragged over it are blurred
// out of the image (Redact), Enter copies it and Esc drops it. The copy
// is one image on the clipboard: the element as it was drawn, outlined,
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
// the clicks and keys. Where things are comes from slackc's inspect
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
	"strings"
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
	// Describe fills the report's window and page fields; id-bearing
	// fields go through red (UI goroutine).
	Describe(r *Report, red *Redactor)
	// ConvKind is "public", "private", "dm", "mpdm" or "" for a channel
	// id: what kind of conversation, never which.
	ConvKind(id string) string
	// Regions are the shell's areas this frame, outermost first.
	Regions() []Region
	// Toast shows a short note at the bottom of the window.
	Toast(text string)
	// CopyText puts text on the clipboard.
	CopyText(text string)
	// Post runs f on the UI goroutine; Invalidate asks for a frame.
	Post(f func())
	Invalidate()
	// ConfigDir is where reports are saved ("": nowhere).
	ConfigDir() string
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
	// the button) is shot in AfterFrame, which starts the review.
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
	win   image.Point // the window's size (px), at the last Begin
	scale float32     // px per dp, at the last Begin

	hover    *pick
	up       int           // ↑ presses: how many components out from the innermost
	pend     *capture      // pressing: the element's report, waiting for the release
	capture  *capture      // picked: waiting for the frame to be shot
	rev      *review       // reviewing
	lastDone chan struct{} // the last report's delivery (Wait)
	lastRep  *Report       // the last report delivered (tests, after Wait)
	lastNow  time.Time     // the frame Begin last read input for
	upKey    string        // the innermost element t.up counts from (innerKey)
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
	core.ObserveExperiments(observeExperiment)
}

func (t *Tool) stop() {
	t.state = off
	t.pend, t.rev = nil, nil
	t.release()
}

// release stops recording frames: no outline, no experiments observed,
// and the last frames' nodes (widgets, props funcs, scopes) let go of.
func (t *Tool) release() {
	t.hover, t.rec = nil, false
	core.ObserveExperiments(nil)
	for i := range t.frames {
		clear(t.frames[i].Nodes)
		t.frames[i].Nodes = t.frames[i].Nodes[:0]
	}
}

// backToInspecting ends a press or a drag that grabbed nothing: the
// session goes on, recording afresh.
func (t *Tool) backToInspecting() {
	// no outline until a frame is recorded again: the one shown was the
	// press's, whose nodes are let go of
	t.state, t.pend, t.rec, t.hover = inspecting, nil, false, nil
	core.ObserveExperiments(observeExperiment)
}

// observeExperiment gives an experiment read on the UI goroutine to the
// node drawing.
func observeExperiment(name, group string) {
	if !uiguard.OnUI() {
		return
	}
	if f := slackc.CurrentInspectFrame(); f != nil {
		f.NoteExperiment(name, group)
	}
}

const pointerKinds = pointer.Move | pointer.Enter | pointer.Leave | pointer.Press | pointer.Release | pointer.Drag | pointer.Cancel

// Begin starts a frame. Call it first in the window's frame, before
// anything reads input; lay the window out with the context it returns
// (gtx itself while no session runs) and call End last.
func (t *Tool) Begin(gtx layout.Context) layout.Context {
	if t.state == off {
		return gtx
	}
	src := gtx.Source
	hover := gtx
	hover.Source = src.HoverOnly()
	if !gtx.Now.IsZero() && gtx.Now.Equal(t.lastNow) {
		// the same frame laid out again (a click navigated mid-frame):
		// its input is read, record it afresh
		if t.state == inspecting {
			slackc.InspectBegin(&t.frames[t.cur], src)
		}
		return hover
	}
	t.lastNow = gtx.Now
	t.win = gtx.Constraints.Max
	t.scale = gtx.Metric.PxPerDp
	if t.scale <= 0 {
		t.scale = 1
	}
	last := &t.frames[t.cur] // the frame before recorded into frames[cur]
	in := t.readPointer(gtx)
	t.readKeys(gtx)
	switch t.state {
	case off:
		return gtx
	case reviewing:
		return hover
	case picked:
		// a new frame and the capture's wasn't shot (AfterFrame doesn't
		// run for a frame under the boot placeholder): give up, don't
		// leave the window taking no clicks
		t.stop()
		t.host.Toast("Inspect: the capture's frame wasn't drawn, try again")
		return gtx
	}
	if t.state == inspecting && t.rec {
		hits := last.ReadHits(src, t.winAt)
		t.hover = t.pickFrom(last, hits)
	}
	if t.state == inspecting && in.pressed {
		// the press: the report of what it's over now, while the frame
		// that drew it is at hand; whether it's a pick or a drag the
		// release says
		t.state, t.pend = pressing, nil
		if t.hover != nil {
			t.pend = t.newCapture(gtx, t.hover)
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
			t.capture, t.pend, t.state = t.pend, nil, picked
		case dragging:
			r := t.dragRect(in.releaseAt)
			if min := int(minRegion*t.scale + .5); r.Dx() < min || r.Dy() < min {
				t.backToInspecting()
				t.host.Toast("Inspect: drag a bigger region to grab it")
				break
			}
			at := image.Pt(int(in.releaseAt.X+.5), int(in.releaseAt.Y+.5))
			t.capture, t.state = t.newRegionCapture(gtx, r, at), picked
		}
	}
	if t.state == inspecting {
		t.cur = 1 - t.cur
		slackc.InspectBegin(&t.frames[t.cur], src)
		t.rec = true
	}
	return hover
}

// release0 stops recording for a press (its report is built): the
// outline stays where it was until the release says what the press was.
func (t *Tool) release0() {
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
// sit over every popup.
func (t *Tool) End(gtx layout.Context) {
	if slackc.Inspecting() {
		slackc.InspectEnd()
	}
	if t.state == off {
		return
	}
	// this frame's filters: the session's keys and the area's pointer
	// kinds (their events come in the next frame's Begin)
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
		t.combo,
	}
}

// comboPress reports that e is the shortcut f: its key, its required
// modifiers and no others than its optional ones (the router's match).
func comboPress(f key.Filter, e key.Event) bool {
	return e.Name == f.Name && e.Modifiers&f.Required == f.Required && e.Modifiers&^(f.Required|f.Optional) == 0
}

// ptrRead is what a frame's pointer events did to a press: a primary
// press came (at t.from), the pointer went past dragSlop from it, and
// the button came up (at releaseAt).
type ptrRead struct {
	pressed, far, released bool
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
		case pointer.Cancel:
			if held || in.pressed {
				t.backToInspecting()
				held, in = false, ptrRead{}
			}
		case pointer.Press:
			t.ptrIn = true
			if pe.Buttons&pointer.ButtonPrimary != 0 && t.state == inspecting && !in.pressed {
				in.pressed, t.from = true, pe.Position
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

func (t *Tool) readKeys(gtx layout.Context) {
	for {
		e, ok := gtx.Event(t.keyFilters()...)
		if !ok {
			break
		}
		ke, ok := e.(key.Event)
		if !ok || ke.State != key.Press {
			continue
		}
		switch {
		case t.state == reviewing:
			t.reviewKey(ke)
		case t.state == picked:
			// the shot comes at this frame's end: the review takes keys
		case comboPress(t.combo, ke):
			t.stop()
		case ke.Name == key.NameEscape && t.state == dragging:
			t.backToInspecting() // only the drag; its release is ignored
		case ke.Name == key.NameEscape:
			t.stop()
		case t.state != inspecting:
		case ke.Name == key.NameUpArrow:
			t.up++
		case ke.Name == key.NameDownArrow:
			t.up = max(t.up-1, 0)
		}
		t.host.Invalidate()
	}
}

// pickFrom chooses what the pointer is over among the last frame's hits:
// the component drawn last (Gio's hit test already dropped what's covered),
// a native drawn inside a plugin's tree over the tree itself, then t.up
// components out.
func (t *Tool) pickFrom(f *slackc.InspectFrame, hits []slackc.InspectHit) *pick {
	ptr := image.Pt(int(t.ptr.X+.5), int(t.ptr.Y+.5))
	p := &pick{frame: f, node: -1, inner: -1, tree: -1}
	for _, r := range t.host.Regions() {
		if ptr.In(r.Rect) {
			p.region = r.Name
		}
	}
	if !t.ptrIn && len(hits) == 0 {
		return nil
	}
	origin := map[int]slackc.InspectHit{}
	best := -1
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
		best = max(best, h.Node) // a component, or an element with a size
	}
	if p.tree >= 0 && best < p.tree {
		// the tree was drawn after the natives inside it: one of them
		// under the pointer still wins; the component the plugin patches
		// (the tree's parent) and a native elsewhere don't
		if parent := f.Nodes[p.tree].Parent; best < 0 || best == parent || !descends(f, best, parent) {
			best = p.tree
		}
	}
	if best < 0 {
		// no component: the shell's area
		for _, r := range t.host.Regions() {
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
	return p
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
	if n.Kind == slackc.InspectElement {
		return selector(n, nil, 3)
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
		t.banner(gtx, th, HintInspect)
	}
}

// The banner's hints: a session's, a drag's.
const (
	HintInspect = "Inspect: click an element to copy a report · drag to grab a region · ↑ ↓ parent · Esc to stop"
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
