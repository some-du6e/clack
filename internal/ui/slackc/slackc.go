// Package slackc is Clack's Slack component registry: the patch point that
// lets Taut plugins (api.patchComponent) wrap or replace Clack's native
// widgets by Slack's React component names, the native layout engine for
// the host trees plugins render, and the CSS engine for api.setStyle and
// user CSS. See docs/PLUGINS.md and docs/PLUGINS-components.md.
//
// Owners wrap a widget at Slack's component boundary:
//
//	return slackc.Render(gtx, "MessageSender", props, r.layoutSender)
//
// With no plugin patching "MessageSender" that is one atomic load and a
// nil check before calling the native func: no JS, no allocation. When
// props are costly to build, check first:
//
//	if slackc.Patched("MessageSender") { return slackc.Render(gtx, "MessageSender", r.senderProps(), r.layoutSender) }
//	return r.layoutSender(gtx, nil)
package slackc

import (
	"image"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"gioui.org/layout"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/imgcache"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// Native lays out a Clack widget. p is nil on the fast path (no plugin);
// under a patch it holds the props the plugin passed to <Original>, and
// the native honours any Slack-shaped overrides in it.
type Native func(gtx layout.Context, p Props) layout.Dimensions

// Bridge is the JS side (internal/taut over internal/jsrt), adapted by the
// ui root. Every method is asynchronous and safe from the UI goroutine.
type Bridge interface {
	// Render mounts or updates root: render the (patched) Slack component
	// name with props, a JSON object. The tree comes back via Deliver.
	Render(root uint64, name string, props []byte)
	// RenderFn mounts root rendering the JS function fn called with args
	// (a JSON array): render props like MenuTrigger.renderMenu.
	RenderFn(root uint64, fn int64, args []byte)
	Unmount(root uint64)
	// Dispatch fires a DOM event at the host node with K == target (0 =
	// document/window); JS runs capture and bubble.
	Dispatch(target int64, event []byte)
	// Call calls a JS function prop with a JSON array of args. done, when
	// non-nil, gets the JSON result (or error) on any goroutine, e.g.
	// ContextMenuTrigger's getTemplate().
	Call(fn int64, args []byte, done func(result []byte, err error))
	// SetRects reports laid-out host node boxes (x, y, w, h in CSS px,
	// window coordinates) for getBoundingClientRect / ResizeObserver.
	SetRects(rects map[int64][4]float32)
	// SetViewport reports the window size, scale and color scheme.
	SetViewport(w, h, dpr float32, dark bool)
}

// Env is what slackc needs from the ui root.
type Env struct {
	Theme      *theme.Theme
	Images     *imgcache.Cache
	Invalidate func()
	// ShowOverlay stacks a popup above the shell (popups.Overlay).
	ShowOverlay func(o interface {
		Layout(gtx layout.Context) layout.Dimensions
		Dismissed() bool
	})
	Toast func(text string)
	// OpenURL opens a link in the browser.
	OpenURL func(url string)
	// Navigate follows a Clack route ("c/<id>", …) for Slack-internal links.
	Navigate func(route string)
	// Core is the in-process Slack service (users, channels, emoji).
	Core *core.App
	// PointerWin is the last pointer position in window coordinates
	// (ui.App.ptrWin). With a widget's local pointer position it gives the
	// widget's window origin, as ui's originTracker does.
	PointerWin func() image.Point
	// OpenEmojiPicker, OpenProfile and OpenMenu are the popups the ui root
	// offers every package (anchor in window coordinates).
	OpenEmojiPicker func(anchor image.Point, onPick func(name string))
	OpenProfile     func(anchor image.Point, userID string)
}

var (
	envMu  sync.RWMutex
	envVal Env

	bridgeVal atomic.Pointer[Bridge]

	// patched is nil until a plugin patches something: the fast path is
	// one atomic load and a nil check.
	patched atomic.Pointer[map[string]bool]
)

// Init installs the ui root's environment. Call before the first frame.
func Init(e Env) {
	envMu.Lock()
	envVal = e
	envMu.Unlock()
}

func env() Env {
	envMu.RLock()
	defer envMu.RUnlock()
	return envVal
}

// CurrentEnv returns the environment Init installed (host components and
// the popups they open read Core, PointerWin, OpenEmojiPicker… from it).
func CurrentEnv() Env { return env() }

func invalidate() {
	if f := env().Invalidate; f != nil {
		f()
	}
}

// SetBridge installs (or with nil removes) the JS side.
func SetBridge(b Bridge) {
	if b == nil {
		bridgeVal.Store(nil)
		return
	}
	bridgeVal.Store(&b)
}

func bridge() Bridge {
	if p := bridgeVal.Load(); p != nil {
		return *p
	}
	return nil
}

// SetPatched replaces the set of component names that have a live patch.
// Called from JS whenever patchComponent registers or a plugin stops.
func SetPatched(names []string) {
	if len(names) == 0 {
		patched.Store(nil)
	} else {
		m := make(map[string]bool, len(names))
		for _, n := range names {
			m[n] = true
		}
		patched.Store(&m)
	}
	invalidate()
}

// Patched reports whether a plugin patches name. Cheap: an atomic load,
// and a map lookup only while any patch exists.
func Patched(name string) bool {
	m := patched.Load()
	return m != nil && (*m)[name]
}

// Render lays out the Slack component name: native(gtx, props) unless a
// plugin patches name, in which case the plugin's host tree is laid out
// and its <Original {...props}> calls native with the plugin's props.
// Never blocks on JS: until the first tree arrives, native draws.
//
// props["$key"] (any comparable value) gives the instance a stable
// identity across prop changes (hook state survives); without it the
// instance is keyed by the props' hash.
//
// native escapes on the patched path, so a capturing closure or method
// value passed here is heap-allocated on every call. Hot widgets use
// Component, which is allocation-free when unpatched.
func Render(gtx layout.Context, name string, props Props, native Native) layout.Dimensions {
	if patched.Load() == nil { // inlined: one atomic load
		return native(gtx, props)
	}
	return renderSlow(gtx, name, props, native)
}

func renderSlow(gtx layout.Context, name string, props Props, native Native) layout.Dimensions {
	if m := patched.Load(); m == nil || !(*m)[name] {
		return native(gtx, props)
	}
	return renderPatched(gtx, name, props, native)
}

// RenderLazy is Render with props built only when name is patched.
func RenderLazy(gtx layout.Context, name string, props func() Props, native Native) layout.Dimensions {
	if patched.Load() == nil {
		return native(gtx, nil)
	}
	return renderLazySlow(gtx, name, props, native)
}

func renderLazySlow(gtx layout.Context, name string, props func() Props, native Native) layout.Dimensions {
	if m := patched.Load(); m == nil || !(*m)[name] {
		return native(gtx, nil)
	}
	return renderPatched(gtx, name, props(), native)
}

// Component is the allocation-free form of Render for hot widgets: with
// method expressions for props and native, nothing is allocated unless a
// plugin patches name.
//
//	return slackc.Component(gtx, "MessageSender", r, (*row).senderProps, (*row).layoutSender)
//
// props may be nil (no props). native's Props argument is nil when
// unpatched.
func Component[T any](gtx layout.Context, name string, recv T, props func(T) Props, native func(T, layout.Context, Props) layout.Dimensions) layout.Dimensions {
	if patched.Load() == nil { // inlined: the native becomes a direct call
		return native(recv, gtx, nil)
	}
	return componentSlow(gtx, name, recv, props, native)
}

func componentSlow[T any](gtx layout.Context, name string, recv T, props func(T) Props, native func(T, layout.Context, Props) layout.Dimensions) layout.Dimensions {
	if m := patched.Load(); m == nil || !(*m)[name] {
		return native(recv, gtx, nil)
	}
	return componentPatched(gtx, name, recv, props, native)
}

func componentPatched[T any](gtx layout.Context, name string, recv T, props func(T) Props, native func(T, layout.Context, Props) layout.Dimensions) layout.Dimensions {
	var p Props
	if props != nil {
		p = props(recv)
	}
	return renderPatched(gtx, name, p, func(gtx layout.Context, p Props) layout.Dimensions {
		return native(recv, gtx, p)
	})
}

// --- patched instances (UI goroutine) ---

type instKey struct {
	name    string
	key     string
	hash    uint64
	parent  uint64 // enclosing patched root (0 = top level)
	parentK int64  // the "@" node in it whose native drew this one
}

// NestedBridge is implemented by bridges that can mount a root inside
// another root's React tree (a portal under the host node parentK of
// root parent), so React context flows from a parent patch into the
// patched components its <Original> draws natively. Without it nested
// roots render on their own, as before.
type NestedBridge interface {
	RenderIn(root, parent uint64, parentK int64, name string, props []byte)
}

type inst struct {
	root  uint64
	name  string
	key   instKey
	sent  uint64 // hash of the props last sent to JS
	used  time.Time
	slots map[string]int64
	view  *View
	tree  atomic.Pointer[[]*Node]
	fn    bool // a RenderFn root
	sub   *Sub
}

var (
	rootSeq atomic.Uint64

	// roots maps root ids to instances for Deliver (any goroutine).
	rootsMu sync.Mutex
	roots   = map[uint64]*inst{}

	// insts is the UI goroutine's lookup by component identity.
	insts     = map[instKey]*inst{}
	lastSweep time.Time
)

func newRoot() uint64 { return rootSeq.Add(1) &^ (1 << 63) }

func now(gtx layout.Context) time.Time {
	if !gtx.Now.IsZero() {
		return gtx.Now
	}
	return time.Now()
}

func renderPatched(gtx layout.Context, name string, props Props, native Native) layout.Dimensions {
	b := bridge()
	if b == nil {
		return native(gtx, props)
	}
	pump()
	t := now(gtx)
	sweep(t)

	h := hashProps(props)
	k := instKey{name: name}
	// Drawn by a native inside another patched component's tree (a row's
	// <Original> calling MessageText): the child root belongs under that
	// "@" node, so JS can mount it inside the parent's React tree and
	// context providers a parent patch adds reach it.
	if c := curRender; c != nil && c.view != nil && c.n != nil {
		k.parent, k.parentK = c.view.root, c.n.K
	}
	if id, ok := props["$key"]; ok {
		k.key = keyString(id)
	} else {
		k.hash = h
	}
	in := insts[k]
	if in == nil {
		in = &inst{root: newRoot(), name: name, key: k, sent: h ^ 1, slots: map[string]int64{}}
		in.view = NewView(in.root)
		insts[k] = in
		rootsMu.Lock()
		roots[in.root] = in
		rootsMu.Unlock()
	}
	in.used = t
	if h != in.sent {
		in.sent = h
		if nb, ok := b.(NestedBridge); ok && k.parent != 0 {
			nb.RenderIn(in.root, k.parent, k.parentK, name, encodeProps(props, in.slots))
		} else {
			b.Render(in.root, name, encodeProps(props, in.slots))
		}
	} else if len(in.slots) > 0 {
		refreshFns(props, in.slots, "")
	}
	nodes := in.tree.Load()
	if nodes == nil {
		return native(gtx, props)
	}
	return in.view.Layout(gtx, *nodes, name, props, native)
}

// refreshFns points each Go-func slot at this frame's closure.
func refreshFns(v any, slots map[string]int64, path string) {
	switch v := v.(type) {
	case Props:
		for k, x := range v {
			refreshFns(x, slots, path+"."+k)
		}
	case map[string]any:
		for k, x := range v {
			refreshFns(x, slots, path+"."+k)
		}
	case []any:
		for i, x := range v {
			refreshFns(x, slots, path+"."+strconv.Itoa(i))
		}
	case Slot:
		if id, ok := slots[path]; ok {
			gofns.Lock()
			gofns.m[id] = v
			gofns.Unlock()
		}
		refreshFns(v.Props, slots, path+".p")
	case nil, string, bool, float64, int, int64, JSFunc, Element:
	default:
		if id, ok := slots[path]; ok && isFunc(v) {
			gofns.Lock()
			gofns.m[id] = v
			gofns.Unlock()
		}
	}
}

func keyString(v any) string {
	switch v := v.(type) {
	case string:
		return v
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		return strconv.FormatFloat(v, 'g', -1, 64)
	}
	return string(appendValue(nil, v, nil, ""))
}

// sweep unmounts instances not drawn for a while (scrolled away, closed).
func sweep(t time.Time) {
	if t.Sub(lastSweep) < 2*time.Second {
		return
	}
	lastSweep = t
	b := bridge()
	for k, in := range insts {
		if t.Sub(in.used) > 5*time.Second {
			delete(insts, k)
			dropRoot(in, b)
		}
	}
}

func dropRoot(in *inst, b Bridge) {
	rootsMu.Lock()
	delete(roots, in.root)
	rootsMu.Unlock()
	releaseSlots(in.slots)
	in.view.Close()
	if b != nil {
		b.Unmount(in.root)
	}
}

// Deliver installs a new host tree for root (JS → Go, any goroutine) and
// invalidates the window. Trees for unknown roots are dropped.
func Deliver(root uint64, tree []byte) error {
	nodes, err := ParseTree(tree)
	if err != nil {
		return err
	}
	rootsMu.Lock()
	in := roots[root]
	rootsMu.Unlock()
	if in == nil {
		if deliverOverlay(root, nodes) {
			invalidate()
		}
		return nil
	}
	if in.sub != nil {
		in.sub.mu.Lock()
		in.sub.tree = nodes
		in.sub.mu.Unlock()
	} else {
		in.tree.Store(&nodes)
	}
	invalidate()
	return nil
}

// --- calls from JS into Go funcs: queued to the UI goroutine ---

var calls struct {
	sync.Mutex
	q []func()
}

// CallGo runs the Go func that crossed into JS as {"$gofn":id}. It is
// queued to the UI goroutine (natives' funcs touch UI state) and returns
// null: JS sees a void callback.
func CallGo(id int64, args []byte) ([]byte, error) {
	f := goFn(id)
	if f == nil {
		return []byte("null"), nil
	}
	var a []any
	if len(args) > 0 {
		v, err := decodeValue(args)
		if err != nil {
			return nil, err
		}
		a, _ = v.([]any)
	}
	calls.Lock()
	calls.q = append(calls.q, func() { callGo(f, a) })
	calls.Unlock()
	invalidate()
	return []byte("null"), nil
}

// Pump runs queued JS→Go calls; the ui root calls it once per frame
// (Render also pumps on the patched path).
func Pump() { pump() }

func pump() {
	calls.Lock()
	q := calls.q
	calls.q = nil
	calls.Unlock()
	for _, f := range q {
		f()
	}
}

// Reset drops every instance (plugins disabled / runtime closed).
func Reset() {
	b := bridge()
	for k, in := range insts {
		delete(insts, k)
		dropRoot(in, b)
	}
	patched.Store(nil)
}

// windowSize is the last frame's window size in px (for vw/vh, fixed
// positioning and SetViewport).
var windowSize image.Point

var lastViewport struct {
	size image.Point
	dpr  float32
	dark bool
}

// Frame is the ui root's per-frame hook, called before laying out the
// shell: it records the window size (vw/vh, fixed positioning), runs
// queued JS→Go calls and reports viewport changes to JS. Free when no
// plugin runtime is attached.
func Frame(gtx layout.Context) {
	windowSize = gtx.Constraints.Max
	pushDocument()
	b := bridge()
	if b == nil {
		return
	}
	pump()
	dark := env().Theme != nil && env().Theme.Dark
	if windowSize != lastViewport.size || gtx.Metric.PxPerDp != lastViewport.dpr || dark != lastViewport.dark {
		lastViewport.size, lastViewport.dpr, lastViewport.dark = windowSize, gtx.Metric.PxPerDp, dark
		s := gtx.Metric.PxPerDp
		if s == 0 {
			s = 1
		}
		b.SetViewport(float32(windowSize.X)/s, float32(windowSize.Y)/s, s, dark)
	}
	flushRects(b)
}
