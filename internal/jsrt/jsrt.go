// Package jsrt is Clack's JavaScript runtime for Taut plugins.
//
// The engine is QuickJS-NG compiled to WebAssembly (see csrc/ and the
// jsrt-wasm workflow) and run on wazero: pure Go, no cgo, full ES2023+.
// Each Runtime owns one JS thread (a goroutine). Every JS value is touched
// only on that thread: Do, Post, HostFunc callbacks and Root callbacks all
// run there. Around the engine sits a browser-like environment written in JS
// (event loop, timers, fetch, URL, TextEncoder, DOM subset, Intl, …) and real
// React with a react-reconciler host config that renders to the JSON host
// tree described in docs/PLUGINS.md.
//
// Nothing here runs until New is called, so Clack without plugins pays
// nothing for this package.
package jsrt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// KV backs localStorage / sessionStorage. Implementations must be safe for
// use from the JS goroutine; they are only called from there.
type KV interface {
	Get(key string) (string, bool)
	Set(key, value string)
	Delete(key string)
	Keys() []string // insertion or sorted order; used by key(i)/length
	Clear()
}

// Options configures a Runtime. Every field is optional.
type Options struct {
	// Fetch performs fetch() requests (and api.fetch built on it). nil means
	// tlsfp.Client.Do (Slack hosts get Clack's Slack ClientHello, others Go's
	// default transport). Called on its own goroutine; ctx is cancelled
	// by AbortSignal.
	Fetch func(ctx context.Context, req *http.Request) (*http.Response, error)
	// Storage returns the backing store for a namespace: "localStorage" or
	// "sessionStorage". nil (or a nil KV) means an in-memory map.
	Storage func(namespace string) KV
	// Log receives console output. level is "log", "info", "warn", "error"
	// or "debug". nil means log.Printf.
	Log func(level, msg string)
	// CacheDir holds wazero's compiled-code cache so later starts skip
	// compiling the engine (~10 ms instead of hundreds). "" = memory only.
	CacheDir string
	// Timeout bounds one JS task (a Do, a timer callback, an event). A task
	// running longer is interrupted with an uncatchable error. Default 5 s.
	Timeout time.Duration
	// MemoryLimit for the JS heap in bytes. Default 512 MiB.
	MemoryLimit int
	// OpenURL opens a link (window.open, <a href> clicks, location.assign).
	// nil ignores them.
	OpenURL func(url string)
	// ClipboardWrite / ClipboardRead back navigator.clipboard. nil rejects.
	ClipboardWrite func(text string) error
	ClipboardRead  func() (string, error)
	// PickFiles shows the native file picker for <input type=file> (accept is
	// the input's accept attribute). Return nil files (and nil error) when
	// the user cancels. Called on its own goroutine. nil = inputs do nothing.
	PickFiles func(accept string, multiple bool) ([]PickedFile, error)
	// Language is navigator.language (default "en-US"); Platform is
	// navigator.platform (default from GOOS: "MacIntel", "Win32", "Linux x86_64").
	Language, Platform string

	// heapWasmMemory (tests) keeps the engine's wasm memory in a Go slice
	// grown like wazero's own, as before wasmmem.go: the "before" side of
	// the tests that measure the reservation.
	heapWasmMemory bool
}

// Errors returned by the runtime.
var (
	ErrClosed  = errors.New("jsrt: runtime closed")
	ErrTimeout = errors.New("jsrt: timeout")
)

// JSError is a JS exception surfaced to Go.
type JSError struct {
	Name    string // "TypeError", …
	Message string
	Stack   string
}

func (e *JSError) Error() string {
	if e.Name == "" {
		return e.Message
	}
	return e.Name + ": " + e.Message
}

// HostFunc is a Go function callable from JS. It runs on the JS thread and
// may use js and the args freely; the returned value is converted with
// Ctx.Value. A non-nil error is thrown as a JS Error (a *JSError keeps its
// name). args are only valid during the call unless Dup'd... they are
// ordinary Values, so keeping one keeps the JS value alive.
type HostFunc func(js *Ctx, this Value, args []Value) (any, error)

// JSONFunc is the JSON flavour of HostFunc: every argument arrives as JSON
// (undefined becomes null, functions become null) and the result is
// marshalled with encoding/json (json.RawMessage passes through, []byte
// becomes a Uint8Array, Value passes through as-is).
type JSONFunc func(args []json.RawMessage) (any, error)

// AsyncFunc is called on its own goroutine; JS gets a Promise that settles
// with its result. ctx is cancelled when the runtime closes.
type AsyncFunc func(ctx context.Context, args []json.RawMessage) (any, error)

// Module registers an ES module source under a bare name ("$taut", "react",
// …). import "name" in any module, Import(name) and dynamic import() resolve
// to it. Registering again replaces it for later imports. Any goroutine.
// "react", "react/jsx-runtime", "react-dom", "react-dom/client" and
// "scheduler" are built in.
func (r *Runtime) Module(name, src string) {
	r.mu.Lock()
	r.modules[name] = src
	r.mu.Unlock()
}

// Import imports a registered module (or anything the loader resolves) and
// returns its namespace object, after top-level await settles. Any
// goroutine; off the JS thread the namespace is remote (see Value).
func (r *Runtime) Import(name string) (Value, error) {
	if r.onJSThread() {
		return r.js.Import(name)
	}
	type res struct {
		v   Value
		err error
	}
	ch := make(chan res, 1)
	err := r.Do(func(js *Ctx) error {
		p, err := js.importPromise(name)
		if err != nil {
			return err
		}
		p.Then(func(v Value, err error) { ch <- res{v.asRemote(), err} })
		return nil
	})
	if err != nil {
		return Value{}, err
	}
	select {
	case x := <-ch:
		return x.v, x.err
	case <-r.done:
		return Value{}, ErrClosed
	}
}

// Eval runs a classic script (sloppy mode, like a <script> or new Function
// body) and returns its completion value. Taut's user plugin format is an
// IIFE expression returning the plugin class, so Eval(name, code) yields the
// class. name is used in stack traces. Any goroutine; the Value is remote
// (usable from any goroutine, see Value).
func (r *Runtime) Eval(name, src string) (Value, error) {
	var v Value
	err := r.Do(func(js *Ctx) (err error) {
		v, err = js.Eval(name, src)
		v = v.asRemote()
		return err
	})
	return v, err
}

// Expose makes fn callable from JS as globalThis.__clack[name]. Any
// goroutine; takes effect before the next JS task.
func (r *Runtime) Expose(name string, fn HostFunc) {
	r.Post(func(js *Ctx) { js.expose(name, fn) })
}

// ExposeJSON is Expose with JSON-encoded arguments.
func (r *Runtime) ExposeJSON(name string, fn JSONFunc) {
	r.Expose(name, func(js *Ctx, _ Value, args []Value) (any, error) {
		raw := make([]json.RawMessage, len(args))
		for i, a := range args {
			raw[i] = a.jsonOrNull()
		}
		return fn(raw)
	})
}

// ExposeAsync exposes fn as a JS function returning a Promise; fn runs on
// its own goroutine.
func (r *Runtime) ExposeAsync(name string, fn AsyncFunc) { r.ExposeAsyncSeq(name, fn, nil) }

// ExposeAsyncSeq is ExposeAsync with the calls seq picks (it sees their
// args, on the JS thread) run in the order JS made them: fn runs for them
// one call after another on one goroutine, where a goroutine each could
// run two in reverse. For calls that never wait long (a slow one holds
// the ones after it): a notification's native, which Slack runs in its one
// JS thread in the order the notifications' patches call it.
func (r *Runtime) ExposeAsyncSeq(name string, fn AsyncFunc, seq func(args []json.RawMessage) bool) {
	r.Expose(name, func(js *Ctx, _ Value, args []Value) (any, error) {
		raw := make([]json.RawMessage, len(args))
		for i, a := range args {
			raw[i] = a.jsonOrNull()
		}
		p, resolve, reject := js.NewPromise()
		r.heldAwait() // a send's chain awaiting Go (awaitUserChain)
		fctx := r.ctx
		if l := r.chainLane(); l.root != 0 {
			fctx = context.WithValue(fctx, laneKey{}, l) // what it calls goes on with the chain (laneOf)
		}
		run := func() {
			v, err := fn(fctx, raw)
			if err != nil {
				reject(err)
				return
			}
			resolve(v)
		}
		if seq != nil && seq(raw) {
			r.runSeq(run)
		} else {
			go run()
		}
		return p, nil
	})
}

// runSeq runs f after every f queued before it, on a goroutine of its own
// (ExposeAsyncSeq).
func (r *Runtime) runSeq(f func()) {
	r.seqMu.Lock()
	r.seq = append(r.seq, f)
	start := !r.seqOn
	r.seqOn = true
	r.seqMu.Unlock()
	if !start {
		return
	}
	go func() {
		for {
			r.seqMu.Lock()
			if len(r.seq) == 0 {
				r.seqOn, r.seq = false, nil
				r.seqMu.Unlock()
				return
			}
			f := r.seq[0]
			r.seq[0] = nil
			r.seq = r.seq[1:]
			r.seqMu.Unlock()
			f()
		}
	}()
}

// CallThen is Call without the wait: the call is queued before CallThen
// returns, on the caller's goroutine (a user-blocking one ahead of what
// the caller queues after it: the composer's re-render after Enter, a
// send's check first; an in-order one ahead of the user's later input),
// and done hears its result on another goroutine.
func (r *Runtime) CallThen(ctx context.Context, path string, done func(json.RawMessage, error), args ...any) {
	if r.closed.Load() {
		go done(nil, ErrClosed)
		return
	}
	type res struct {
		v   json.RawMessage
		err error
	}
	ch := make(chan res, 1)
	t := callTask(ctx, path)
	t.f = func() {
		defer func() {
			if p := recover(); p != nil {
				ch <- res{nil, fmt.Errorf("jsrt: panic in Call: %v", p)}
				r.afterPanic(p)
			}
		}()
		js := r.js
		fn, this, err := js.resolvePath(path)
		if err != nil {
			ch <- res{nil, err}
			return
		}
		v, err := fn.CallThis(this, args...)
		if err != nil {
			ch <- res{nil, err}
			return
		}
		if !v.IsPromise() {
			b, err := v.JSON()
			ch <- res{b, err}
			return
		}
		v.Then(func(v Value, err error) {
			if err != nil {
				ch <- res{nil, err}
				return
			}
			b, err := v.JSON()
			ch <- res{b, err}
		})
	}
	r.enqueue(t)
	go func() {
		select {
		case x := <-ch:
			done(x.v, x.err)
		case <-ctx.Done():
			select {
			case x := <-ch: // it answered as the deadline passed
				done(x.v, x.err)
			default:
				done(nil, ctx.Err())
			}
		case <-r.done:
			select {
			case x := <-ch:
				done(x.v, x.err)
			default:
				done(nil, ErrClosed)
			}
		}
	}()
}

// callTask is a Call's task, on the lane ctx names: the user's
// (UserBlocking), in order with the user's later work (InOrder), or the
// queue.
func callTask(ctx context.Context, path string) task {
	urgent := IsUserBlocking(ctx)
	t := task{kind: "Call", what: path, urgent: urgent, ordered: !urgent && IsInOrder(ctx)}
	if !urgent {
		return t
	}
	l, ok := laneOf(ctx)
	if ok {
		// the chain an async Go function's call goes on with: its root
		// and its start
		t.root, t.chainSeq = l.root, l.seq
	}
	t.send = IsUserChain(ctx) || ok && l.send
	t.hold = t.send && !IsNoChain(ctx) && (IsUserChain(ctx) || ok && l.hold)
	return t
}

// Call calls the global function at path ("a.b.c" from globalThis; `this`
// is the object holding the last segment) with args converted by
// Ctx.Value, waits for a returned Promise to settle (or ctx to end), and
// returns the result as JSON (undefined → null). Any goroutine except the
// JS thread itself. With a UserBlocking ctx it runs ahead of the tasks
// already queued; with an InOrder one, ahead of the user's later work.
func (r *Runtime) Call(ctx context.Context, path string, args ...any) (json.RawMessage, error) {
	type res struct {
		v   json.RawMessage
		err error
	}
	ch := make(chan res, 1)
	onJS := r.onJSThread()
	err := r.doCtx(ctx, callTask(ctx, path), func(js *Ctx) error {
		fn, this, err := js.resolvePath(path)
		if err != nil {
			return err
		}
		v, err := fn.CallThis(this, args...)
		if err != nil {
			return err
		}
		if !v.IsPromise() {
			b, err := v.JSON()
			ch <- res{b, err}
			return nil
		}
		if onJS {
			return errors.New("jsrt: Runtime.Call of an async function from the JS thread would deadlock; use Value.Then")
		}
		v.Then(func(v Value, err error) {
			if err != nil {
				ch <- res{nil, err}
				return
			}
			b, err := v.JSON()
			ch <- res{b, err}
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	select {
	case x := <-ch: // an answer that is in wins over a deadline that passed with it
		return x.v, x.err
	default:
	}
	select {
	case x := <-ch:
		return x.v, x.err
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-r.done:
		return nil, ErrClosed
	}
}
