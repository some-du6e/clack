package slackc

import (
	"log"
	"sync"

	"gioui.org/layout"

	"github.com/some-du6e/yoloslackgoclient/internal/imgcache"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc/css"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
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
	warned map[string]bool
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
