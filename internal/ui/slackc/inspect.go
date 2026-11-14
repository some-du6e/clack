package slackc

import (
	"image"
	"image/color"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
)

// Inspect mode, for Clack's inspect tool (internal/ui/inspect, a developer
// shortcut for bug reports; nothing Slack has). While a frame runs between
// InspectBegin and InspectEnd, slackc records the Slack components natives
// draw (Render, RenderLazy, Component), the elements natives enter
// (ClassDef.Enter, so their Slack classes) and the scopes of plugins' host
// trees, nested as they were drawn. Each record with a size gets a
// pass-through pointer area over it with a new tag, so Gio's router says
// which of them are under the pointer through its own hit test (clips, and
// whatever is drawn over them, count as for any widget): it sends a new
// area under the pointer an Enter at the end of the frame, in the area's
// coordinates, and the tool reads them in the next frame (ReadHits).
//
// The records hold no message content: names, classes, attributes Slack
// puts in its DOM (data-qa, role) and sizes. Props stay with the caller
// until the tool asks for them (InspectNode.Props), and the tool keeps
// only ids from them, hashed (internal/ui/inspect redact.go).
//
// Outside inspect mode nothing changes: the unpatched fast paths load one
// more atomic, and Enter and Leave one more too.

var (
	inspecting atomic.Bool
	inspectF   *InspectFrame // UI goroutine, under the frame lock
)

// InspectKind is what an InspectNode recorded.
type InspectKind uint8

const (
	// InspectComponent is a Slack component boundary (Render, Component).
	InspectComponent InspectKind = iota + 1
	// InspectElement is an element a native entered (ClassDef.Enter): its
	// tag, classes and attributes; natives don't say how big it is.
	InspectElement
	// InspectTree is the scope of a plugin's host tree: InspectFrame.Boxes
	// finds its boxes under a point.
	InspectTree
)

// InspectNode is one thing a frame drew.
type InspectNode struct {
	Kind   InspectKind
	Parent int // the enclosing node's index, -1 at the top
	// Name is a component's Slack name; Patched, that a plugin's tree drew
	// it.
	Name    string
	Patched bool
	// Size is in px (zero: unknown, as for every element).
	Size image.Point
	// Exps are the experiments read while it drew ("name=group", sorted).
	Exps []string

	def   *ClassDef
	mods  uint64
	attrK string
	attrV string
	extra string
	props func() Props
	scope *scope
	tag   *inspectTag
}

type inspectTag struct{ _ byte }

// Tag is an element's tag name ("div", "button"; "" for other kinds).
func (n *InspectNode) Tag() string {
	if n.def == nil {
		return ""
	}
	return n.def.tag
}

// Classes are an element's classes, its active modifiers included.
func (n *InspectNode) Classes() []string {
	if n.def == nil {
		return nil
	}
	e := nativeEl{def: n.def, mods: n.mods, extra: strings.Fields(n.extra)}
	return append([]string(nil), e.Classes()...)
}

// Attrs are an element's static attributes as name, value pairs: its
// class's (data-qa, role, aria-label), fixed in Clack's source.
func (n *InspectNode) Attrs() []string {
	if n.def == nil {
		return nil
	}
	return append([]string(nil), n.def.attrs...)
}

// RuntimeAttr is the attribute the element was entered with (EnterAttr:
// a channel's id, an item key), "" if none. Its value comes from data.
func (n *InspectNode) RuntimeAttr() (name, value string) { return n.attrK, n.attrV }

// Props builds a component's props as a plugin patching it would get them
// (nil without a props func). Built only when asked: they can hold message
// content, which nothing here keeps.
func (n *InspectNode) Props() Props {
	if n.props == nil {
		return nil
	}
	return n.props()
}

// InspectFrame is one frame's record.
type InspectFrame struct {
	Nodes []InspectNode
	src   input.Source
	stack []int
}

// InspectBegin starts recording f for a frame (UI goroutine, inside the
// frame lock): src is the window's own input Source, which declares the
// areas' pointer filters even while the frame is laid out with another.
func InspectBegin(f *InspectFrame, src input.Source) {
	f.Nodes = f.Nodes[:0]
	f.stack = f.stack[:0]
	f.src = src
	inspectF = f
	inspecting.Store(true)
}

// InspectEnd stops recording.
func InspectEnd() {
	inspecting.Store(false)
	inspectF = nil
}

// Inspecting reports that a frame is being recorded (UI goroutine).
func Inspecting() bool { return inspecting.Load() && inspectF != nil }

func (f *InspectFrame) top() int {
	if n := len(f.stack); n > 0 {
		return f.stack[n-1]
	}
	return -1
}

func (f *InspectFrame) push(n InspectNode) int {
	n.Parent = f.top()
	f.Nodes = append(f.Nodes, n)
	i := len(f.Nodes) - 1
	f.stack = append(f.stack, i)
	return i
}

// area puts node i's pass-through pointer area over sz at the current
// op origin and declares its filter for this frame.
func (f *InspectFrame) area(gtx layout.Context, i int, sz image.Point) {
	if sz.X <= 0 || sz.Y <= 0 {
		return
	}
	t := new(inspectTag) // a new tag every frame: Gio sends each new area under the pointer an Enter
	f.Nodes[i].tag = t
	f.src.Event(pointer.Filter{Target: t, Kinds: pointer.Enter | pointer.Leave | pointer.Move | pointer.Cancel})
	pp := pointer.PassOp{}.Push(gtx.Ops)
	ar := clip.Rect{Max: sz}.Push(gtx.Ops)
	event.Op(gtx.Ops, t)
	ar.Pop()
	pp.Pop()
}

// beginComponent records component name around its native's draw.
func (f *InspectFrame) beginComponent(name string, props func() Props) int {
	return f.push(InspectNode{Kind: InspectComponent, Name: name, Patched: Patched(name), props: props})
}

func (f *InspectFrame) endComponent(gtx layout.Context, i int, d layout.Dimensions) {
	// back to the component's depth: an element some native entered and
	// never left stays inside it
	for n := len(f.stack); n > 0; n-- {
		if f.stack[n-1] == i {
			f.stack = f.stack[:n-1]
			break
		}
	}
	if i < len(f.Nodes) {
		f.Nodes[i].Size = d.Size
		f.area(gtx, i, d.Size)
	}
}

func inspectEnter(d *ClassDef, mods uint64, ak, av, extra string) {
	if f := inspectF; f != nil {
		f.push(InspectNode{Kind: InspectElement, def: d, mods: mods, attrK: ak, attrV: av, extra: extra})
	}
}

func inspectLeave() {
	f := inspectF
	if f == nil {
		return
	}
	// only an element: a component's end pops the component
	if n := len(f.stack); n > 0 && f.Nodes[f.stack[n-1]].Kind == InspectElement {
		f.stack = f.stack[:n-1]
	}
}

// tree records a host tree's scope s, painted at the current op origin
// over r (scope coordinates).
func (f *InspectFrame) tree(gtx layout.Context, s *scope, r image.Rectangle) {
	i := f.push(InspectNode{Kind: InspectTree, scope: s, Size: r.Max})
	f.stack = f.stack[:len(f.stack)-1]
	if r.Empty() {
		return
	}
	n := &f.Nodes[i]
	t := new(inspectTag)
	n.tag = t
	f.src.Event(pointer.Filter{Target: t, Kinds: pointer.Enter | pointer.Leave | pointer.Move | pointer.Cancel})
	pp := pointer.PassOp{}.Push(gtx.Ops)
	ar := clip.Rect(r).Push(gtx.Ops)
	event.Op(gtx.Ops, t)
	ar.Pop()
	pp.Pop()
}

// NoteExperiment attributes an experiment read to the node drawing now
// (the tool's core observer calls it on the UI goroutine).
func (f *InspectFrame) NoteExperiment(name, group string) {
	i := f.top()
	if i < 0 {
		return
	}
	s := name + "=" + group
	if group == "" {
		s = name + "=(none)"
	}
	n := &f.Nodes[i]
	j := sort.SearchStrings(n.Exps, s)
	if j < len(n.Exps) && n.Exps[j] == s {
		return
	}
	n.Exps = append(n.Exps, "")
	copy(n.Exps[j+1:], n.Exps[j:])
	n.Exps[j] = s
}

// CurrentInspectFrame is the frame being recorded, or nil (UI goroutine).
func CurrentInspectFrame() *InspectFrame {
	if !inspecting.Load() {
		return nil
	}
	return inspectF
}

// InspectHit is a node under the pointer.
type InspectHit struct {
	Node int
	// Origin is the node's area's top-left in window px; Local, the
	// pointer in the area's coordinates.
	Origin image.Point
	Local  f32.Point
}

// ReadHits reads the pointer events of the areas f put down (call it in
// the next frame, before that frame records anything) and returns the
// nodes whose area the pointer is in, in drawing order. winAt gives the
// window position of the pointer event at time t (the tool's overlay saw
// every one).
func (f *InspectFrame) ReadHits(src input.Source, winAt func(t time.Duration) (f32.Point, bool)) []InspectHit {
	var out []InspectHit
	for i := range f.Nodes {
		n := &f.Nodes[i]
		if n.tag == nil {
			continue
		}
		in := false
		var last pointer.Event
		for {
			e, ok := src.Event(pointer.Filter{Target: n.tag, Kinds: pointer.Enter | pointer.Leave | pointer.Move | pointer.Cancel})
			if !ok {
				break
			}
			pe, ok := e.(pointer.Event)
			if !ok {
				continue
			}
			switch pe.Kind {
			case pointer.Enter, pointer.Move:
				in, last = true, pe
			default:
				in = false
			}
		}
		if !in {
			continue
		}
		w, ok := winAt(last.Time)
		if !ok {
			continue
		}
		o := w.Sub(last.Position)
		out = append(out, InspectHit{Node: i, Origin: image.Pt(int(o.X+.5), int(o.Y+.5)), Local: last.Position})
	}
	return out
}

// Chain is node i and its ancestors, innermost first.
func (f *InspectFrame) Chain(i int) []int {
	var out []int
	for i >= 0 && i < len(f.Nodes) {
		out = append(out, i)
		i = f.Nodes[i].Parent
	}
	return out
}

// InspectBox is one element of a plugin's host tree.
type InspectBox struct {
	// Component is a "@" box's Slack name (a native drawn in the tree).
	Component string
	Tag       string
	Classes   []string
	// Attrs are the box's data-qa and role (name, value pairs); nothing a
	// plugin may have filled with text (aria-label, title).
	Attrs []string
	// Rect is the painted border box, in the scope's coordinates.
	Rect image.Rectangle
	// The computed style's colours and font.
	Color, Background color.NRGBA
	FontSize          float32
	FontWeight        int
	FontFamily        string
}

// Boxes is the box chain of tree node i under local (the scope's
// coordinates, from its InspectHit), innermost first: what the scope's
// own hit test finds for a pointer there, and its ancestors. Call it
// before the next layout of the tree (ReadHits' frame).
func (f *InspectFrame) Boxes(i int, local f32.Point) []InspectBox {
	if i < 0 || i >= len(f.Nodes) || f.Nodes[i].scope == nil {
		return nil
	}
	b := f.Nodes[i].scope.hitTest(local)
	var out []InspectBox
	for ; b != nil; b = b.parent {
		if !b.isElem() || b.n == nil {
			continue // text, anonymous and pseudo boxes
		}
		x := InspectBox{Tag: b.n.T, Rect: irect(b.rect), Color: b.st.Color, Background: b.st.Background.Color,
			FontSize: b.st.FontSize, FontWeight: b.st.FontWeight, FontFamily: b.st.FontFamily}
		if b.n.IsComponent() {
			x.Component, x.Tag = b.n.N, ""
		}
		x.Classes = append(x.Classes, b.n.Class...)
		for _, a := range []string{"data-qa", "role"} {
			if v, ok := b.n.AttrString(a); ok && v != "" {
				x.Attrs = append(x.Attrs, a, v)
			}
		}
		out = append(out, x)
	}
	return out
}
