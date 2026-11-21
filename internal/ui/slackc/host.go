package slackc

import (
	"image"
	"image/color"
	"log"
	"math"
	"strings"
	"sync"

	"gioui.org/f32"
	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/ui/slackc/css"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// HostComponent draws a Slack component that appears in a plugin's host
// tree as {"t":"@","n":name}: api.elements (Button, Tooltip, SvgIcon…) and
// Slack components plugins render directly (ConnectedBaseAvatar, Blocks,
// MrkdwnElement…). It lays out like an inline-block box: it gets the
// constraints the layout engine computed and returns its size.
type HostComponent func(gtx layout.Context, h *Host) layout.Dimensions

var hosts struct {
	sync.RWMutex
	m      map[string]HostComponent
	el     map[string]*HostElement
	warned map[string]bool
}

// HostElement is the DOM element a Slack component renders at its root,
// for the CSS that styles it. A host component registered with one
// (RegisterHostElement) matches selectors as that element does in
// Slack's DOM: its tag, and the classes Slack's component adds beside the
// className a plugin passes (Button is button.c-button.c-button--outline
// .c-button--small…, so OwlMode's .taut-owl-save__button.c-button--outline
// reaches it). Its box is a host node for the host base sheet too: Slack's
// rules for those classes, and for the Slack classes a plugin puts on it
// (.p-block_kit_button_element), style it.
type HostElement struct {
	Tag string
	// Classes are the classes Slack's component adds for these props
	// (nil: none).
	Classes func(p Props) []string
	// OwnBox marks a component that draws its own padding, borders and
	// background (kit's Button, Slack's c-button look): the layout engine
	// sizes and places its border box and leaves those to it, reading
	// what CSS sets through Host.Style and Host.Has.
	OwnBox bool
	// Style, when set, gives the element its own look where no rule set a
	// property (has reports the ones a rule set), as a UA stylesheet and
	// Slack's own rules give Slack's element: its font and colour, which
	// its children inherit (a button's icon and label are its colour, at
	// its size and weight). It runs whenever the box is styled.
	Style func(p Props, st *css.Style, has func(prop string) bool)
}

// RegisterHostElement declares the element behind host components
// registered under names. Call it before trees with them are styled
// (with RegisterHost).
func RegisterHostElement(el HostElement, names ...string) {
	hosts.Lock()
	defer hosts.Unlock()
	if hosts.el == nil {
		hosts.el = map[string]*HostElement{}
	}
	e := el
	for _, n := range names {
		hosts.el[n] = &e
	}
}

func lookupHostElement(name string) *HostElement {
	hosts.RLock()
	defer hosts.RUnlock()
	return hosts.el[name]
}

// RegisterHost registers fn for name and any aliases (e.g. "Avatar" and
// "ConnectedBaseAvatar"). Owners register their widgets here so plugins
// can render them; later registrations replace earlier ones.
func RegisterHost(fn HostComponent, names ...string) {
	hosts.Lock()
	defer hosts.Unlock()
	if hosts.m == nil {
		hosts.m = map[string]HostComponent{}
	}
	for _, n := range names {
		hosts.m[n] = fn
	}
}

// LookupHost returns the registered host component for name.
func LookupHost(name string) (HostComponent, bool) {
	hosts.RLock()
	fn, ok := hosts.m[name]
	hosts.RUnlock()
	if !ok {
		hosts.Lock()
		if hosts.warned == nil {
			hosts.warned = map[string]bool{}
		}
		if !hosts.warned[name] {
			hosts.warned[name] = true
			log.Printf("slackc: no native component for %q; drawing its children", name)
		}
		hosts.Unlock()
	}
	return fn, ok
}

// Host is the context a HostComponent draws with.
type Host struct {
	// Node is the "@" node; Props its decoded props (functions are JSFunc
	// or the original Go func, React-element props are Element).
	Node  *Node
	Props Props
	// Style is the computed style of the "@" node itself (className
	// passed to the component, matching CSS rules, inherited text style).
	Style *css.Style

	view *View
	fr   *frame
	set  propSet // the properties a rule set on the node (Has)
}

// Has reports whether a rule set prop ("color", "padding-left",
// "border-color"…) on the component's own box: a plugin's or the user's,
// or Slack's for a class the component carries (the host base sheet), as
// opposed to a value it inherited or the property's initial one. A
// component that draws its own look (HostElement.OwnBox) uses CSS where
// this is true and its own defaults elsewhere. Some properties share a
// bit: a border's widths and styles (its colours have their own),
// min/max sizes, a side's padding longhands and shorthands.
func (h *Host) Has(prop string) bool {
	b, ok := propBits[prop]
	return ok && h.set&b != 0
}

// PlaceChildren tells the layout engine where the component drew what
// Children laid out this frame: at is the children's top-left in px from
// the component's own top-left. Their window rects (getBoundingClientRect,
// ResizeObserver, Host.WindowRect inside) follow it in this frame: the
// children's scope flushed its rects as Children returned, before the
// component knew where it would centre them, so the engine flushes them
// again when it paints the component, where the frame's layout has put
// it (painter.native). Without it they sit at the component's corner.
func (h *Host) PlaceChildren(at image.Point) {
	if h.view == nil || h.Node == nil || measuring > 0 {
		return // a measure pass places nothing (measuring)
	}
	b := h.view.boxOf(h.Node)
	if b == nil {
		return
	}
	s := h.view.subs[subKey{b: b, kind: 1}]
	if s == nil {
		return
	}
	if p := f32.Pt(float32(at.X), float32(at.Y)); p != s.place {
		s.place = p
		s.rectsDirty = true
	}
}

// Theme is Clack's theme.
func (h *Host) Theme() *theme.Theme { return env().Theme }

// Images is the image cache.
func (h *Host) Images() *imgcache.Cache { return env().Images }

// Children lays out the node's children (React children) as a block
// flow under the component's style.
func (h *Host) Children(gtx layout.Context) layout.Dimensions {
	return h.view.layoutKids(gtx, h.fr, h.Node, h.Style)
}

// HasChildren reports whether the node has any children.
func (h *Host) HasChildren() bool { return len(h.Node.C) > 0 }

// Element lays out a React-element prop (Tooltip tip, Label text…).
// Strings and numbers in such props are drawn as text.
func (h *Host) Element(gtx layout.Context, prop string) layout.Dimensions {
	switch v := h.Props[prop].(type) {
	case Element:
		return h.view.layoutNodes(gtx, h.fr, v.Nodes, h.Node, h.Style)
	case string:
		return h.view.layoutText(gtx, h.fr, v, h.Style)
	case float64:
		return h.view.layoutText(gtx, h.fr, keyString(v), h.Style)
	}
	return layout.Dimensions{}
}

// HasElement reports whether a React-element-or-text prop is set.
func (h *Host) HasElement(prop string) bool {
	switch v := h.Props[prop].(type) {
	case Element:
		return len(v.Nodes) > 0
	case string:
		return v != ""
	case float64:
		return true
	}
	return false
}

// Nodes lays out arbitrary host nodes in the component's context (e.g.
// a select option's label).
func (h *Host) Nodes(gtx layout.Context, nodes []*Node) layout.Dimensions {
	return h.view.layoutNodes(gtx, h.fr, nodes, h.Node, h.Style)
}

// Call invokes a function prop (JS or Go) with args.
func (h *Host) Call(prop string, args ...any) { h.Props.Invoke(prop, args...) }

// State returns the component instance's persistent state, created by
// mk on first use. It lives as long as the host instance (Node.K) stays
// in the tree.
func (h *Host) State(mk func() any) any {
	return h.view.hostState(h.Node.K, mk)
}

// Invalidate requests a new frame.
func (h *Host) Invalidate() { invalidate() }

// ShowOverlay stacks a popup above the shell.
func (h *Host) ShowOverlay(o interface {
	Layout(gtx layout.Context) layout.Dimensions
	Dismissed() bool
}) {
	if f := env().ShowOverlay; f != nil {
		f(o)
	}
}

// RenderFn mounts a root rendering the JS function fn(args...) (render
// props such as MenuTrigger's renderMenu) and returns a Sub that lays it
// out; Close it when done.
func (h *Host) RenderFn(fn JSFunc, args ...any) *Sub {
	s := &Sub{root: newRoot()}
	s.view = NewView(s.root)
	rootsMu.Lock()
	roots[s.root] = &inst{root: s.root, fn: true, view: s.view, sub: s}
	rootsMu.Unlock()
	if b := bridge(); b != nil {
		b.RenderFn(s.root, fn.ID, encodeArgs(args))
	}
	return s
}

// NewSub registers an empty root for a JS-rendered subtree the caller
// mounts itself (Preferences > Taut: Bridge.RenderEditor / RenderDataPanel
// / RenderPreferencesTab with Root()). Close it when done.
func NewSub() *Sub {
	s := &Sub{root: newRoot()}
	s.view = NewView(s.root)
	rootsMu.Lock()
	roots[s.root] = &inst{root: s.root, fn: true, view: s.view, sub: s}
	rootsMu.Unlock()
	return s
}

// Root is the root id to mount into.
func (s *Sub) Root() uint64 { return s.root }

// Sub is a JS-rendered subtree owned by a host component.
type Sub struct {
	root uint64
	view *View
	tree []*Node
	mu   sync.Mutex
}

// Layout lays out the subtree (nothing until JS delivers it).
func (s *Sub) Layout(gtx layout.Context) layout.Dimensions {
	s.mu.Lock()
	t := s.tree
	s.mu.Unlock()
	if t == nil {
		return layout.Dimensions{}
	}
	return s.view.Layout(gtx, t, "", nil, nil)
}

// Ready reports whether JS delivered the subtree.
func (s *Sub) Ready() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tree != nil
}

// Close unmounts the subtree.
func (s *Sub) Close() {
	rootsMu.Lock()
	delete(roots, s.root)
	rootsMu.Unlock()
	s.view.Close()
	if b := bridge(); b != nil {
		b.Unmount(s.root)
	}
}

// PropChildren returns the children a plugin passed to <Original>
// ({children}), already rendered to host nodes. A native whose Slack
// counterpart takes children (BaseBroadcastPreamble, ReactionBar, …) lays
// them out with LayoutElement instead of drawing its own parts; the native
// parts it handed out as Slots come back inside as "@" slot nodes.
func PropChildren(p Props) (Element, bool) {
	e, ok := p["children"].(Element)
	return e, ok && len(e.Nodes) > 0
}

// LayoutElement lays out a host element (children or a React-node prop)
// that came back in a native's props, in the render context of the
// patched component currently being drawn. Outside a patched render it
// draws nothing.
func LayoutElement(gtx layout.Context, e Element) layout.Dimensions {
	c := curRender
	if c == nil || len(e.Nodes) == 0 {
		return layout.Dimensions{}
	}
	parent := c.n
	if len(e.Nodes) > 0 && e.Nodes[0].parent != nil {
		parent = e.Nodes[0].parent
	}
	return c.view.layoutNodes(gtx, c.fr, e.Nodes, parent, c.st)
}

// LayoutChildren is LayoutElement for the children a native lays out
// inside the element it draws itself, d with mods, as Slack's DOM has
// them there: that element is their CSS parent, they inherit its text,
// and when it is a flex container (Slack's own rule for it in the base
// sheet, or a plugin's) they are its flex items under its flex
// properties. The reaction pill's emoji and count, handed back by a
// patched ReactionBar, are .c-reaction's inline-flex items, 4px apart
// and centred, in 12px text; laid out as one inline run they touched
// and sat on the baseline of a line box with the row's 15px strut. The
// native has entered d with mods (Enter); with no CSS installed Enter
// pushes nothing, and d's element goes on the stack for the layout.
func (d *ClassDef) LayoutChildren(gtx layout.Context, mods uint64, e Element) layout.Dimensions {
	c := curRender
	if c == nil || len(e.Nodes) == 0 {
		return layout.Dimensions{}
	}
	parent := c.n
	if e.Nodes[0].parent != nil {
		parent = e.Nodes[0].parent
	}
	pushed := false
	if !d.entered(c) {
		k := nsKey{def: d, mods: mods}
		var pe *stackEntry
		if n := len(stack); n > 0 {
			pe = &stack[n-1]
			k.parent = pe.key
		}
		stack = append(stack, lookupNative(k, pe).entry())
		pushed = true
	}
	st := stack[len(stack)-1].style
	dims := c.view.layoutNodesIn(gtx, c.fr, e.Nodes, parent, st, true)
	if pushed {
		stack = stack[:len(stack)-1]
	}
	return dims
}

// entered reports that the element on top of the stack is d's, entered
// by the native c is drawing.
func (d *ClassDef) entered(c *renderCtx) bool {
	if len(stack) <= c.depth {
		return false
	}
	switch el := stack[len(stack)-1].el.(type) {
	case *nativeEl:
		return el.def == d
	case *kidsEl:
		return el.def == d
	}
	return false
}

// InheritText is the text style a native's own DOM gives a React-node
// prop it wraps, which the prop's nodes inherit (TypingNames' <b>{name}</b>
// inside .p-notification_bar__section: 10.5px, 1rem line height,
// foreground_max, bold). Zero fields keep the render context's value.
type InheritText struct {
	Size, LineHeight float32 // CSS px
	Weight           int     // 100..900
	Color            color.NRGBA
}

// LayoutElementText is LayoutElement with the nodes' CSS parent styled
// by t, as if the native had drawn its own element around them.
func LayoutElementText(gtx layout.Context, e Element, t InheritText) layout.Dimensions {
	c := curRender
	if c == nil || len(e.Nodes) == 0 {
		return layout.Dimensions{}
	}
	parent := c.n
	if e.Nodes[0].parent != nil {
		parent = e.Nodes[0].parent
	}
	var st css.Style
	if c.st != nil {
		st = *c.st
	} else {
		st = css.Initial(cssEnv().Fg)
	}
	if t.Size > 0 {
		st.FontSize = t.Size
	}
	if t.LineHeight > 0 {
		st.LineHeight = css.Px(t.LineHeight)
	}
	if t.Weight > 0 {
		st.FontWeight = t.Weight
	}
	if t.Color.A > 0 {
		st.Color = t.Color
	}
	// The element the native stands for, on the native stack too: natives
	// among the nodes (MemberLink's Enter) and the nodes laid out after
	// one take their inherited text from the stack (nativeAnchor), not
	// from the host box, and inside a native's own elements (the
	// composer's) that chain ends in the root's 15px.
	el, _, pk := stackParent()
	var set propSet
	for _, k := range []string{"font-size", "font-weight", "line-height", "color"} {
		set |= propBits[k]
	}
	h := uint64(math.Float32bits(st.FontSize))<<32 ^ uint64(st.FontWeight)<<16 ^ uint64(math.Float32bits(st.LineHeight.Px)) ^
		uint64(st.Color.R)<<56 ^ uint64(st.Color.G)<<48 ^ uint64(st.Color.B)<<40
	pushHost(el, &st, set, mix(pk^h^0x7e47c0de))
	defer popHost()
	return c.view.layoutNodes(gtx, c.fr, e.Nodes, parent, &st)
}

// Items splits children into their flex items, for a native whose Slack
// element is a flex container (the reaction bar): each element child is
// laid out on its own (LayoutElement) and placed by the native, rather
// than all of them flowing as one inline run, whose line box would add
// the strut's descent under atomic inlines like the reaction pills.
// Whitespace-only text is dropped, as flex layout does.
func Items(e Element) []Element {
	out := make([]Element, 0, len(e.Nodes))
	for i, n := range e.Nodes {
		if n.T == "#" && strings.TrimSpace(n.V) == "" {
			continue
		}
		out = append(out, Element{Nodes: e.Nodes[i : i+1 : i+1]})
	}
	return out
}

// curRender is the innermost host-tree render in progress (UI
// goroutine); the layout engine sets it around native calls.
var curRender *renderCtx

type renderCtx struct {
	view *View
	fr   *frame
	st   *css.Style
	n    *Node // the "@" node whose native is drawing
	// depth is the element stack's height when the native was called;
	// above it are the elements the native entered (nativeAnchor).
	depth int
}

// WindowRect is the component's border box in window pixels as of the
// last frame it was painted (ok false before that, or when the window
// origin isn't known yet). Popovers and menus anchor to it.
func (h *Host) WindowRect() (image.Rectangle, bool) {
	if h.view == nil || h.Node == nil {
		return image.Rectangle{}, false
	}
	b := h.view.byK[h.Node.K]
	if b == nil || b.scope == nil || b.rectFr == 0 {
		return image.Rectangle{}, false
	}
	ox, oy := b.scope.windowOrigin()
	r := b.rect
	return image.Rect(int(r.x0+ox+.5), int(r.y0+oy+.5), int(r.x1+ox+.5), int(r.y1+oy+.5)), true
}

// WindowSize is the window size in pixels (for flipping popovers at the
// edges).
func (h *Host) WindowSize() image.Point { return windowSize }
