// Package inspect is Clack's inspect tool: a developer shortcut, not a
// Slack feature, that makes a UI bug report one paste. The shortcut
// (off unless CLACK_INSPECT=1 or the hidden ui.json pref inspect_tool is
// set) starts an inspect session, like a browser's inspect element: the
// Slack component under the pointer gets an outline and a label, ↑ and ↓
// pick its parent or go back, Esc ends the session, and a click picks the
// element. Clack then copies one image to the clipboard: the window as it
// was drawn with the element outlined, over a panel with a report (which
// component and element, its classes, its rect, the colours drawn there,
// the theme, the experiments its code read, and tags for the ids it was
// drawn for: one-way hashes, salted per report, so the report is safe to
// paste in #clack). The image and the report's text are also saved in
// <config>/inspect-reports.
//
// While a session runs the window is laid out with a hover-only input
// source (Gio patch 21): hover states, scrolling, carets and animations
// stay, and no button, editor or shortcut reacts; the tool alone reads
// the clicks and keys. Where things are comes from slackc's inspect
// records (slackc/inspect.go): each component boundary gets a
// pass-through pointer area, and Gio's own hit test says which are under
// the pointer. Plugins' host trees go down to their boxes. Outside
// components (plain Gio widgets) the report falls back to the shell area.
package inspect

import (
	"fmt"
	"image"
	"image/color"
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
	// picked: the click's frame is captured; the session ends when the
	// button comes up, so no widget gets a release it never saw pressed.
	picked
)

// Tool is one window's inspect tool. Everything runs on the UI goroutine.
type Tool struct {
	host  Host
	state state
	combo key.Filter // the shortcut, which also ends a session

	frames [2]slackc.InspectFrame
	cur    int // frames[cur] records this frame; the other one is the last
	rec    bool

	ptag   int // the window-wide pointer area (topmost, pass-through)
	ptr    f32.Point
	ptrIn  bool
	hist   [32]ptrAt
	histN  int
	pickAt time.Time

	hover    *pick
	up       int // ↑ presses: how many components out from the innermost
	capture  *capture
	lastDone chan struct{} // the last report's delivery (Wait)
	lastNow  time.Time     // the frame Begin last read input for
	endAfter bool          // picked, and the release came with the press
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

// Active reports a running session (a pick's frame included).
func (t *Tool) Active() bool { return t.state != off && !(t.state == picked && t.endAfter) }

// Hovered is what the session outlines now: its label and rect (window
// px); ok false when nothing is.
func (t *Tool) Hovered() (label string, rect image.Rectangle, ok bool) {
	if t.state != inspecting || t.hover == nil {
		return "", image.Rectangle{}, false
	}
	return t.hover.label, t.hover.rect, true
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
	t.hover, t.up, t.rec, t.endAfter = nil, 0, false, false
	t.frames[0].Nodes, t.frames[1].Nodes = t.frames[0].Nodes[:0], t.frames[1].Nodes[:0]
	core.ObserveExperiments(observeExperiment)
}

func (t *Tool) stop() {
	t.state = off
	t.hover, t.rec = nil, false
	core.ObserveExperiments(nil)
	// let go of last frames' nodes (widgets, props funcs, scopes)
	for i := range t.frames {
		clear(t.frames[i].Nodes)
		t.frames[i].Nodes = t.frames[i].Nodes[:0]
	}
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
	if t.state == picked && t.endAfter {
		// the pick's frame was laid out (and its click dropped): done
		t.stop()
		return gtx
	}
	last := &t.frames[t.cur] // the frame before recorded into frames[cur]
	clicked, released := t.readPointer(gtx)
	t.readKeys(gtx)
	if t.state == off {
		return gtx
	}
	if t.state == inspecting && t.rec {
		hits := last.ReadHits(src, t.winAt)
		t.hover = t.pickFrom(last, hits)
		if clicked && t.hover != nil {
			t.capture = t.newCapture(gtx, t.hover)
			t.state, t.pickAt = picked, time.Now()
			// the button came up in the same batch: this frame (laid out
			// hover-only, so no widget sees the click) is the last
			t.endAfter = released
		}
	}
	if t.state == picked && time.Since(t.pickAt) > 3*time.Second {
		t.stop() // no release came (the pointer left the window pressed)
		return gtx
	}
	if t.state == inspecting {
		t.cur = 1 - t.cur
		slackc.InspectBegin(&t.frames[t.cur], src)
		t.rec = true
	}
	return hover
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
	if t.state == picked && t.endAfter {
		return // the session ends at the next frame's Begin
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
	if t.state == inspecting {
		t.drawHover(gtx)
	}
	pp := pointer.PassOp{}.Push(gtx.Ops)
	ar := clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops)
	pointer.CursorCrosshair.Add(gtx.Ops)
	event.Op(gtx.Ops, &t.ptag)
	ar.Pop()
	pp.Pop()
	op.Defer(gtx.Ops, m.Stop())
}

func (t *Tool) keyFilters() []event.Filter {
	return []event.Filter{
		key.Filter{Name: key.NameEscape},
		key.Filter{Name: key.NameUpArrow},
		key.Filter{Name: key.NameDownArrow},
		key.Filter{Name: key.NameTab, Optional: key.ModShift}, // no focus moves underneath
		t.combo,
	}
}

// readPointer reads the window-wide area's events and reports a primary
// press, and a release after it in the same batch.
func (t *Tool) readPointer(gtx layout.Context) (clicked, released bool) {
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
		switch pe.Kind {
		case pointer.Leave:
			t.ptrIn = false
		case pointer.Cancel:
			if t.state == picked {
				t.stop()
			}
		case pointer.Press:
			t.ptrIn = true
			if pe.Buttons&pointer.ButtonPrimary != 0 && t.state == inspecting {
				clicked = true
			}
		case pointer.Release:
			if t.state == picked {
				t.stop()
			}
			released = clicked
		default:
			t.ptrIn = true
		}
	}
	return clicked, released
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
		case ke.Name == key.NameEscape:
			t.stop()
		case ke.Name == t.combo.Name && ke.Modifiers == t.combo.Required:
			t.stop()
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
	for _, h := range hits {
		origin[h.Node] = h
		n := &f.Nodes[h.Node]
		if n.Kind == slackc.InspectTree {
			if p.tree < 0 || h.Node > p.tree {
				p.tree, p.local = h.Node, h.Local
			}
			continue
		}
		best = max(best, h.Node)
	}
	if p.tree >= 0 && best < p.tree {
		// the tree was drawn after the natives inside it: one of them
		// under the pointer still wins, a native elsewhere doesn't
		if best < 0 || !descends(f, best, f.Nodes[p.tree].Parent) {
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
	// ↑: the enclosing components that were hit too
	node := best
	for k := 0; k < t.up; k++ {
		next := -1
		for i := f.Nodes[node].Parent; i >= 0; i = f.Nodes[i].Parent {
			if _, ok := origin[i]; ok && f.Nodes[i].Kind != slackc.InspectElement {
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

// descends reports that node i is anc or inside it.
func descends(f *slackc.InspectFrame, i, anc int) bool {
	for ; i >= 0; i = f.Nodes[i].Parent {
		if i == anc {
			return true
		}
	}
	return false
}

// labelFor is the hover label: the component and its root element.
func (t *Tool) labelFor(f *slackc.InspectFrame, i int) string {
	n := &f.Nodes[i]
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
		msg := "Inspect: click an element to copy a report · ↑ ↓ parent · Esc to stop"
		tw := th.Measure(gtx, 13, theme.Bold, msg)
		h := gtx.Dp(28)
		r := image.Rect((sz.X-tw)/2-gtx.Dp(14), gtx.Dp(8), (sz.X+tw)/2+gtx.Dp(14), gtx.Dp(8)+h)
		theme.RRect(gtx, r, h/2, color.NRGBA{R: 0x16, G: 0x17, B: 0x1a, A: 0xf0})
		frame(gtx, r, 1, outlineCol)
		t.text(gtx, th, msg, image.Pt(r.Min.X+gtx.Dp(14), r.Min.Y), h, tw)
	}
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
