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
// (UserBlocking); a continuous one (a mouse move, a scroll) waits its
// turn, though never behind a later discrete one (PostInOrder: a browser
// dispatches input in order).
func (r *Runtime) DispatchEvent(target int, ev []byte) {
	if discreteEvent(ev) {
		r.post(true, func(js *Ctx) { _, _ = js.dispatch(target, ev) })
		return
	}
	r.postMove(target, ev)
}

// moveKey is a continuous event's target and type (postMove).
type moveKey struct {
	target int
	typ    string
}

// queuedMove is the newest continuous event of its key waiting in its
// task (postMove).
type queuedMove struct {
	ev  []byte
	seq uint64 // enqN when its task was queued; 0 until then (movesMu)
}

// coalescedEvents are the continuous events a newer one of the same target
// and type may stand in for while its task waits: the pointer's moves and
// a scroller's scroll, whose newest says all an older one did. The
// boundary events (mouseover, mouseout, mouseenter, mouseleave and the
// pointer ones) never are: each says the pointer crossed a box, and a later
// one doesn't undo an earlier one (leave, enter, leave merged into leave,
// enter left a box hovered with the pointer outside it, and nothing more
// came to clear it); nor is a wheel event, whose delta adds to the ones
// before it (Chromium sums them), so each goes out with its own.
var coalescedEvents = map[string]bool{
	"mousemove": true, "pointermove": true, "touchmove": true, "drag": true, "dragover": true, "scroll": true,
}

// postMove queues a continuous event (a move, a scroll, a hover, a wheel)
// in order with the user's input. While the JS thread is behind, a move or
// a scroll replaces the event its target's task waits with, but only while
// that task is the newest queued, as Chromium coalesces an event only with
// the last one queued: each used to queue a task of its own, 60 a second,
// and the user's next key or send ran behind them all. Coalesced across
// anything queued since (another target's move, a boundary event, the
// user's input), the newer event ran ahead of what came before it: A at
// p1, B at p2, A at p3 reached JS as A at p3, then B at p2, and a drag
// ended at p2.
func (r *Runtime) postMove(target int, ev []byte) {
	typ := eventType(ev)
	k := moveKey{target, typ}
	merge := coalescedEvents[typ]
	r.movesMu.Lock()
	if q := r.movesQ[k]; merge && q != nil && q.seq != 0 && q.seq == r.seenN.Load() {
		q.ev = ev // its task is still the newest queued: nothing came between (but handle releases, seenN)
		r.movesMu.Unlock()
		return
	}
	q := &queuedMove{ev: ev}
	if merge {
		if r.movesQ == nil {
			r.movesQ = map[moveKey]*queuedMove{}
		}
		r.movesQ[k] = q
	}
	r.movesMu.Unlock()
	n := r.enqueue(task{kind: "Post", what: typ, pc: callerPC(1), ordered: true, f: func() {
		r.movesMu.Lock()
		ev := q.ev
		if r.movesQ[k] == q {
			delete(r.movesQ, k)
		}
		r.movesMu.Unlock()
		_, _ = r.js.dispatch(target, ev)
	}})
	r.movesMu.Lock()
	if r.movesQ[k] == q {
		q.seq = n // not yet run: a newer move may join it while nothing is queued after it
	}
	r.movesMu.Unlock()
}

// eventType is the event JSON's type ("" for none).
func eventType(ev []byte) string {
	i := bytes.Index(ev, []byte(`"type":`))
	if i < 0 {
		return ""
	}
	rest := bytes.TrimLeft(ev[i+len(`"type":`):], " ")
	if len(rest) == 0 || rest[0] != '"' {
		return ""
	}
	rest = rest[1:]
	if j := bytes.IndexByte(rest, '"'); j >= 0 {
		return string(rest[:j])
	}
	return ""
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
func discreteEvent(ev []byte) bool { return discreteEvents[eventType(ev)] }

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
// getBoundingClientRect, offsetWidth & co. and ResizeObserver). Async; in
// order with the user's input (PostInOrder): a click's handler reads the
// layout the click was made on, as in a browser. One task at a time: a
// newer layout joins the boxes the queued one waits with (setRects takes
// each box as it is, so the newest of each wins), until the user's next
// input; each frame that moved a box (every frame while scrolling) queued
// a task of its own.
func (r *Runtime) SetRects(rects map[int]Rect) {
	gen := r.coalesce.Load()
	r.rectsMu.Lock()
	if q := r.rectsQ; q != nil && q.gen == gen {
		for k, x := range rects {
			q.m[k] = x
		}
		r.rectsMu.Unlock()
		return
	}
	q := &rectsBatch{m: make(map[int]Rect, len(rects)), gen: gen}
	for k, x := range rects {
		q.m[k] = x
	}
	r.rectsQ = q
	r.rectsMu.Unlock()
	r.PostInOrder(func(js *Ctx) {
		r.rectsMu.Lock()
		if r.rectsQ == q {
			r.rectsQ = nil
		}
		b, _ := json.Marshal(rectsJSON(q.m))
		r.rectsMu.Unlock()
		fn := js.Global().Get("__clack").Get("setRects")
		v, err := js.ParseJSON(b)
		if err == nil {
			_, _ = fn.Call(v)
		}
	})
}

// rectsBatch is the boxes a queued setRects task carries (SetRects).
type rectsBatch struct {
	m   map[int]Rect
	gen uint64
}

func rectsJSON(rects map[int]Rect) map[int][4]float32 {
	m := make(map[int][4]float32, len(rects))
	for k, x := range rects {
		m[k] = [4]float32{x.X, x.Y, x.W, x.H}
	}
	return m
}

// SetViewport sets window.innerWidth/innerHeight, devicePixelRatio and
// prefers-color-scheme (dark); fires resize / matchMedia change. Async; in
// order with the user's input (PostInOrder).
func (r *Runtime) SetViewport(w, h, dpr float32, dark bool) {
	r.PostInOrder(func(js *Ctx) {
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
// visibilitychange, window focus/blur). Async; in order with the user's
// input (PostInOrder): a key after the window gained focus comes after
// its focus event.
func (r *Runtime) SetHidden(hidden bool) {
	r.PostInOrder(func(js *Ctx) {
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
