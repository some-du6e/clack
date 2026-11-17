package jsrt

import (
	"bytes"
	"context"
	"encoding/json"
	"sync/atomic"
)

// Root is a React root rendering into the host tree. Each commit delivers
// the whole tree (a JSON array of top-level nodes, docs/PLUGINS.md) to the
// onUpdate callback, on the JS thread; the slice is the receiver's to keep.
type Root struct {
	r        *Runtime
	id       uint64
	onUpdate func(tree []byte)
	closed   bool
}

var rootSeq atomic.Uint64

// NewRoot creates a React root. Any goroutine. The JS side of the root is
// created lazily on the first Render.
func (r *Runtime) NewRoot(onUpdate func(tree []byte)) *Root {
	rt := &Root{r: r, id: rootSeq.Add(1), onUpdate: onUpdate}
	r.Post(func(js *Ctx) {
		r.roots[rt.id] = rt
	})
	return rt
}

// ID identifies the root (also to JS: __clack.render(id, el)).
func (rt *Root) ID() uint64 { return rt.id }

// Render renders a React element (or anything React can render) into the
// root. JS thread. The commit happens synchronously for the first render
// and asynchronously (batched per task) for later state updates; each
// commit calls onUpdate.
func (rt *Root) Render(js *Ctx, element Value) error {
	if rt.closed {
		return ErrClosed
	}
	js.r.roots[rt.id] = rt
	fn := js.Global().Get("__clack").Get("render")
	_, err := fn.Call(float64(rt.id), element)
	return err
}

// RenderPortal renders element as a portal inside another root's tree, at
// the native Slack component whose host node has k (its "k" in the host
// tree): the element sees every context and provider around that native,
// while its commits are delivered to this root's onUpdate as usual. Used
// for patched components a native draws inside another patched
// component. JS thread. Unmount removes the portal.
func (rt *Root) RenderPortal(js *Ctx, k int64, element Value) error {
	if rt.closed {
		return ErrClosed
	}
	js.r.roots[rt.id] = rt
	fn := js.Global().Get("__clack").Get("portalInto")
	_, err := fn.Call(float64(k), float64(rt.id), element)
	return err
}

// Unmount unmounts the tree and forgets the root. Any goroutine.
func (rt *Root) Unmount() {
	rt.r.Post(func(js *Ctx) {
		if rt.closed {
			return
		}
		rt.closed = true
		delete(js.r.roots, rt.id)
		if fn := js.Global().Get("__clack").Get("unmount"); fn.IsFunction() {
			_, _ = fn.Call(float64(rt.id))
		}
	})
}

// deliver is called from JS (via the internal "commit" host function).
func (r *Runtime) deliver(id uint64, tree []byte) {
	if rt := r.roots[id]; rt != nil {
		if !rt.closed && rt.onUpdate != nil {
			rt.onUpdate(tree)
		}
		return
	}
	if id&jsRootBit != 0 && r.onCommit != nil {
		r.onCommit(id, tree)
	}
}

// jsRootBit marks root ids allocated by JS (__clack.createRoot()).
const jsRootBit = 1 << 52

// EventResult reports what JS handlers did with an event.
type EventResult struct {
	DefaultPrevented   bool `json:"defaultPrevented"`
	PropagationStopped bool `json:"propagationStopped"`
}

// Rect is a laid-out box in window coordinates (CSS px).
type Rect struct{ X, Y, W, H float32 }

// DispatchEvent dispatches a DOM event at host node target (its "k"; 0 is
// the document, for window-level keys). ev is the event JSON from
// docs/PLUGINS.md. Capture and bubble run in JS like a browser. Async, any
// goroutine.
// A discrete event (a click, a key, a change: React's
// DiscreteEventPriority) is the user's input and runs on the user-blocking
// lane, in order with the user's other input and the sends after it
// (UserBlocking); a continuous one (a mouse move, a scroll) waits its turn.
func (r *Runtime) DispatchEvent(target int, ev []byte) {
	r.post(discreteEvent(ev), func(js *Ctx) { _, _ = js.dispatch(target, ev) })
}

// DispatchEventThen is DispatchEvent telling done, on the JS goroutine,
// what JS did with the event: whether a listener stopped its propagation
// or prevented its default. done doesn't run if the runtime closes first.
func (r *Runtime) DispatchEventThen(target int, ev []byte, done func(EventResult, error)) {
	r.post(discreteEvent(ev), func(js *Ctx) {
		v, err := js.dispatch(target, ev)
		done(v, err)
	})
}

// discreteEvents are React's DiscreteEventPriority events (ReactDOM's
// getEventPriority): the user's input, which a browser handles in order
// ahead of rendering.
var discreteEvents = map[string]bool{
	"beforetoggle": true, "cancel": true, "click": true, "close": true, "contextmenu": true, "copy": true, "cut": true,
	"auxclick": true, "dblclick": true, "dragend": true, "dragstart": true, "drop": true, "focusin": true, "focusout": true,
	"input": true, "invalid": true, "keydown": true, "keypress": true, "keyup": true, "mousedown": true, "mouseup": true,
	"paste": true, "pause": true, "play": true, "pointercancel": true, "pointerdown": true, "pointerup": true,
	"ratechange": true, "reset": true, "resize": true, "seeked": true, "submit": true, "toggle": true, "touchcancel": true,
	"touchend": true, "touchstart": true, "volumechange": true, "change": true, "selectionchange": true, "textInput": true,
	"compositionstart": true, "compositionend": true, "compositionupdate": true, "beforeblur": true, "afterblur": true,
	"beforeinput": true, "blur": true, "fullscreenchange": true, "focus": true, "hashchange": true, "popstate": true,
	"select": true, "selectstart": true,
}

// discreteEvent reports whether the event JSON's type is a discrete
// event's.
func discreteEvent(ev []byte) bool {
	i := bytes.Index(ev, []byte(`"type":`))
	if i < 0 {
		return false
	}
	rest := bytes.TrimLeft(ev[i+len(`"type":`):], " ")
	if len(rest) == 0 || rest[0] != '"' {
		return false
	}
	rest = rest[1:]
	j := bytes.IndexByte(rest, '"')
	return j >= 0 && discreteEvents[string(rest[:j])]
}

// DispatchEventSync is DispatchEvent waiting for the result (e.g. to learn
// whether a key was preventDefault'ed). Bounded by ctx and Options.Timeout.
func (r *Runtime) DispatchEventSync(ctx context.Context, target int, ev []byte) (EventResult, error) {
	type res struct {
		v   EventResult
		err error
	}
	ch := make(chan res, 1)
	r.post(discreteEvent(ev), func(js *Ctx) {
		v, err := js.dispatch(target, ev)
		ch <- res{v, err}
	})
	select {
	case x := <-ch:
		return x.v, x.err
	case <-ctx.Done():
		return EventResult{}, ctx.Err()
	case <-r.done:
		return EventResult{}, ErrClosed
	}
}

func (c *Ctx) dispatch(target int, ev []byte) (EventResult, error) {
	fn := c.Global().Get("__clack").Get("dispatch")
	evv, err := c.ParseJSON(ev)
	if err != nil {
		return EventResult{}, err
	}
	v, err := fn.Call(target, evv)
	if err != nil {
		return EventResult{}, err
	}
	var out EventResult
	err = v.Decode(&out)
	return out, err
}

// CallFn invokes a function prop that was serialized as {"$fn":id} inside a
// native ("@") node, with JSON args. Async, any goroutine.
func (r *Runtime) CallFn(id int, args []byte) { r.callFn(false, id, args) }

// CallFnUserBlocking is CallFn for the user's input (a control's onClick
// or onChange): it runs on the user-blocking lane (PostUserBlocking).
func (r *Runtime) CallFnUserBlocking(id int, args []byte) { r.callFn(true, id, args) }

func (r *Runtime) callFn(urgent bool, id int, args []byte) {
	r.post(urgent, func(js *Ctx) {
		fn := js.Global().Get("__clack").Get("callFn")
		var a Value
		if len(args) > 0 {
			a, _ = js.ParseJSON(args)
		}
		_, _ = fn.Call(id, a)
	})
}

// SetRects reports laid-out boxes of host nodes by "k" (feeds
// getBoundingClientRect, offsetWidth & co. and ResizeObserver). Async.
func (r *Runtime) SetRects(rects map[int]Rect) {
	b, _ := json.Marshal(rectsJSON(rects))
	r.Post(func(js *Ctx) {
		fn := js.Global().Get("__clack").Get("setRects")
		v, err := js.ParseJSON(b)
		if err == nil {
			_, _ = fn.Call(v)
		}
	})
}

func rectsJSON(rects map[int]Rect) map[int][4]float32 {
	m := make(map[int][4]float32, len(rects))
	for k, x := range rects {
		m[k] = [4]float32{x.X, x.Y, x.W, x.H}
	}
	return m
}

// SetViewport sets window.innerWidth/innerHeight, devicePixelRatio and
// prefers-color-scheme (dark); fires resize / matchMedia change. Async.
func (r *Runtime) SetViewport(w, h, dpr float32, dark bool) {
	r.Post(func(js *Ctx) {
		fn := js.Global().Get("__clack").Get("setViewport")
		_, _ = fn.Call(w, h, dpr, dark)
	})
}

// OnDocument subscribes to document changes: body is document.body's
// children as a host tree (overlays), css the text of every <style> in
// document.head plus inline <style> in body, in order. Called on the JS
// thread after each change, once per task at most. Any goroutine.
func (r *Runtime) OnDocument(fn func(body []byte, css []string)) {
	r.Post(func(js *Ctx) {
		js.r.onDoc = fn
		js.resendDocument()
	})
}

// OnDocumentProps subscribes to the attributes and inline style of <html>
// and <body> (a "p" object each, same shape as host-tree props), e.g.
// documentElement.classList / style.setProperty('--x', …) used by global CSS.
// Called on the JS thread after a change. Any goroutine.
func (r *Runtime) OnDocumentProps(fn func(html, body []byte)) {
	r.Post(func(js *Ctx) {
		js.r.onDocProps = fn
		js.resendDocument()
	})
}

// OnCommit receives the commits of roots created from JS
// (__clack.createRoot(), ids with bit 52 set), on the JS thread.
func (r *Runtime) OnCommit(fn func(id uint64, tree []byte)) {
	r.Post(func(js *Ctx) { js.r.onCommit = fn })
}

// flushRender is run at each checkpoint: JS commits pending roots and the
// document, then calls back into Go (deliver / document).
func (r *Runtime) flushRender() {
	if r.eng == nil || !r.eng.hasDirty() {
		return
	}
	r.eng.dirty = false
	fn := r.js.Global().Get("__clack").Get("flush")
	if fn.IsFunction() {
		if _, err := fn.Call(); err != nil {
			r.logError("flush", err)
		}
	}
	r.eng.runJobs(r.js)
}

// SetHidden reports window visibility / focus (document.hidden,
// visibilitychange, window focus/blur). Async.
func (r *Runtime) SetHidden(hidden bool) {
	r.Post(func(js *Ctx) {
		_, _ = js.Global().Get("__clack").Get("setHidden").Call(hidden)
	})
}

// WantsEvent reports whether JS listens for DOM event type on window or
// document (e.g. "mousemove", "keydown", "pointermove", "resize"), so the UI
// can skip forwarding global input nobody handles. Any goroutine; cheap.
func (r *Runtime) WantsEvent(typ string) bool {
	r.globalMu.RLock()
	defer r.globalMu.RUnlock()
	return r.global[typ] > 0
}

func (c *Ctx) resendDocument() {
	if fn := c.Global().Get("__clack").Get("resendDocument"); fn.IsFunction() {
		_, _ = fn.Call()
	}
}
