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
	"bytes"
	"image"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/ui/theme"
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
	// offers every package (anchor in window coordinates). OpenProfile is a
	// click on a person (the profile pane, Slack's openProfile);
	// OpenProfileCard opens the profile card as a popover.
	OpenEmojiPicker func(anchor image.Point, onPick func(name string))
	OpenProfile     func(anchor image.Point, userID string)
	OpenProfileCard func(anchor image.Point, userID string)
}

var (
	envMu  sync.RWMutex
	envVal Env

	bridgeVal atomic.Pointer[Bridge]
	// bridgeGen counts SetBridge calls: a new bridge is a JS runtime that
	// hasn't heard the window's viewport yet (Frame).
	bridgeGen atomic.Uint64

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
	bridgeGen.Add(1)
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

// PatchedNames lists the components a plugin patches now, sorted.
func PatchedNames() []string {
	m := patched.Load()
	if m == nil {
		return nil
	}
	out := make([]string, 0, len(*m))
	for n := range *m {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
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
	if patched.Load() == nil && !inspecting.Load() { // inlined: two atomic loads
		return native(gtx, props)
	}
	return renderSlow(gtx, name, props, native)
}

func renderSlow(gtx layout.Context, name string, props Props, native Native) layout.Dimensions {
	if f := CurrentInspectFrame(); f != nil { // the inspect tool (inspect.go)
		i := f.beginComponent(name, func() Props { return props })
		d := renderSlow0(gtx, name, props, native)
		f.endComponent(gtx, i, d)
		return d
	}
	return renderSlow0(gtx, name, props, native)
}

func renderSlow0(gtx layout.Context, name string, props Props, native Native) layout.Dimensions {
	if m := patched.Load(); m == nil || !(*m)[name] || unpatched > 0 {
		return native(gtx, props)
	}
	return renderPatched(gtx, name, props, props, native)
}

// RenderLazy is Render with props built only when name is patched.
func RenderLazy(gtx layout.Context, name string, props func() Props, native Native) layout.Dimensions {
	if patched.Load() == nil && !inspecting.Load() {
		return native(gtx, nil)
	}
	return renderLazySlow(gtx, name, props, native)
}

func renderLazySlow(gtx layout.Context, name string, props func() Props, native Native) layout.Dimensions {
	if f := CurrentInspectFrame(); f != nil { // the inspect tool (inspect.go)
		// no props kept: holding the func would make every caller's
		// closure escape, the fast path included (TestUnpatchedNoAlloc)
		i := f.beginComponent(name, nil)
		d := renderLazySlow0(gtx, name, props, native)
		f.endComponent(gtx, i, d)
		return d
	}
	return renderLazySlow0(gtx, name, props, native)
}

func renderLazySlow0(gtx layout.Context, name string, props func() Props, native Native) layout.Dimensions {
	if m := patched.Load(); m == nil || !(*m)[name] || unpatched > 0 {
		return native(gtx, nil)
	}
	return renderPatched(gtx, name, props(), nil, native)
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
	if patched.Load() == nil && reserved.Load() == nil && !inspecting.Load() { // inlined: the native becomes a direct call
		return native(recv, gtx, nil)
	}
	return componentSlow(gtx, name, recv, props, native)
}

func componentSlow[T any](gtx layout.Context, name string, recv T, props func(T) Props, native func(T, layout.Context, Props) layout.Dimensions) layout.Dimensions {
	if f := CurrentInspectFrame(); f != nil { // the inspect tool (inspect.go)
		var pf func() Props
		if props != nil {
			pf = func() Props { return props(recv) }
		}
		i := f.beginComponent(name, pf)
		d := componentSlow0(gtx, name, recv, props, native)
		f.endComponent(gtx, i, d)
		return d
	}
	return componentSlow0(gtx, name, recv, props, native)
}

func componentSlow0[T any](gtx layout.Context, name string, recv T, props func(T) Props, native func(T, layout.Context, Props) layout.Dimensions) layout.Dimensions {
	if unpatched > 0 { // drawn without plugins (guard.go)
		return native(recv, gtx, nil)
	}
	if m := patched.Load(); m == nil || !(*m)[name] {
		if ins, ok := reservedFor(name); ok { // a plugin's slot kept free until it renders (reserve.go)
			return drawReserved(gtx, ins, func(g layout.Context) layout.Dimensions { return native(recv, g, nil) })
		}
		return native(recv, gtx, nil)
	}
	return componentPatched(gtx, name, recv, props, native)
}

func componentPatched[T any](gtx layout.Context, name string, recv T, props func(T) Props, native func(T, layout.Context, Props) layout.Dimensions) layout.Dimensions {
	var p Props
	if props != nil {
		p = props(recv)
	}
	return renderPatched(gtx, name, p, nil, func(gtx layout.Context, p Props) layout.Dimensions {
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
	// the pane that drew it (Pane) and that pane's window: "" and nil
	// outside a pane
	pane string
	win  *frameClock
}

// curPane is the pane being drawn ("" outside one; UI goroutine, frame
// lock).
var curPane string

// Pane keys the patched component instances drawn until EndPane by the
// pane id and by the window drawing the pane: each pane gets roots of its
// own, even where two of them draw the same component with the same
// "$key", as each of Slack's panes is a React subtree with components of
// its own. A reply also sent to the channel is drawn by the channel's
// list and by its thread pane (Slack: BaseMessageList's MessageWrapper
// and ThreadsFlexpane's ThreadReply, two components): sharing one root,
// the two panes sent JS their own props in turn and the root rendered
// again every frame. The inner of nested panes keys. One assignment, so
// free when nothing is patched:
//
//	defer slackc.EndPane(slackc.Pane(id))
func Pane(id string) (outer string) {
	outer, curPane = curPane, id
	return outer
}

// EndPane ends the pane Pane started; outer is what Pane returned.
func EndPane(outer string) { curPane = outer }

// CurrentPane is the pane being drawn ("" outside one): what the
// instances drawn now are keyed by (RemountKeyedIn). UI goroutine.
func CurrentPane() string { return curPane }

// ResultBridge is implemented by bridges that report what JS did with an
// event: done gets, on any goroutine, whether a listener stopped its
// propagation. A focused plugin element's Escape keydown goes on to the
// window's overlay layers only when none did (dispatchKey), as a DOM
// keydown bubbles up to react-modal's content. Without it the keydown
// goes on at once.
type ResultBridge interface {
	DispatchResult(target int64, event []byte, done func(propagationStopped bool))
}

// EventBridge is implemented by bridges that call a JS function prop with
// a DOM event JS makes React's synthetic one (args carry {"$event":
// fields}: the handler can call stopPropagation and preventDefault) and
// tell done, on any goroutine, whether the handler stopped it
// (JSFunc.CallEvent). In order with Dispatch's events.
type EventBridge interface {
	CallEvent(fn int64, args []byte, done func(stopped bool))
}

// TruthyBridge is implemented by bridges that call a JS function prop the
// way Slack calls a handler whose return value decides what runs next
// (MAMenu's dispatchMessageAction: `onSelectAction(…) || native(…)`):
// done hears, on any goroutine, whether it returned something truthy as
// JS sees it, right away (a returned Promise is truthy and isn't awaited),
// or threw; err when no runtime answered (none, or it went away first).
type TruthyBridge interface {
	CallTruthy(fn int64, args []byte, done func(truthy, threw bool, err error))
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
	sent  uint64    // hash of the props last sent to JS
	draws []drawRec // each window's latest draw (frame.go)
	slots map[string]int64
	view  *View
	tree  atomic.Pointer[[]*Node]
	raw   []byte // the last tree delivered, as JS sent it (rootsMu)
	fn    bool   // a RenderFn root
	sub   *Sub

	// what an unchanged frame skips (UI goroutine): every path leading to
	// a Go func slot (refreshSlots), and the hash of the props' "msg" term
	// while it's the same map (propsHash)
	prefixes map[string]bool
	prefN    int
	pbuf     []byte // refreshSlots' path buffer
	hmsg     Props
	hmsgH    uint64
}

var (
	rootSeq atomic.Uint64

	// roots maps root ids to instances for Deliver (any goroutine).
	rootsMu sync.Mutex
	roots   = map[uint64]*inst{}

	// insts is the UI goroutine's lookup by component identity.
	insts = map[instKey]*inst{}
)

func newRoot() uint64 { return rootSeq.Add(1) &^ (1 << 63) }

func now(gtx layout.Context) time.Time {
	if !gtx.Now.IsZero() {
		return gtx.Now
	}
	return time.Now()
}

// renderPatched draws component name through its plugin's host tree. Until
// the first tree arrives, and with no bridge, native draws as it would
// unpatched: with pre, which is props for Render and nil for Component and
// RenderLazy, whose natives take non-nil props for <Original>'s. Handed
// the props built for JS there, the composer's natives looked in them for
// the host nodes of their parts, found Go's slots and drew nothing: the
// message box was gone until JS answered, and for good when it didn't.
// In a reserved slot (reserve.go) the native still gets props: drawn
// inside the insets, ClientPrimaryView learns its origin from the pointer
// as under a patch.
func renderPatched(gtx layout.Context, name string, props, pre Props, native Native) layout.Dimensions {
	b := bridge()
	if b == nil {
		return native(gtx, pre)
	}
	pump()
	t := now(gtx)

	k := instKey{name: name}
	if curPane != "" {
		k.pane, k.win = curPane, curClock
	}
	// Drawn by a native inside another patched component's tree (a row's
	// <Original> calling MessageText): the child root belongs under that
	// "@" node, so JS can mount it inside the parent's React tree and
	// context providers a parent patch adds reach it.
	if c := curRender; c != nil && c.view != nil && c.n != nil {
		k.parent, k.parentK = c.view.root, c.n.K
	}
	var h uint64
	id, keyed := props["$key"]
	if keyed {
		k.key = keyString(id)
	} else {
		h = hashProps(props)
		k.hash = h
	}
	in := insts[k]
	created := in == nil
	if created {
		in = &inst{root: newRoot(), name: name, key: k, slots: map[string]int64{}}
		in.view = NewView(in.root)
		insts[k] = in
		rootsMu.Lock()
		roots[in.root] = in
		rootsMu.Unlock()
	}
	if keyed {
		h = in.propsHash(props)
	}
	if created {
		in.sent = h ^ 1 // the first frame always renders
	}
	in.drawn(t)
	if h != in.sent {
		in.sent = h
		if nb, ok := b.(NestedBridge); ok && k.parent != 0 {
			nb.RenderIn(in.root, k.parent, k.parentK, name, encodeProps(props, in.slots))
		} else {
			b.Render(in.root, name, encodeProps(props, in.slots))
		}
	} else if len(in.slots) > 0 {
		in.refreshSlots(props)
	}
	nodes := in.tree.Load()
	if nodes == nil {
		if ins, ok := reservedFor(name); ok { // its slot stays free until the tree arrives (reserve.go)
			return drawReserved(gtx, ins, func(g layout.Context) layout.Dimensions {
				return probeNative(name, func() layout.Dimensions { return native(g, props) })
			})
		}
		return probeNative(name, func() layout.Dimensions { return native(gtx, pre) })
	}
	unreserve(name)
	learn := in.view.learnPending
	d := in.view.Layout(gtx, *nodes, name, props, native)
	if learn {
		in.view.learnPending = false
		in.view.learnOriginal(name, d.Size, gtx.Metric.PxPerDp)
	}
	return d
}

// refreshSlots points each Go-func slot at this frame's closure (the
// props are rebuilt every frame, the ids stay). It walks only the paths
// that lead to a slot, building them in a buffer: walking all the props,
// a message's blocks and reactions included, with a new string per key,
// cost a patched row more than hashing them did.
func (in *inst) refreshSlots(p Props) {
	if in.prefN != len(in.slots) { // slots only grow (encodeProps)
		in.prefixes = make(map[string]bool, 2*len(in.slots))
		for path := range in.slots {
			for i := 1; i < len(path); i++ {
				if path[i] == '.' {
					in.prefixes[path[:i]] = true
				}
			}
			in.prefixes[path] = true
		}
		in.prefN = len(in.slots)
	}
	if in.pbuf == nil {
		in.pbuf = make([]byte, 0, 96)
	}
	gofns.Lock()
	in.refresh(p, in.pbuf[:0])
	gofns.Unlock()
}

// refresh is refreshSlots under v at path (gofns held). The paths are the
// ones encodeProps gave the slots: ".key", ".index", ".p" for a Slot's
// props.
func (in *inst) refresh(v any, path []byte) {
	switch x := v.(type) {
	case Props:
		for k, c := range x {
			if q := append(append(path, '.'), k...); in.prefixes[string(q)] {
				in.refresh(c, q)
			}
		}
	case map[string]any:
		for k, c := range x {
			if q := append(append(path, '.'), k...); in.prefixes[string(q)] {
				in.refresh(c, q)
			}
		}
	case []any:
		for i, c := range x {
			if q := strconv.AppendInt(append(path, '.'), int64(i), 10); in.prefixes[string(q)] {
				in.refresh(c, q)
			}
		}
	case Slot:
		if id, ok := in.slots[string(path)]; ok {
			gofns.m[id] = v // the interface it came in (x would be boxed again)
		}
		if q := append(path, ".p"...); in.prefixes[string(q)] {
			in.refresh(x.Props, q)
		}
	case nil, string, bool, float64, int, int64, JSFunc, Element:
	default:
		if id, ok := in.slots[string(path)]; ok && isFunc(v) {
			gofns.m[id] = v
		}
	}
}

// propsHash is hashProps(p), keeping the "msg" term while it's the same
// map: rows build their message props once per revision of the message,
// and hashing its blocks, files and reactions again for each of a row's
// patched components every frame was most of their hashing. The memo
// holds the map, so its address can't come back as another message.
func (in *inst) propsHash(p Props) uint64 {
	var h uint64
	for k, v := range p {
		if k == "$key" {
			continue
		}
		if m, ok := v.(Props); ok && k == "msg" && m != nil {
			if in.hmsg == nil || !sameMap(in.hmsg, m) {
				in.hmsg, in.hmsgH = m, mix(hashValue(hstr(fnvOff, k), v))
			}
			h += in.hmsgH
			continue
		}
		h += mix(hashValue(hstr(fnvOff, k), v))
	}
	return mix(h ^ uint64(len(p)))
}

// sameMap reports whether a and b are the same map (not equal contents).
func sameMap(a, b Props) bool {
	return *(*unsafe.Pointer)(unsafe.Pointer(&a)) == *(*unsafe.Pointer)(unsafe.Pointer(&b))
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
	// JS commits a root again with the same output whenever React
	// re-renders it for nothing (a portal under it mounted or unmounted,
	// a context or store update its output ignores): installing that tree
	// would rebuild the view's boxes (hover states, scroll offsets and
	// measured natives start over) and cost a frame, every time.
	rootsMu.Lock()
	in := roots[root]
	if in != nil && in.raw != nil && bytes.Equal(in.raw, tree) {
		rootsMu.Unlock()
		return nil
	}
	rootsMu.Unlock()
	nodes, err := ParseTree(tree)
	if err != nil {
		return err
	}
	if in != nil {
		rootsMu.Lock()
		in.raw = append(in.raw[:0], tree...)
		rootsMu.Unlock()
	}
	if in == nil {
		if deliverOverlay(root, nodes) {
			invalidate()
		}
		return nil
	}
	logTree(in.name, root, nodes)
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
	if subFrame { // callbacks touch the main window's UI state: run them there
		return
	}
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

// lastViewport is what Frame last reported to JS, and to which bridge
// (bridgeGen): a runtime started after it (plugins turned off and on
// again) is told the viewport at its first frame, rather than keeping
// its document's defaults (1280x800 at 1x, light: <body> without
// sk-client-theme--dark) until the window changes.
var lastViewport struct {
	size image.Point
	dpr  float32
	dark bool
	gen  uint64
}

// Frame is the ui root's per-frame hook, called before laying out the
// shell: it starts the main window's frame (its patched roots the last
// frames left undrawn unmount: frame.go), records the window size (vw/vh,
// fixed positioning), runs queued JS→Go calls and reports viewport
// changes to JS. Free when no plugin runtime is attached.
func Frame(gtx layout.Context) {
	curClock, curPane = &mainClock, ""
	mainClock.begin(now(gtx))
	mainClock.layers.begin(gtx)
	windowSize = gtx.Constraints.Max
	pushDocument()
	b := bridge()
	if b == nil {
		return
	}
	pump()
	dark := env().Theme != nil && env().Theme.Dark
	gen := bridgeGen.Load()
	if windowSize != lastViewport.size || gtx.Metric.PxPerDp != lastViewport.dpr || dark != lastViewport.dark || gen != lastViewport.gen {
		lastViewport.size, lastViewport.dpr, lastViewport.dark, lastViewport.gen = windowSize, gtx.Metric.PxPerDp, dark, gen
		s := gtx.Metric.PxPerDp
		if s == 0 {
			s = 1
		}
		b.SetViewport(float32(windowSize.X)/s, float32(windowSize.Y)/s, s, dark)
	}
	flushRects(b)
}
